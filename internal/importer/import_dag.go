// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"

	"github.com/loombeading/loom/internal/domain"
)

type dagEdge struct {
	beadID, dependsOnID, createdAt string
	stateAt                        string
}

func resolveCyclesAndParents(ctx context.Context, tx *sql.Tx, opt Options, sum *Summary) error {
	for _, depType := range []string{domain.BlocksDepType, domain.ParentChildDepType} {
		if err := breakCycles(ctx, tx, depType, opt, sum); err != nil {
			return err
		}
	}
	return resolveDuplicateParents(ctx, tx, opt, sum)
}

func loadActiveEdges(ctx context.Context, tx *sql.Tx, depType string) ([]dagEdge, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT bead_id, depends_on_id, created_at, COALESCE(removed_set_at, created_at) FROM dependencies
		WHERE type = ? AND removed = 0`, depType)
	if err != nil {
		return nil, fmt.Errorf("import: load active %s edges: %w", depType, err)
	}
	defer func() { _ = rows.Close() }()
	var out []dagEdge
	for rows.Next() {
		var e dagEdge
		if err := rows.Scan(&e.beadID, &e.dependsOnID, &e.createdAt, &e.stateAt); err != nil {
			return nil, fmt.Errorf("import: scan %s edge: %w", depType, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func breakCycles(ctx context.Context, tx *sql.Tx, depType string, opt Options, sum *Summary) error {
	for {
		edges, err := loadActiveEdges(ctx, tx, depType)
		if err != nil {
			return err
		}
		toRemove := newestCycleEdge(edges)
		if toRemove == nil {
			return nil
		}

		if err := removeDependencyEdge(ctx, tx, *toRemove, depType, opt); err != nil {
			return err
		}
		display, err := cycleEdgeLine(ctx, tx, *toRemove, depType)
		if err != nil {
			return err
		}
		sum.CycleLines = append(sum.CycleLines, display)
	}
}

func newestCycleEdge(edges []dagEdge) *dagEdge {
	adj := map[string][]string{}
	for _, e := range edges {
		adj[e.beadID] = append(adj[e.beadID], e.dependsOnID)
	}
	var toRemove *dagEdge
	for _, comp := range tarjanSCCs(adj) {
		if len(comp) < 2 {
			continue
		}
		worst := newestEdgeWithin(edges, comp)
		if worst != nil && (toRemove == nil || dagEdgeNewer(*worst, *toRemove)) {
			toRemove = worst
		}
	}
	return toRemove
}

func newestEdgeWithin(edges []dagEdge, comp []string) *dagEdge {
	inComp := map[string]bool{}
	for _, n := range comp {
		inComp[n] = true
	}
	var worst *dagEdge
	for _, e := range edges {
		if !inComp[e.beadID] || !inComp[e.dependsOnID] {
			continue
		}
		if worst == nil || dagEdgeNewer(e, *worst) {
			ec := e
			worst = &ec
		}
	}
	return worst
}

func dagEdgeNewer(a, b dagEdge) bool {
	if a.createdAt != b.createdAt {
		return a.createdAt > b.createdAt
	}
	if a.beadID != b.beadID {
		return a.beadID > b.beadID
	}
	return a.dependsOnID > b.dependsOnID
}

func removeDependencyEdge(ctx context.Context, tx *sql.Tx, e dagEdge, depType string, opt Options) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE dependencies SET removed = 1, removed_set_at = ?
		WHERE bead_id = ? AND depends_on_id = ? AND type = ?`,
		e.stateAt, e.beadID, e.dependsOnID, depType)
	if err != nil {
		return fmt.Errorf("import: remove cycle edge: %w", err)
	}
	return domain.Audit(ctx, tx, domain.AuditRecord{
		OccurredAt: opt.Now, Actor: opt.Actor, BeadID: e.beadID,
		Kind: domain.AuditKindMerge, Field: domain.FieldDependency, Origin: domain.OriginImport,
		NewValue: fmt.Sprintf(`{"depends_on_id":%q,"type":%q,"removed":true}`, e.dependsOnID, depType),
		Reason:   fmt.Sprintf("cycle: %s -> %s", e.beadID, e.dependsOnID),
	})
}

func cycleEdgeLine(ctx context.Context, tx *sql.Tx, e dagEdge, depType string) (string, error) {
	from, err := domain.DisplayIDFor(ctx, tx, e.beadID)
	if err != nil {
		return "", err
	}
	to, err := domain.DisplayIDFor(ctx, tx, e.dependsOnID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Cycle resolved: %s -> %s (%s) removed", from, to, depType), nil
}

func resolveDuplicateParents(ctx context.Context, tx *sql.Tx, opt Options, sum *Summary) error {
	edges, err := loadActiveEdges(ctx, tx, domain.ParentChildDepType)
	if err != nil {
		return err
	}
	byChild := map[string][]dagEdge{}
	for _, e := range edges {
		byChild[e.beadID] = append(byChild[e.beadID], e)
	}
	var children []string
	for c := range byChild {
		children = append(children, c)
	}
	sort.Strings(children)

	for _, child := range children {
		parents := byChild[child]
		if len(parents) < 2 {
			continue
		}
		slices.SortFunc(parents, func(a, b dagEdge) int {
			return cmp.Or(
				cmp.Compare(a.createdAt, b.createdAt),
				cmp.Compare(a.dependsOnID, b.dependsOnID),
			)
		})
		for _, e := range parents[1:] {
			if err := removeDuplicateParentEdge(ctx, tx, e, opt); err != nil {
				return err
			}
			from, err := domain.DisplayIDFor(ctx, tx, e.beadID)
			if err != nil {
				return err
			}
			to, err := domain.DisplayIDFor(ctx, tx, e.dependsOnID)
			if err != nil {
				return err
			}
			sum.ParentLines = append(sum.ParentLines, fmt.Sprintf("Parent resolved: %s -> %s removed", from, to))
		}
	}
	return nil
}

func removeDuplicateParentEdge(ctx context.Context, tx *sql.Tx, e dagEdge, opt Options) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE dependencies SET removed = 1, removed_set_at = ?
		WHERE bead_id = ? AND depends_on_id = ? AND type = ?`,
		e.stateAt, e.beadID, e.dependsOnID, domain.ParentChildDepType)
	if err != nil {
		return fmt.Errorf("import: remove duplicate parent edge: %w", err)
	}
	return domain.Audit(ctx, tx, domain.AuditRecord{
		OccurredAt: opt.Now, Actor: opt.Actor, BeadID: e.beadID,
		Kind: domain.AuditKindMerge, Field: domain.FieldDependency, Origin: domain.OriginImport,
		NewValue: fmt.Sprintf(`{"depends_on_id":%q,"type":%q,"removed":true}`, e.dependsOnID, domain.ParentChildDepType),
		Reason:   fmt.Sprintf("duplicate parent: %s -> %s", e.beadID, e.dependsOnID),
	})
}

func countDangling(ctx context.Context, tx *sql.Tx) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dependencies d
		WHERE NOT EXISTS (SELECT 1 FROM beads i WHERE i.id = d.depends_on_id)`,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("import: count dangling: %w", err)
	}
	return n, nil
}

func tarjanSCCs(adj map[string][]string) [][]string {
	nodes := map[string]bool{}
	for n, outs := range adj {
		nodes[n] = true
		for _, o := range outs {
			nodes[o] = true
		}
	}
	order := make([]string, 0, len(nodes))
	for n := range nodes {
		order = append(order, n)
	}
	sort.Strings(order)

	index := map[string]int{}
	lowlink := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	counter := 0
	var out [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		index[v] = counter
		lowlink[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true

		neighbors := append([]string{}, adj[v]...)
		sort.Strings(neighbors)
		for _, w := range neighbors {
			if _, ok := index[w]; !ok {
				strongconnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if index[w] < lowlink[v] {
					lowlink[v] = index[w]
				}
			}
		}

		if lowlink[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			out = append(out, comp)
		}
	}

	for _, n := range order {
		if _, ok := index[n]; !ok {
			strongconnect(n)
		}
	}
	return out
}
