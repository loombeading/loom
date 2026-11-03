// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"cmp"
	"context"
	"fmt"
	"time"
)

type EffectivePriority struct {
	Priority int
	Source   string
}

const effBeadsCTE = `
		eff_beads(id, status, created_at, priority) AS (
			SELECT id, status, created_at,
			       CASE WHEN expedite_until > ? AND status IN (?, ?) THEN 0
			            WHEN due_at IS NOT NULL AND due_at <= ? AND status IN (?, ?) THEN MIN(priority, 1)
			            ELSE priority END
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

func dueCutoff(ctx context.Context, q Querier, now string) (string, error) {
	coef, err := LoadCoefficients(ctx, q)
	if err != nil {
		return "", err
	}
	nowT, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		return "", fmt.Errorf("due cutoff: now %q: %w", now, err)
	}
	return FormatTime(nowT.Add(coef.Duration("due_lead"))), nil
}

func effBeadsArgs(now, dueBefore string) []any {
	return []any{now, StatusOpen, StatusInProgress, dueBefore, StatusOpen, StatusInProgress}
}

func reachLendersArgs(now, dueBefore, id string, extra ...any) []any {
	return append(append(effBeadsArgs(now, dueBefore), id,
		BlocksDepType, StatusClosed, StatusCancelled, StatusClosed, StatusCancelled,
		ParentChildDepType, StatusClosed, StatusCancelled, StatusClosed, StatusCancelled,
	), extra...)
}

const (
	SourceExpedite = "expedite"
	SourceDue      = "due"
)

func RaisedPriorities(ctx context.Context, q Querier, now string) (map[string]EffectivePriority, error) {
	now = nowOrDefault(now)
	dueBefore, err := dueCutoff(ctx, q, now)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `
		WITH RECURSIVE expedited(id) AS MATERIALIZED (
			SELECT id FROM beads WHERE expedite_until > ? AND status IN (?, ?)
		), dued(id) AS MATERIALIZED (
			SELECT id FROM beads WHERE due_at IS NOT NULL AND due_at <= ? AND status IN (?, ?)
		), lend(id, p) AS (
			SELECT d.depends_on_id, l.priority FROM beads l
			CROSS JOIN dependencies d ON d.bead_id = l.id AND d.type = ? AND d.removed = 0
			CROSS JOIN beads h ON h.id = d.depends_on_id
			WHERE l.status IN (?, ?) AND h.status IN (?, ?) AND l.priority < h.priority
			UNION
			SELECT d.bead_id, l.priority FROM beads l
			CROSS JOIN dependencies d ON d.depends_on_id = l.id AND d.type = ? AND d.removed = 0
			CROSS JOIN beads h ON h.id = d.bead_id
			WHERE l.status IN (?, ?) AND h.status IN (?, ?) AND l.priority < h.priority
			UNION
			SELECT d.depends_on_id, 0 FROM expedited l
			CROSS JOIN dependencies d ON d.bead_id = l.id AND d.type = ? AND d.removed = 0
			CROSS JOIN beads h ON h.id = d.depends_on_id
			WHERE h.status IN (?, ?)
			UNION
			SELECT d.bead_id, 0 FROM expedited l
			CROSS JOIN dependencies d ON d.depends_on_id = l.id AND d.type = ? AND d.removed = 0
			CROSS JOIN beads h ON h.id = d.bead_id
			WHERE h.status IN (?, ?)
			UNION
			SELECT d.depends_on_id, 1 FROM dued l
			CROSS JOIN dependencies d ON d.bead_id = l.id AND d.type = ? AND d.removed = 0
			CROSS JOIN beads h ON h.id = d.depends_on_id
			WHERE h.status IN (?, ?) AND 1 < h.priority
			UNION
			SELECT d.bead_id, 1 FROM dued l
			CROSS JOIN dependencies d ON d.depends_on_id = l.id AND d.type = ? AND d.removed = 0
			CROSS JOIN beads h ON h.id = d.bead_id
			WHERE h.status IN (?, ?) AND 1 < h.priority
			UNION
			SELECT d.depends_on_id, lend.p FROM lend
			CROSS JOIN dependencies d ON d.bead_id = lend.id AND d.type = ? AND d.removed = 0
			CROSS JOIN beads h ON h.id = d.depends_on_id
			WHERE h.status IN (?, ?) AND lend.p < h.priority
			UNION
			SELECT d.bead_id, lend.p FROM lend
			CROSS JOIN dependencies d ON d.depends_on_id = lend.id AND d.type = ? AND d.removed = 0
			CROSS JOIN beads h ON h.id = d.bead_id
			WHERE h.status IN (?, ?) AND lend.p < h.priority
		)
		SELECT id, MIN(p) FROM lend GROUP BY id`,
		now, StatusOpen, StatusInProgress,
		dueBefore, StatusOpen, StatusInProgress,
		BlocksDepType, StatusOpen, StatusInProgress, StatusOpen, StatusInProgress,
		ParentChildDepType, StatusOpen, StatusInProgress, StatusOpen, StatusInProgress,
		BlocksDepType, StatusOpen, StatusInProgress,
		ParentChildDepType, StatusOpen, StatusInProgress,
		BlocksDepType, StatusOpen, StatusInProgress,
		ParentChildDepType, StatusOpen, StatusInProgress,
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
	if err := addOwnDues(ctx, q, dueBefore, out); err != nil {
		return nil, err
	}
	if err := addOwnExpedites(ctx, q, now, out); err != nil {
		return nil, err
	}
	return out, nil
}

func addOwnDues(ctx context.Context, q Querier, dueBefore string, out map[string]EffectivePriority) error {
	rows, err := q.QueryContext(ctx, `SELECT id FROM beads WHERE due_at IS NOT NULL AND due_at <= ? AND status IN (?, ?) AND priority > 1`,
		dueBefore, StatusOpen, StatusInProgress)
	if err != nil {
		return fmt.Errorf("raised priorities: own dues: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("raised priorities: own dues: scan: %w", err)
		}
		if e, ok := out[id]; ok && e.Priority < 1 {
			continue
		}
		out[id] = EffectivePriority{Priority: 1, Source: SourceDue}
	}
	return rows.Err()
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
	dueBefore, err := dueCutoff(ctx, q, now)
	if err != nil {
		return err
	}
	for _, id := range ids {
		e, ok := raised[id]
		if !ok || e.Source != "" {
			continue
		}
		err := q.QueryRowContext(ctx, `
			WITH RECURSIVE `+effBeadsCTE+`, `+reachLenders+`
			SELECT i.id FROM reach JOIN eff_beads i ON i.id = reach.id
			WHERE i.priority = ? ORDER BY i.created_at ASC, i.id ASC LIMIT 1`,
			reachLendersArgs(now, dueBefore, id, e.Priority)...).Scan(&e.Source)
		if err != nil {
			return fmt.Errorf("effective priority source of %s: %w", id, err)
		}
		raised[id] = e
	}
	return nil
}

func EffectivePriorityOf(ctx context.Context, q Querier, now, id string) (EffectivePriority, bool, error) {
	now = nowOrDefault(now)
	dueBefore, err := dueCutoff(ctx, q, now)
	if err != nil {
		return EffectivePriority{}, false, err
	}
	var own, stored int
	if err := q.QueryRowContext(ctx, `WITH `+effBeadsCTE+` SELECT e.priority, b.priority FROM eff_beads e JOIN beads b ON b.id = e.id WHERE e.id = ?`, append(effBeadsArgs(now, dueBefore), id)...).Scan(&own, &stored); err != nil {
		return EffectivePriority{}, false, fmt.Errorf("effective priority of %s: %w", id, err)
	}
	if own == 0 {
		return EffectivePriority{Priority: 0, Source: SourceExpedite}, true, nil
	}
	var e EffectivePriority
	err = q.QueryRowContext(ctx, `
		WITH RECURSIVE `+effBeadsCTE+`, `+reachLenders+`
		SELECT i.id, i.priority FROM reach JOIN eff_beads i ON i.id = reach.id
		ORDER BY i.priority ASC, i.created_at ASC, i.id ASC LIMIT 1`,
		reachLendersArgs(now, dueBefore, id)...).Scan(&e.Source, &e.Priority)
	if err != nil {
		return EffectivePriority{}, false, fmt.Errorf("effective priority of %s: %w", id, err)
	}
	if e.Priority >= own {
		if own < stored {
			return EffectivePriority{Priority: own, Source: SourceDue}, true, nil
		}
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

func compareAdjusted(adj TimeAdjustment, a, b Bead) int {
	return compareEffective(adj.Eff, adj.Demand, a, b)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

func compareEffective(raised map[string]EffectivePriority, demand map[string]bool, a, b Bead) int {
	return cmp.Or(
		cmp.Compare(effectiveOf(raised, a.ID, a.Priority), effectiveOf(raised, b.ID, b.Priority)),
		cmp.Compare(boolRank(!demand[a.ID]), boolRank(!demand[b.ID])),
		cmp.Compare(a.CreatedAt, b.CreatedAt),
		cmp.Compare(a.ID, b.ID),
	)
}
