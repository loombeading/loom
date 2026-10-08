// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
)

type DerivedMetric struct {
	BlastRadius int

	RetryCount int

	ReworkCount int

	TokenCost int

	ClaimedAt string

	DepsAfterClaim int

	ChildrenAfterClaim int
}

func DerivedMetrics(ctx context.Context, q Querier, ids []string) (map[string]DerivedMetric, error) {
	out := make(map[string]DerivedMetric, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := inPlaceholders(len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	blastRows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT d.depends_on_id, COUNT(*)
		FROM dependencies d
		JOIN beads b ON b.id = d.bead_id
		WHERE d.type = ? AND d.removed = 0 AND b.status NOT IN (?, ?)
		  AND d.depends_on_id IN (%s)
		GROUP BY d.depends_on_id`, placeholders),
		append([]any{BlocksDepType, StatusClosed, StatusCancelled}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("derived metrics: blast radius: %w", err)
	}
	if err := scanMetricCounts(blastRows, out, func(m *DerivedMetric, n int) { m.BlastRadius = n }); err != nil {
		return nil, fmt.Errorf("derived metrics: blast radius: %w", err)
	}

	retryRows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT bead_id, COUNT(*)
		FROM audit_log
		WHERE field IN (?, ?, ?) AND bead_id IN (%s)
		GROUP BY bead_id`, placeholders),
		append([]any{FieldRelease, FieldForceRelease, FieldClaimTakeover}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("derived metrics: retry count: %w", err)
	}
	if err := scanMetricCounts(retryRows, out, func(m *DerivedMetric, n int) { m.RetryCount = n }); err != nil {
		return nil, fmt.Errorf("derived metrics: retry count: %w", err)
	}

	reworkRows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT bead_id, COUNT(*)
		FROM audit_log
		WHERE field = ? AND bead_id IN (%s)
		GROUP BY bead_id`, placeholders),
		append([]any{FieldRework}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("derived metrics: rework count: %w", err)
	}
	if err := scanMetricCounts(reworkRows, out, func(m *DerivedMetric, n int) { m.ReworkCount = n }); err != nil {
		return nil, fmt.Errorf("derived metrics: rework count: %w", err)
	}

	tokenRows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT bead_id, COALESCE(SUM(tokens_in + tokens_out), 0)
		FROM token_costs
		WHERE bead_id IN (%s)
		GROUP BY bead_id`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("derived metrics: token cost: %w", err)
	}
	if err := scanMetricCounts(tokenRows, out, func(m *DerivedMetric, n int) { m.TokenCost = n }); err != nil {
		return nil, fmt.Errorf("derived metrics: token cost: %w", err)
	}

	scopeArgs := make([]any, 0, 3+len(args)+3)
	scopeArgs = append(scopeArgs, AuditKindField, FieldClaim, FieldForceClaim)
	scopeArgs = append(scopeArgs, args...)
	scopeArgs = append(scopeArgs, AuditKindField, FieldDependency,
		fmt.Sprintf(`%%"type":%q,"removed":false}`, BlocksDepType), ParentChildDepType)
	scopeRows, err := q.QueryContext(ctx, fmt.Sprintf(`
		WITH claimed AS (
			SELECT bead_id, MIN(occurred_at) AS t
			FROM audit_log
			WHERE kind = ? AND field IN (?, ?) AND bead_id IN (%s)
			GROUP BY bead_id
		)
		SELECT claimed.bead_id, claimed.t,
			COALESCE(deps.n, 0),
			COALESCE(children.n, 0)
		FROM claimed
		LEFT JOIN (
			SELECT a.bead_id, COUNT(*) AS n
			FROM audit_log a
			JOIN claimed c ON c.bead_id = a.bead_id
			WHERE a.kind = ? AND a.field = ? AND a.new_value LIKE ? AND a.occurred_at > c.t
			GROUP BY a.bead_id
		) deps ON deps.bead_id = claimed.bead_id
		LEFT JOIN (
			SELECT d.depends_on_id AS bead_id, COUNT(*) AS n
			FROM dependencies d
			JOIN beads child ON child.id = d.bead_id
			JOIN claimed c ON c.bead_id = d.depends_on_id
			WHERE d.type = ? AND d.removed = 0 AND child.created_at > c.t
			GROUP BY d.depends_on_id
		) children ON children.bead_id = claimed.bead_id`, placeholders),
		scopeArgs...)
	if err != nil {
		return nil, fmt.Errorf("derived metrics: scope growth: %w", err)
	}
	func() {
		defer func() { _ = scopeRows.Close() }()
		for scopeRows.Next() {
			var id, claimedAt string
			var depsAfter, childrenAfter int
			if scanErr := scopeRows.Scan(&id, &claimedAt, &depsAfter, &childrenAfter); scanErr != nil {
				err = fmt.Errorf("scan: %w", scanErr)
				return
			}
			m := out[id]
			m.ClaimedAt = claimedAt
			m.DepsAfterClaim = depsAfter
			m.ChildrenAfterClaim = childrenAfter
			out[id] = m
		}
		if rowsErr := scopeRows.Err(); rowsErr != nil {
			err = fmt.Errorf("iterate: %w", rowsErr)
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("derived metrics: scope growth: %w", err)
	}

	return out, nil
}

func scanMetricCounts(rows *sql.Rows, out map[string]DerivedMetric, set func(m *DerivedMetric, n int)) error {
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		m := out[id]
		set(&m, n)
		out[id] = m
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate: %w", err)
	}
	return nil
}
