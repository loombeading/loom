// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"fmt"
	"io"
	"strings"
)

const ParentChildDepType = "parent-child"

type NotFoundError struct {
	Input string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no Bead matches %q", e.Input)
}

type AmbiguousIDError struct {
	Input      string
	Candidates []string
}

func (e *AmbiguousIDError) Error() string {
	return fmt.Sprintf("%q is ambiguous; candidates: %s", e.Input, strings.Join(e.Candidates, ", "))
}

type staleIDNoticeKey struct{}

func WithStaleIDNotice(ctx context.Context, w io.Writer) context.Context {
	return context.WithValue(ctx, staleIDNoticeKey{}, w)
}

func noticeStaleID(ctx context.Context, input, current string) {
	if w, ok := ctx.Value(staleIDNoticeKey{}).(io.Writer); ok {
		fmt.Fprintf(w, "Note: %s uses a former namespace; the current display ID is %s\n", input, current)
	}
}

type AliasUnresolvedError struct {
	Input string
}

func (e *AliasUnresolvedError) Error() string {
	return fmt.Sprintf("alias %q could not be resolved under the current hierarchy; use the canonical ID instead", e.Input)
}

const maxAliasDepth = 64

type resolvedBead struct {
	id        string
	namespace string
	status    string
	title     string
}

func findByHexPrefix(ctx context.Context, q Querier, idPrefix string) ([]resolvedBead, error) {
	exact, err := queryResolved(ctx, q, `SELECT id, namespace, status, title FROM beads WHERE short_id = ? ORDER BY created_at ASC, id ASC`, idPrefix)
	if err != nil || len(exact) == 1 {
		return exact, err
	}
	return queryResolved(ctx, q, `SELECT id, namespace, status, title FROM beads WHERE id LIKE ? ESCAPE '\' ORDER BY created_at ASC, id ASC`, escapeLike(idPrefix)+"%")
}

func queryResolved(ctx context.Context, q Querier, query string, arg string) ([]resolvedBead, error) {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("resolve id: query by hex namespace: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []resolvedBead
	for rows.Next() {
		var ri resolvedBead
		if err := rows.Scan(&ri.id, &ri.namespace, &ri.status, &ri.title); err != nil {
			return nil, fmt.Errorf("resolve id: scan: %w", err)
		}
		out = append(out, ri)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve id: iterate: %w", err)
	}
	return out, nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func resolveHexPrefix(ctx context.Context, q Querier, input, hexPrefix string, n ShortIDs) (resolvedBead, error) {
	matches, err := findByHexPrefix(ctx, q, hexPrefix)
	if err != nil {
		return resolvedBead{}, err
	}
	switch len(matches) {
	case 0:
		return resolvedBead{}, &NotFoundError{Input: input}
	case 1:
		return matches[0], nil
	default:
		var live []resolvedBead
		for _, m := range matches {
			if m.status == StatusOpen || m.status == StatusInProgress {
				live = append(live, m)
			}
		}
		if len(live) == 1 {
			return live[0], nil
		}
		candidates := make([]string, len(matches))
		for i, m := range matches {
			candidates[i] = fmt.Sprintf("%s [%s] %s", DisplayID(m.namespace, m.id, n), m.status, m.title)
		}
		return resolvedBead{}, &AmbiguousIDError{Input: input, Candidates: candidates}
	}
}

func ResolveID(ctx context.Context, q Querier, input string) (string, error) {
	if input == "" {
		return "", &NotFoundError{Input: input}
	}

	n, err := LoadShortIDs(ctx, q)
	if err != nil {
		return "", err
	}

	namespacePart, rest, hasNamespace := splitNamespace(input)
	hexPart, aliasSteps, isAlias := splitAliasSteps(rest)
	if isAlias && aliasSteps == nil {
		return "", &AliasUnresolvedError{Input: input}
	}

	ri, err := resolveHexPrefix(ctx, q, input, NormalizeIDInput(hexPart), n)
	if err != nil {
		return "", err
	}
	id := ri.id
	if isAlias {
		if id, err = walkAliasSteps(ctx, q, input, ri.id, aliasSteps); err != nil {
			return "", err
		}
	}
	if hasNamespace && ri.namespace != namespacePart {
		current := DisplayID(ri.namespace, ri.id, n)
		if _, steps, ok := strings.Cut(rest, "."); ok {
			current += "." + steps
		}
		noticeStaleID(ctx, input, current)
	}
	return id, nil
}

func splitNamespace(input string) (string, string, bool) {
	before, after, ok := strings.Cut(input, "-")
	if !ok {
		return "", input, false
	}
	return before, after, true
}

func splitAliasSteps(rest string) (string, []int, bool) {
	parts := strings.Split(rest, ".")
	hexPrefix := parts[0]
	if len(parts) == 1 {
		return hexPrefix, nil, false
	}
	if hexPrefix == "" {
		return hexPrefix, nil, true
	}
	steps := make([]int, 0, len(parts)-1)
	for _, p := range parts[1:] {
		v, ok := parseAliasStep(p)
		if !ok {
			return hexPrefix, nil, true
		}
		steps = append(steps, v)
	}
	return hexPrefix, steps, true
}

func parseAliasStep(p string) (int, bool) {
	if p == "" || p[0] == '0' || len(p) > 9 {
		return 0, false
	}
	v := 0
	for _, c := range p {
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	return v, true
}

func walkAliasSteps(ctx context.Context, q Querier, input, root string, steps []int) (string, error) {
	current := root
	for depth, step := range steps {
		if depth >= maxAliasDepth {
			return "", &AliasUnresolvedError{Input: input}
		}
		children, err := rankedChildren(ctx, q, current)
		if err != nil {
			return "", err
		}
		if step > len(children) || !children[step-1].active {
			return "", &AliasUnresolvedError{Input: input}
		}
		current = children[step-1].id
	}
	return current, nil
}

type rankedChild struct {
	id     string
	active bool
}

func rankedChildren(ctx context.Context, q Querier, parent string) ([]rankedChild, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT bead_id, removed FROM dependencies
		WHERE depends_on_id = ? AND type = ?
		ORDER BY created_at ASC, bead_id ASC`, parent, ParentChildDepType)
	if err != nil {
		return nil, fmt.Errorf("resolve alias: query children: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []rankedChild
	for rows.Next() {
		var id string
		var removed bool
		if err := rows.Scan(&id, &removed); err != nil {
			return nil, fmt.Errorf("resolve alias: scan child: %w", err)
		}
		out = append(out, rankedChild{id: id, active: !removed})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve alias: iterate children: %w", err)
	}
	return out, nil
}
