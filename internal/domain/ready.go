// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

type ReadyFilter struct {
	Type      string
	Priority  int
	ClaimedBy string
	Label     string
	Namespace string

	ParentID      string
	UpdatedBefore string
	UpdatedAfter  string

	Limit int

	Now string

	TimeAdjusted bool
}

const blockedExistsClause = `
	(
		EXISTS (
			SELECT 1 FROM dependencies d
			JOIN beads b ON b.id = d.depends_on_id
			WHERE d.bead_id = i.id AND d.type = ? AND d.removed = 0 AND b.status NOT IN (?, ?)
		)
		OR EXISTS (
			SELECT 1 FROM dependencies pc
			JOIN beads c ON c.id = pc.bead_id
			WHERE pc.depends_on_id = i.id AND pc.type = ? AND pc.removed = 0
			  AND c.status NOT IN (?, ?)
		)
	)`

func readyBaseFromWhere(blocked bool) string {
	neg := "NOT "
	if blocked {
		neg = ""
	}
	return fmt.Sprintf(`FROM beads i WHERE status = ? AND %s%s`, neg, blockedExistsClause)
}

func readyArgs(status string) []any {
	return []any{status, BlocksDepType, StatusClosed, StatusCancelled, ParentChildDepType, StatusClosed, StatusCancelled}
}

func applyReadyFilter(query string, args []any, f ReadyFilter) (string, []any) {
	if f.Type != "" {
		query += ` AND type = ?`
		args = append(args, f.Type)
	}
	if f.Priority >= 0 {
		query += ` AND priority = ?`
		args = append(args, f.Priority)
	}
	if f.ClaimedBy != "" {
		query += ` AND claimed_by = ?`
		args = append(args, f.ClaimedBy)
	}
	query, args = commonListFilterConds(query, args, f.Namespace, f.ParentID, readyDirectChildOfClause, f.UpdatedBefore, f.UpdatedAfter)
	return query, args
}

const readyDirectChildOfClause = `
	EXISTS (
		SELECT 1 FROM dependencies pc
		WHERE pc.bead_id = i.id AND pc.depends_on_id = ? AND pc.type = ? AND pc.removed = 0
	)`

func queryBeads(ctx context.Context, q Querier, query string, args []any, label string) ([]Bead, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query beads: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Bead
	for rows.Next() {
		var b Bead
		var labels, externalRefs sql.NullString
		if err := rows.Scan(&b.ID, &b.Namespace, &b.Title, &b.Description, &b.Status, &b.Priority, &b.BeadType,
			&b.ClaimedBy, &labels, &b.CreatedAt, &b.UpdatedAt, &b.ClosedAt,
			&b.Depth, &externalRefs); err != nil {
			return nil, fmt.Errorf("query beads: scan: %w", err)
		}
		b.Labels, err = decodeStringList(labels)
		if err != nil {
			return nil, err
		}
		b.ExternalRefs, err = decodeStringList(externalRefs)
		if err != nil {
			return nil, err
		}
		if label != "" && !slices.Contains(b.Labels, label) {
			continue
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query beads: iterate: %w", err)
	}
	return out, nil
}

func ReadyBeads(ctx context.Context, q Querier, f ReadyFilter) ([]Bead, int, error) {
	beads, _, total, err := ReadyBeadsEffective(ctx, q, f)
	return beads, total, err
}

type ReadyResult struct {
	Beads    []Bead
	Eff      map[string]EffectivePriority
	Demand   map[string]bool
	Total    int
	Held     int
	Adjusted bool
}

func ReadyBeadsEffective(ctx context.Context, q Querier, f ReadyFilter) ([]Bead, map[string]EffectivePriority, int, error) {
	r, err := ReadyList(ctx, q, f)
	return r.Beads, r.Eff, r.Total, err
}

func ReadyList(ctx context.Context, q Querier, f ReadyFilter) (ReadyResult, error) {
	fromWhere, args := ReadyWhere(f)

	now := nowOrDefault(f.Now)
	coef, err := LoadCoefficients(ctx, q)
	if err != nil {
		return ReadyResult{}, err
	}
	adjusted := f.TimeAdjusted || coef[CoefficientTimeAdjust] == TimeAdjustOn
	raised, err := RaisedPriorities(ctx, q, now)
	if err != nil {
		return ReadyResult{}, err
	}

	keys, err := queryBeads(ctx, q, "SELECT "+readyKeyCols+" "+fromWhere, args, f.Label)
	if err != nil {
		return ReadyResult{}, err
	}
	res := ReadyResult{Eff: raised, Adjusted: adjusted}
	filtered := keys
	if adjusted {
		adj, err := TimeAdjust(ctx, q, now, coef, raised, keys)
		if err != nil {
			return ReadyResult{}, err
		}
		res.Eff, res.Demand, res.Held = adj.Eff, adj.Demand, len(adj.Held)
		filtered = slices.DeleteFunc(keys, func(b Bead) bool { return adj.Held[b.ID] })
		slices.SortFunc(filtered, func(a, b Bead) int { return compareAdjusted(adj, a, b) })
	} else {
		res.Demand, err = CandidateDemand(ctx, q, now, coef, withinLimitTiers(raised, keys, f.Limit))
		if err != nil {
			return ReadyResult{}, err
		}
		slices.SortFunc(filtered, func(a, b Bead) int { return compareEffective(raised, res.Demand, a, b) })
	}

	res.Total = len(filtered)
	if f.Limit > 0 && f.Limit < res.Total {
		filtered = filtered[:f.Limit]
	}
	res.Beads, err = beadsInOrder(ctx, q, filtered)
	return res, err
}

func withinLimitTiers(raised map[string]EffectivePriority, keys []Bead, limit int) []Bead {
	if limit <= 0 || limit >= len(keys) {
		return keys
	}
	prios := make([]int, len(keys))
	for i, b := range keys {
		prios[i] = effectiveOf(raised, b.ID, b.Priority)
	}
	cutoff := slices.Sorted(slices.Values(prios))[limit-1]
	out := make([]Bead, 0, limit)
	for i, b := range keys {
		if prios[i] <= cutoff {
			out = append(out, b)
		}
	}
	return out
}

const readyKeyCols = `id, namespace, '', '', status, priority, type, claimed_by, labels, created_at, updated_at, closed_at, reasoning_depth, NULL`

func beadsInOrder(ctx context.Context, q Querier, keys []Bead) ([]Bead, error) {
	args := make([]any, len(keys))
	pos := make(map[string]int, len(keys))
	for i, k := range keys {
		args[i] = k.ID
		pos[k.ID] = i
	}
	beads, err := queryBeads(ctx, q, "SELECT "+listRowCols+" FROM beads WHERE id IN ("+inPlaceholders(len(keys))+")", args, "")
	if err != nil {
		return nil, err
	}
	slices.SortFunc(beads, func(a, b Bead) int { return pos[a.ID] - pos[b.ID] })
	return beads, nil
}

func ReadyWhere(f ReadyFilter) (string, []any) {
	fromWhere := readyBaseFromWhere(false)
	args := readyArgs(StatusOpen)
	fromWhere, args = applyReadyFilter(fromWhere, args, f)
	fromWhere += ` AND type != ?`
	args = append(args, BeadTypeGate)
	fromWhere += abolishedAncestorClause
	args = append(args, ParentChildDepType, ParentChildDepType, AbolishedByLabelPrefix)
	return fromWhere, args
}

const abolishedAncestorClause = ` AND NOT EXISTS (
	WITH RECURSIVE anc(id) AS (
		SELECT d.depends_on_id FROM dependencies d
		WHERE d.bead_id = i.id AND d.type = ? AND d.removed = 0
		UNION
		SELECT d.depends_on_id FROM dependencies d
		JOIN anc a ON d.bead_id = a.id
		WHERE d.type = ? AND d.removed = 0
	)
	SELECT 1 FROM beads b JOIN anc a ON b.id = a.id
	WHERE b.labels LIKE '%"' || ? || '%'
)`

type Blocker struct {
	ID        string
	Namespace string
	IsGate    bool

	Text string
}

type BlockedBead struct {
	Bead

	Blockers []Blocker

	HeldSince string
}

func heldReadyBeads(ctx context.Context, q Querier, f ReadyFilter, now string, coef Coefficients, raised map[string]EffectivePriority) (TimeAdjustment, []Bead, error) {
	fromWhere, args := ReadyWhere(f)
	keys, err := queryBeads(ctx, q, "SELECT "+readyKeyCols+" "+fromWhere, args, f.Label)
	if err != nil {
		return TimeAdjustment{}, nil, err
	}
	adj, err := TimeAdjust(ctx, q, now, coef, raised, keys)
	if err != nil {
		return TimeAdjustment{}, nil, err
	}
	held := slices.DeleteFunc(keys, func(b Bead) bool { return !adj.Held[b.ID] })
	beads, err := beadsInOrder(ctx, q, held)
	return adj, beads, err
}

func BlockedBeadsEffective(ctx context.Context, q Querier, f ReadyFilter) ([]BlockedBead, map[string]EffectivePriority, int, error) {
	fromWhere := readyBaseFromWhere(true)
	args := readyArgs(StatusOpen)
	fromWhere, args = applyReadyFilter(fromWhere, args, f)
	fromWhere += ` AND type != ?`
	args = append(args, BeadTypeGate)

	now := nowOrDefault(f.Now)
	raised, err := RaisedPriorities(ctx, q, now)
	if err != nil {
		return nil, nil, 0, err
	}
	coef, err := LoadCoefficients(ctx, q)
	if err != nil {
		return nil, nil, 0, err
	}
	adjusted := coef[CoefficientTimeAdjust] == TimeAdjustOn

	beads, _, err := runListQuery(ctx, q, fromWhere, args, f.Label, 0, SortPriority)
	if err != nil {
		return nil, nil, 0, err
	}

	var total int
	out := make([]BlockedBead, 0, len(beads))
	for _, b := range beads {
		out = append(out, BlockedBead{Bead: b})
	}
	if adjusted {
		adj, held, err := heldReadyBeads(ctx, q, f, now, coef, raised)
		if err != nil {
			return nil, nil, 0, err
		}
		for _, b := range held {
			out = append(out, BlockedBead{Bead: b, HeldSince: FormatTime(adj.HeldSince[b.ID])})
		}
		slices.SortStableFunc(out, func(a, b BlockedBead) int { return compareAdjusted(adj, a.Bead, b.Bead) })
		raised = adj.Eff
		total = len(out)
		if f.Limit > 0 && f.Limit < total {
			out = out[:f.Limit]
		}
	} else {
		demand, err := CandidateDemand(ctx, q, now, coef, beads)
		if err != nil {
			return nil, nil, 0, err
		}
		slices.SortStableFunc(out, func(a, b BlockedBead) int { return compareEffective(raised, demand, a.Bead, b.Bead) })
		total = len(out)
		if f.Limit > 0 && f.Limit < total {
			out = out[:f.Limit]
		}
	}

	for i := range out {
		if out[i].HeldSince != "" {
			continue
		}
		blockers, err := ActiveBlockers(ctx, q, out[i].ID)
		if err != nil {
			return nil, nil, 0, err
		}
		out[i].Blockers = blockers
	}
	return out, raised, total, nil
}

func ActiveBlockers(ctx context.Context, q Querier, beadID string) ([]Blocker, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT b.id, b.namespace, b.type
		FROM dependencies d
		JOIN beads b ON b.id = d.depends_on_id
		WHERE d.bead_id = ? AND d.type = ? AND d.removed = 0 AND b.status NOT IN (?, ?)
		ORDER BY b.created_at ASC, b.id ASC`,
		beadID, BlocksDepType, StatusClosed, StatusCancelled)
	if err != nil {
		return nil, fmt.Errorf("active blockers: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Blocker
	for rows.Next() {
		var bl Blocker
		var beadType string
		if err := rows.Scan(&bl.ID, &bl.Namespace, &beadType); err != nil {
			return nil, fmt.Errorf("active blockers: scan: %w", err)
		}
		bl.IsGate = beadType == BeadTypeGate
		out = append(out, bl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("active blockers: iterate: %w", err)
	}

	children, err := unfinishedChildren(ctx, q, beadID)
	if err != nil {
		return nil, err
	}
	out = append(out, children...)
	return out, nil
}

func openBlocksBlockers(ctx context.Context, q Querier, beadID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT b.id
		FROM dependencies d
		JOIN beads b ON b.id = d.depends_on_id
		WHERE d.bead_id = ? AND d.type = ? AND d.removed = 0 AND b.status NOT IN (?, ?)
		ORDER BY b.priority ASC, b.created_at ASC, b.id ASC`,
		beadID, BlocksDepType, StatusClosed, StatusCancelled)
	if err != nil {
		return nil, fmt.Errorf("open blocks blockers: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("open blocks blockers: scan: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("open blocks blockers: iterate: %w", err)
	}
	return out, nil
}

func unfinishedChildren(ctx context.Context, q Querier, parentID string) ([]Blocker, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.id, c.namespace, c.type
		FROM dependencies pc
		JOIN beads c ON c.id = pc.bead_id
		WHERE pc.depends_on_id = ? AND pc.type = ? AND pc.removed = 0 AND c.status NOT IN (?, ?)
		ORDER BY c.created_at ASC, c.id ASC`,
		parentID, ParentChildDepType, StatusClosed, StatusCancelled)
	if err != nil {
		return nil, fmt.Errorf("unfinished children: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Blocker
	for rows.Next() {
		var bl Blocker
		var beadType string
		if err := rows.Scan(&bl.ID, &bl.Namespace, &beadType); err != nil {
			return nil, fmt.Errorf("unfinished children: scan: %w", err)
		}
		bl.IsGate = beadType == BeadTypeGate
		out = append(out, bl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unfinished children: iterate: %w", err)
	}
	return out, nil
}

func ReadyIDSet(ctx context.Context, q Querier) (map[string]bool, error) {
	beads, _, err := ReadyBeads(ctx, q, ReadyFilter{Priority: -1})
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(beads))
	for _, b := range beads {
		set[b.ID] = true
	}
	return set, nil
}

func NewlyReadyDiff(ctx context.Context, q Querier, fn func() error) ([]string, error) {
	before, err := ReadyIDSet(ctx, q)
	if err != nil {
		return nil, err
	}
	if err := fn(); err != nil {
		return nil, err
	}
	after, err := ReadyIDSet(ctx, q)
	if err != nil {
		return nil, err
	}
	var newly []string
	for id := range after {
		if !before[id] {
			newly = append(newly, id)
		}
	}
	return newly, nil
}
