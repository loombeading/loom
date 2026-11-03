// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"unicode"
)

const (
	SimilarLimit        = 5
	SimilarClosedLimit  = 3
	SimilarClosedWindow = "-30 days"
)

type SimilarMatch struct {
	Bead               Bead
	K, N               int
	Milestone          string
	MilestoneNamespace string
}

func SimilarWords(s string) map[string]struct{} {
	words := map[string]struct{}{}
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		if run[0] <= unicode.MaxASCII {
			if len(run) >= 2 {
				words[strings.ToLower(string(run))] = struct{}{}
			}
		} else {
			for i := 0; i+1 < len(run); i++ {
				words[string(run[i:i+2])] = struct{}{}
			}
		}
		run = run[:0]
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(run) > 0 && (run[0] <= unicode.MaxASCII) != (r <= unicode.MaxASCII) {
			flush()
		}
		run = append(run, r)
	}
	flush()
	return words
}

func SimilarBeads(ctx context.Context, q Querier, namespace, title, newID string) ([]SimilarMatch, error) {
	t := SimilarWords(title)
	if len(t) < 2 {
		return nil, nil
	}
	const scope = ` AND type != ? AND id != ?
		AND id NOT IN (SELECT depends_on_id FROM dependencies WHERE bead_id = ? AND removed = 0)`
	open, err := similarMatches(ctx, q, t, SimilarLimit, "SELECT "+listRowCols+` FROM beads
		WHERE namespace = ? AND status IN (?, ?)`+scope+listOrderClause,
		namespace, StatusOpen, StatusInProgress, BeadTypeGate, newID, newID)
	if err != nil {
		return nil, err
	}
	closed, err := similarMatches(ctx, q, t, SimilarClosedLimit, "SELECT "+listRowCols+` FROM beads
		WHERE namespace = ? AND status = ?
		AND closed_at >= (SELECT strftime('%Y-%m-%dT%H:%M:%fZ', created_at, ?) FROM beads WHERE id = ?)`+scope+listOrderClause,
		namespace, StatusClosed, SimilarClosedWindow, newID, BeadTypeGate, newID, newID)
	if err != nil {
		return nil, err
	}
	out := slices.Concat(open, closed)
	for i := range out {
		if out[i].Milestone, out[i].MilestoneNamespace, err = nearestMilestone(ctx, q, out[i].Bead.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func similarMatches(ctx context.Context, q Querier, t map[string]struct{}, limit int, query string, args ...any) ([]SimilarMatch, error) {
	beads, err := queryBeads(ctx, q, query, args, "")
	if err != nil {
		return nil, err
	}
	n := len(t)
	var out []SimilarMatch
	for _, b := range beads {
		c := SimilarWords(b.Title + "\n" + b.Description.String)
		k := 0
		for w := range t {
			if _, ok := c[w]; ok {
				k++
			}
		}
		if k >= 2 && 2*k >= n {
			out = append(out, SimilarMatch{Bead: b, K: k, N: n})
		}
	}
	slices.SortStableFunc(out, func(a, b SimilarMatch) int { return b.K - a.K })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func MilestoneExempt(ctx context.Context, q Querier, id string) (bool, error) {
	var exempt bool
	err := q.QueryRowContext(ctx, `SELECT COALESCE(labels, '') LIKE '%"' || ? || '"%' FROM beads WHERE id = ?`, ExemptMilestoneLabel, id).Scan(&exempt)
	return exempt, err
}

func nearestMilestone(ctx context.Context, q Querier, id string) (string, string, error) {
	var ms, ns string
	err := q.QueryRowContext(ctx, `
		WITH RECURSIVE anc(id, depth) AS (
			SELECT ?, 0
			UNION ALL
			SELECT d.depends_on_id, a.depth + 1 FROM dependencies d
			JOIN anc a ON d.bead_id = a.id
			WHERE d.type = ? AND d.removed = 0 AND a.depth < 64
		)
		SELECT b.id, b.namespace FROM anc a JOIN beads b ON b.id = a.id
		WHERE b.labels LIKE '%"' || ? || '%'
		ORDER BY a.depth LIMIT 1`, id, ParentChildDepType, milestoneLabelPrefix).Scan(&ms, &ns)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return ms, ns, err
}
