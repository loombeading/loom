// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const flowTimeFormat = "2006-01-02T15:04:05.000Z"

type FlowRecord struct {
	ID        string
	CreatedAt time.Time
	ClosedAt  time.Time
	ClaimedAt time.Time
	ReadyAt   time.Time
	Reopened  bool
}

func FlowRecords(ctx context.Context, q Querier, from, to time.Time, namespace string) ([]FlowRecord, error) {
	window := `
		SELECT id FROM beads
		WHERE status = ? AND closed_at > ? AND closed_at <= ? AND type NOT IN (?, 'epic')`
	args := []any{StatusClosed, from.UTC().Format(flowTimeFormat), to.UTC().Format(flowTimeFormat), BeadTypeGate}
	if namespace != "" {
		window += ` AND namespace = ?`
		args = append(args, namespace)
	}

	rows, err := q.QueryContext(ctx, `
		WITH w AS (`+window+`)
		SELECT b.id, b.created_at, b.closed_at,
			(SELECT MIN(a.occurred_at) FROM audit_log a
				WHERE a.bead_id = b.id AND a.kind = ? AND a.field IN (?, ?)),
			EXISTS(SELECT 1 FROM audit_log a
				WHERE a.bead_id = b.id AND a.kind = ? AND a.field = ?)
		FROM beads b JOIN w ON w.id = b.id
		ORDER BY b.id`,
		append(append([]any{}, args...), AuditKindField, FieldClaim, FieldForceClaim, AuditKindField, FieldReopen)...)
	if err != nil {
		return nil, fmt.Errorf("flow records: %w", err)
	}
	records, err := collectRows(rows, scanFlowRecord)
	if err != nil {
		return nil, fmt.Errorf("flow records: %w", err)
	}
	if len(records) == 0 {
		return records, nil
	}

	candRows, err := q.QueryContext(ctx, `
		WITH w AS (`+window+`)
		SELECT a.bead_id, a.occurred_at FROM audit_log a
			WHERE a.kind = ? AND a.field = ? AND a.bead_id IN (SELECT id FROM w)
		UNION ALL
		SELECT d.bead_id, p.closed_at FROM dependencies d
			JOIN beads p ON p.id = d.depends_on_id
			WHERE d.type = ? AND p.closed_at IS NOT NULL AND d.bead_id IN (SELECT id FROM w)
		UNION ALL
		SELECT d.bead_id, d.removed_set_at FROM dependencies d
			WHERE d.type = ? AND d.removed = 1 AND d.removed_set_at IS NOT NULL AND d.bead_id IN (SELECT id FROM w)`,
		append(append([]any{}, args...), AuditKindField, FieldReopen, BlocksDepType, BlocksDepType)...)
	if err != nil {
		return nil, fmt.Errorf("flow records: ready candidates: %w", err)
	}
	cands, err := collectRows(candRows, func(r *sql.Rows, c *flowCandidate) error {
		return r.Scan(&c.id, &c.at)
	})
	if err != nil {
		return nil, fmt.Errorf("flow records: ready candidates: %w", err)
	}

	index := make(map[string]int, len(records))
	for i, r := range records {
		index[r.ID] = i
	}
	for _, c := range cands {
		i, ok := index[c.id]
		if !ok || records[i].ClaimedAt.IsZero() {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, c.at)
		if err != nil {
			return nil, fmt.Errorf("flow records: ready candidate time %q: %w", c.at, err)
		}
		if at.After(records[i].ClaimedAt) {
			continue
		}
		if at.After(records[i].ReadyAt) {
			records[i].ReadyAt = at
		}
	}
	return records, nil
}

type flowCandidate struct {
	id string
	at string
}

func scanFlowRecord(rows *sql.Rows, r *FlowRecord) error {
	var created, closed string
	var claimed sql.NullString
	if err := rows.Scan(&r.ID, &created, &closed, &claimed, &r.Reopened); err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	var err error
	if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return fmt.Errorf("created_at %q: %w", created, err)
	}
	if r.ClosedAt, err = time.Parse(time.RFC3339Nano, closed); err != nil {
		return fmt.Errorf("closed_at %q: %w", closed, err)
	}
	if claimed.Valid {
		if r.ClaimedAt, err = time.Parse(time.RFC3339Nano, claimed.String); err != nil {
			return fmt.Errorf("claimed_at %q: %w", claimed.String, err)
		}
		r.ReadyAt = r.CreatedAt
	}
	return nil
}
