// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

func Alias(ctx context.Context, q Querier, canonical string) (string, error) {
	r, err := NewAliasResolverFor(ctx, q, []string{canonical})
	if err != nil {
		return "", err
	}
	alias, hasParent, err := r.Alias(canonical)
	if err != nil {
		return "", err
	}
	if hasParent {
		return alias, nil
	}
	return DisplayID(r.namespace[canonical], canonical, r.n), nil
}

type AliasResolver struct {
	parent    map[string]string
	rank      map[string]int
	namespace map[string]string
	n         ShortIDs
}

func NewAliasResolverFor(ctx context.Context, q Querier, ids []string) (*AliasResolver, error) {
	n, err := LoadShortIDs(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &AliasResolver{parent: map[string]string{}, rank: map[string]int{}, namespace: map[string]string{}, n: n}, nil
	}

	seedPlaceholders := valuesPlaceholders(len(ids))
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, ParentChildDepType)

	ancestorRows, err := q.QueryContext(ctx, fmt.Sprintf(`
		WITH RECURSIVE seed(id) AS (VALUES %s),
		ancestors(id) AS (
			SELECT id FROM seed
			UNION
			SELECT d.depends_on_id FROM dependencies d
			JOIN ancestors a ON d.bead_id = a.id
			WHERE d.type = ? AND d.removed = 0
		)
		SELECT id FROM ancestors`, seedPlaceholders), args...)
	if err != nil {
		return nil, fmt.Errorf("alias resolver: query ancestors: %w", err)
	}
	ancestorIDs, err := collectRows(ancestorRows, scanString)
	if err != nil {
		return nil, fmt.Errorf("alias resolver: iterate ancestors: %w", err)
	}

	ancestorPlaceholders := inPlaceholders(len(ancestorIDs))
	ancestorArgs := make([]any, len(ancestorIDs))
	for i, id := range ancestorIDs {
		ancestorArgs[i] = id
	}

	namespaceRows, err := q.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, namespace FROM beads WHERE id IN (%s)`, ancestorPlaceholders), ancestorArgs...)
	if err != nil {
		return nil, fmt.Errorf("alias resolver: query prefixes: %w", err)
	}
	namespace, err := scanNamespaceRows(namespaceRows)
	if err != nil {
		return nil, err
	}

	linkArgs := append([]any{ParentChildDepType}, ancestorArgs...)
	depRows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT bead_id, depends_on_id, removed, created_at FROM dependencies
		WHERE type = ? AND depends_on_id IN (%s)
		ORDER BY created_at ASC, bead_id ASC`, ancestorPlaceholders), linkArgs...)
	if err != nil {
		return nil, fmt.Errorf("alias resolver: query links: %w", err)
	}
	parent, rank, err := scanParentRankLinks(depRows)
	if err != nil {
		return nil, err
	}

	return &AliasResolver{parent: parent, rank: rank, namespace: namespace, n: n}, nil
}

func valuesPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("(?),", n), ",")
}

func inPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func scanNamespaceRows(rows *sql.Rows) (map[string]string, error) {
	defer func() { _ = rows.Close() }()
	namespace := map[string]string{}
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, fmt.Errorf("alias resolver: scan namespace: %w", err)
		}
		namespace[id] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("alias resolver: iterate prefixes: %w", err)
	}
	return namespace, nil
}

func scanParentRankLinks(rows *sql.Rows) (map[string]string, map[string]int, error) {
	defer func() { _ = rows.Close() }()
	type link struct {
		child, parentID, createdAt string
		removed                    bool
	}
	var links []link
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.child, &l.parentID, &l.removed, &l.createdAt); err != nil {
			return nil, nil, fmt.Errorf("alias resolver: scan link: %w", err)
		}
		links = append(links, l)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("alias resolver: iterate links: %w", err)
	}

	winner := map[string]string{}
	winnerCreatedAt := map[string]string{}
	for _, l := range links {
		if l.removed {
			continue
		}
		cur, ok := winner[l.child]
		if !ok || l.createdAt < winnerCreatedAt[l.child] ||
			(l.createdAt == winnerCreatedAt[l.child] && l.parentID < cur) {
			winner[l.child] = l.parentID
			winnerCreatedAt[l.child] = l.createdAt
		}
	}

	childrenByParent := map[string][]link{}
	for _, l := range links {
		childrenByParent[l.parentID] = append(childrenByParent[l.parentID], l)
	}
	parent := map[string]string{}
	rank := map[string]int{}
	for p, kids := range childrenByParent {
		for i, l := range kids {
			if l.removed {
				continue
			}
			if winner[l.child] != p {
				continue
			}
			parent[l.child] = p
			rank[l.child] = i + 1
		}
	}
	return parent, rank, nil
}

func (r *AliasResolver) Parent(canonical string) (string, bool) {
	p, ok := r.parent[canonical]
	return p, ok
}

func (r *AliasResolver) Alias(canonical string) (string, bool, error) {
	var steps []int
	current := canonical
	for depth := 0; ; depth++ {
		if depth >= maxAliasDepth {
			return "", false, fmt.Errorf("alias: parent chain from %s exceeds depth limit %d (possible cycle; cycle detection is out of scope here)", canonical, maxAliasDepth)
		}
		p, ok := r.parent[current]
		if !ok {
			break
		}
		steps = append(steps, r.rank[current])
		current = p
	}
	if len(steps) == 0 {
		return "", false, nil
	}

	display := DisplayID(r.namespace[current], current, r.n)
	var sb strings.Builder
	sb.WriteString(display)
	for _, step := range slices.Backward(steps) {
		fmt.Fprintf(&sb, ".%d", step)
	}
	return sb.String(), true, nil
}
