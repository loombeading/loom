// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type EstimateBead struct {
	ID        string
	Namespace string
	Title     string
	BeadType  string
	Priority  int
	Status    string

	Depth int
}

type EstimateDep struct {
	BeadID      string
	DependsOnID string
	Type        string
}

func EstimateInput(ctx context.Context, q Querier, readyFilter ReadyFilter) (map[string]EstimateBead, []string, []EstimateDep, map[string]int, []string, error) {
	beads, order, err := estimateBeads(ctx, q)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	deps, err := estimateDeps(ctx, q)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	tokenByBeadID, err := estimateTokens(ctx, q)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	readyBeads, _, err := ReadyBeads(ctx, q, readyFilter)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("estimate input: ready beads: %w", err)
	}
	readyIDs := make([]string, len(readyBeads))
	for i, b := range readyBeads {
		readyIDs[i] = b.ID
	}

	return beads, order, deps, tokenByBeadID, readyIDs, nil
}

func estimateBeads(ctx context.Context, q Querier) (map[string]EstimateBead, []string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, title, type, priority, status, namespace, reasoning_depth FROM beads`+listOrderClause)
	if err != nil {
		return nil, nil, fmt.Errorf("estimate input: query beads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	beads := map[string]EstimateBead{}
	var order []string
	for rows.Next() {
		var iss EstimateBead
		var depthVal sql.NullInt64
		if err := rows.Scan(&iss.ID, &iss.Title, &iss.BeadType, &iss.Priority, &iss.Status, &iss.Namespace, &depthVal); err != nil {
			return nil, nil, fmt.Errorf("estimate input: scan bead: %w", err)
		}
		if depthVal.Valid {
			iss.Depth = int(depthVal.Int64)
		}
		beads[iss.ID] = iss
		order = append(order, iss.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("estimate input: iterate beads: %w", err)
	}
	return beads, order, nil
}

func estimateDeps(ctx context.Context, q Querier) ([]EstimateDep, error) {
	rows, err := q.QueryContext(ctx, `SELECT bead_id, depends_on_id, type FROM dependencies WHERE removed = 0`)
	if err != nil {
		return nil, fmt.Errorf("estimate input: query dependencies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var deps []EstimateDep
	for rows.Next() {
		var d EstimateDep
		if err := rows.Scan(&d.BeadID, &d.DependsOnID, &d.Type); err != nil {
			return nil, fmt.Errorf("estimate input: scan dependency: %w", err)
		}
		deps = append(deps, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("estimate input: iterate dependencies: %w", err)
	}
	return deps, nil
}

func estimateTokens(ctx context.Context, q Querier) (map[string]int, error) {
	rows, err := q.QueryContext(ctx, `SELECT bead_id, tokens_in, tokens_out FROM token_costs`)
	if err != nil {
		return nil, fmt.Errorf("estimate input: query token_costs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	tokenByBeadID := map[string]int{}
	for rows.Next() {
		var beadID string
		var tokensIn, tokensOut int
		if err := rows.Scan(&beadID, &tokensIn, &tokensOut); err != nil {
			return nil, fmt.Errorf("estimate input: scan token_cost: %w", err)
		}
		tokenByBeadID[beadID] += tokensIn + tokensOut
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("estimate input: iterate token_costs: %w", err)
	}
	return tokenByBeadID, nil
}

type DayTotal struct {
	Date   string
	Tokens int
}

type TokenCostEntry struct {
	BeadID     string
	RecordedAt time.Time
	Tokens     int
}

func TokenCostEntries(ctx context.Context, q Querier, namespace string) ([]TokenCostEntry, error) {
	query := `SELECT bead_id, recorded_at, tokens_in, tokens_out FROM token_costs`
	var args []any
	if namespace != "" {
		query = `SELECT token_costs.bead_id, token_costs.recorded_at, token_costs.tokens_in, token_costs.tokens_out
			FROM token_costs
			JOIN beads ON beads.id = token_costs.bead_id
			WHERE beads.namespace = ?`
		args = append(args, namespace)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("token cost entries: query: %w", err)
	}
	var out []TokenCostEntry
	func() {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var e TokenCostEntry
			var recordedAt string
			var tokensIn, tokensOut int
			if scanErr := rows.Scan(&e.BeadID, &recordedAt, &tokensIn, &tokensOut); scanErr != nil {
				err = fmt.Errorf("token cost entries: scan: %w", scanErr)
				return
			}
			t, parseErr := time.Parse(time.RFC3339, recordedAt)
			if parseErr != nil {
				err = fmt.Errorf("token cost entries: parse recorded_at %q: %w", recordedAt, parseErr)
				return
			}
			e.RecordedAt, e.Tokens = t, tokensIn+tokensOut
			out = append(out, e)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			err = fmt.Errorf("token cost entries: iterate: %w", rowsErr)
		}
	}()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func DailyTokenTotals(ctx context.Context, q Querier, now time.Time, namespace string) ([]DayTotal, error) {
	entries, err := TokenCostEntries(ctx, q, namespace)
	if err != nil {
		return nil, err
	}
	totals := map[string]int{}
	for _, e := range entries {
		if e.RecordedAt.Before(now) {
			totals[e.RecordedAt.UTC().Format("2006-01-02")] += e.Tokens
		}
	}
	out := make([]DayTotal, 0, len(totals))
	for day, tokens := range totals {
		out = append(out, DayTotal{Date: day, Tokens: tokens})
	}
	return out, nil
}

type ThroughputBead struct {
	ID        string
	CreatedAt time.Time
	ClosedAt  time.Time
	Closed    bool
	HasPR     bool
}

func ThroughputInput(ctx context.Context, q Querier, namespace string) ([]ThroughputBead, error) {
	beads, _, err := ListBeads(ctx, q, ListFilter{All: true, Priority: -1, Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("throughput input: list beads: %w", err)
	}
	out := make([]ThroughputBead, 0, len(beads))
	for _, b := range beads {
		if b.BeadType == BeadTypeGate {
			continue
		}
		createdAt, perr := time.Parse(time.RFC3339, b.CreatedAt)
		if perr != nil {
			return nil, fmt.Errorf("throughput input: parse created_at %q: %w", b.CreatedAt, perr)
		}
		tb := ThroughputBead{ID: b.ID, CreatedAt: createdAt, Closed: b.Status == StatusClosed}
		if b.ClosedAt.Valid {
			closedAt, perr := time.Parse(time.RFC3339, b.ClosedAt.String)
			if perr != nil {
				return nil, fmt.Errorf("throughput input: parse closed_at %q: %w", b.ClosedAt.String, perr)
			}
			tb.ClosedAt = closedAt
		}
		for _, ref := range b.ExternalRefs {
			if strings.Contains(ref, "/pull/") {
				tb.HasPR = true
				break
			}
		}
		out = append(out, tb)
	}
	return out, nil
}
