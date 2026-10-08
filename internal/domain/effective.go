// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"cmp"
	"context"
	"fmt"
)

type EffectivePriority struct {
	Priority int
	Source   string
}

const effBeadsCTE = `
		eff_beads(id, status, created_at, priority) AS (
			SELECT id, status, created_at,
			       CASE WHEN expedite_until > ? AND status IN (?, ?) THEN 0 ELSE priority END
			FROM beads
		)`

const reachLenders = `
	reach(id) AS (
		SELECT ?
		UNION
		SELECT d.bead_id FROM reach
		CROSS JOIN dependencies d ON d.depends_on_id = reach.id AND d.type = ? AND d.removed = 0
		CROSS JOIN eff_beads h ON h.id = reach.id CROSS JOIN eff_beads l ON l.id = d.bead_id
		WHERE h.status NOT IN (?, ?) AND l.status NOT IN (?, ?)
		UNION
		SELECT d.depends_on_id FROM reach
		CROSS JOIN dependencies d ON d.bead_id = reach.id AND d.type = ? AND d.removed = 0
		CROSS JOIN eff_beads h ON h.id = reach.id CROSS JOIN eff_beads l ON l.id = d.depends_on_id
		WHERE h.status NOT IN (?, ?) AND l.status NOT IN (?, ?)
	)`

func effBeadsArgs(now string) []any {
	return []any{now, StatusOpen, StatusInProgress}
}

func reachLendersArgs(now, id string, extra ...any) []any {
	return append(append(effBeadsArgs(now), id,
		BlocksDepType, StatusClosed, StatusCancelled, StatusClosed, StatusCancelled,
		ParentChildDepType, StatusClosed, StatusCancelled, StatusClosed, StatusCancelled,
	), extra...)
}

const SourceExpedite = "expedite"

func RaisedPriorities(ctx context.Context, q Querier, now string) (map[string]EffectivePriority, error) {
	now = nowOrDefault(now)
	if _, err := LoadCoefficients(ctx, q); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `
		WITH RECURSIVE `+effBeadsCTE+`, lend(id, p) AS (
			SELECT d.depends_on_id, l.priority FROM eff_beads l
			CROSS JOIN dependencies d ON d.bead_id = l.id AND d.type = ? AND d.removed = 0
			CROSS JOIN eff_beads h ON h.id = d.depends_on_id
			WHERE l.status IN (?, ?) AND h.status IN (?, ?) AND l.priority < h.priority
			UNION
			SELECT d.bead_id, l.priority FROM eff_beads l
			CROSS JOIN dependencies d ON d.depends_on_id = l.id AND d.type = ? AND d.removed = 0
			CROSS JOIN eff_beads h ON h.id = d.bead_id
			WHERE l.status IN (?, ?) AND h.status IN (?, ?) AND l.priority < h.priority
			UNION
			SELECT d.depends_on_id, lend.p FROM lend
			CROSS JOIN dependencies d ON d.bead_id = lend.id AND d.type = ? AND d.removed = 0
			CROSS JOIN eff_beads h ON h.id = d.depends_on_id
			WHERE h.status IN (?, ?) AND lend.p < h.priority
			UNION
			SELECT d.bead_id, lend.p FROM lend
			CROSS JOIN dependencies d ON d.depends_on_id = lend.id AND d.type = ? AND d.removed = 0
			CROSS JOIN eff_beads h ON h.id = d.bead_id
			WHERE h.status IN (?, ?) AND lend.p < h.priority
		)
		SELECT id, MIN(p) FROM lend GROUP BY id`,
		now, StatusOpen, StatusInProgress,
		BlocksDepType, StatusOpen, StatusInProgress, StatusOpen, StatusInProgress,
		ParentChildDepType, StatusOpen, StatusInProgress, StatusOpen, StatusInProgress,
		BlocksDepType, StatusOpen, StatusInProgress,
		ParentChildDepType, StatusOpen, StatusInProgress)
	if err != nil {
		return nil, fmt.Errorf("raised priorities: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]EffectivePriority{}
	for rows.Next() {
		var id string
		var e EffectivePriority
		if err := rows.Scan(&id, &e.Priority); err != nil {
			return nil, fmt.Errorf("raised priorities: scan: %w", err)
		}
		out[id] = e
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("raised priorities: iterate: %w", err)
	}
	if err := addOwnExpedites(ctx, q, now, out); err != nil {
		return nil, err
	}
	return out, nil
}

func addOwnExpedites(ctx context.Context, q Querier, now string, out map[string]EffectivePriority) error {
	rows, err := q.QueryContext(ctx, `SELECT id FROM beads WHERE expedite_until > ? AND status IN (?, ?)`,
		now, StatusOpen, StatusInProgress)
	if err != nil {
		return fmt.Errorf("raised priorities: own expedites: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("raised priorities: own expedites: scan: %w", err)
		}
		out[id] = EffectivePriority{Priority: 0, Source: SourceExpedite}
	}
	return rows.Err()
}

func FillSources(ctx context.Context, q Querier, now string, raised map[string]EffectivePriority, ids []string) error {
	now = nowOrDefault(now)
	for _, id := range ids {
		e, ok := raised[id]
		if !ok || e.Source != "" {
			continue
		}
		err := q.QueryRowContext(ctx, `
			WITH RECURSIVE `+effBeadsCTE+`, `+reachLenders+`
			SELECT i.id FROM reach JOIN eff_beads i ON i.id = reach.id
			WHERE i.priority = ? ORDER BY i.created_at ASC, i.id ASC LIMIT 1`,
			reachLendersArgs(now, id, e.Priority)...).Scan(&e.Source)
		if err != nil {
			return fmt.Errorf("effective priority source of %s: %w", id, err)
		}
		raised[id] = e
	}
	return nil
}

func EffectivePriorityOf(ctx context.Context, q Querier, now, id string) (EffectivePriority, bool, error) {
	now = nowOrDefault(now)
	var own int
	if err := q.QueryRowContext(ctx, `WITH `+effBeadsCTE+` SELECT priority FROM eff_beads WHERE id = ?`, append(effBeadsArgs(now), id)...).Scan(&own); err != nil {
		return EffectivePriority{}, false, fmt.Errorf("effective priority of %s: %w", id, err)
	}
	if own == 0 {
		return EffectivePriority{Priority: 0, Source: SourceExpedite}, true, nil
	}
	var e EffectivePriority
	err := q.QueryRowContext(ctx, `
		WITH RECURSIVE `+effBeadsCTE+`, `+reachLenders+`
		SELECT i.id, i.priority FROM reach JOIN eff_beads i ON i.id = reach.id
		ORDER BY i.priority ASC, i.created_at ASC, i.id ASC LIMIT 1`,
		reachLendersArgs(now, id)...).Scan(&e.Source, &e.Priority)
	if err != nil {
		return EffectivePriority{}, false, fmt.Errorf("effective priority of %s: %w", id, err)
	}
	if e.Priority >= own {
		return EffectivePriority{Priority: own}, false, nil
	}
	return e, true, nil
}

func ExpiredExpedites(ctx context.Context, q Querier, now string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM beads WHERE expedite_until <= ? AND status IN (?, ?)`,
		now, StatusOpen, StatusInProgress)
	if err != nil {
		return nil, fmt.Errorf("expired expedites: query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("expired expedites: scan: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

func effectiveOf(raised map[string]EffectivePriority, id string, stored int) int {
	if e, ok := raised[id]; ok {
		return e.Priority
	}
	return stored
}

func compareEffective(raised map[string]EffectivePriority, a, b Bead) int {
	return cmp.Or(
		cmp.Compare(effectiveOf(raised, a.ID, a.Priority), effectiveOf(raised, b.ID, b.Priority)),
		cmp.Compare(a.CreatedAt, b.CreatedAt),
		cmp.Compare(a.ID, b.ID),
	)
}
