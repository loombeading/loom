// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/domain"
)

func TestDagEdgeNewer(t *testing.T) {
	base := dagEdge{beadID: "b", dependsOnID: "d", createdAt: "2"}
	for _, tc := range []struct {
		name string
		a, b dagEdge
		want bool
	}{
		{"later createdAt wins", dagEdge{beadID: "b", dependsOnID: "d", createdAt: "3"}, base, true},
		{"earlier createdAt loses", dagEdge{beadID: "b", dependsOnID: "d", createdAt: "1"}, base, false},
		{"createdAt dominates a larger beadID", dagEdge{beadID: "z", dependsOnID: "z", createdAt: "1"}, base, false},
		{"same createdAt larger beadID wins", dagEdge{beadID: "c", dependsOnID: "d", createdAt: "2"}, base, true},
		{"same createdAt smaller beadID loses", dagEdge{beadID: "a", dependsOnID: "d", createdAt: "2"}, base, false},
		{"beadID dominates a larger dependsOnID", dagEdge{beadID: "a", dependsOnID: "z", createdAt: "2"}, base, false},
		{"same createdAt and beadID larger dependsOnID wins", dagEdge{beadID: "b", dependsOnID: "e", createdAt: "2"}, base, true},
		{"same createdAt and beadID smaller dependsOnID loses", dagEdge{beadID: "b", dependsOnID: "c", createdAt: "2"}, base, false},
		{"identical edges are not newer", base, base, false},
	} {
		if got := dagEdgeNewer(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: dagEdgeNewer(%+v, %+v) = %v, want %v", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDagEdgeNewerIsAsymmetric(t *testing.T) {
	edges := []dagEdge{
		{beadID: "a", dependsOnID: "b", createdAt: "1"},
		{beadID: "a", dependsOnID: "b", createdAt: "2"},
		{beadID: "a", dependsOnID: "c", createdAt: "2"},
		{beadID: "b", dependsOnID: "a", createdAt: "2"},
	}
	for i, x := range edges {
		for j, y := range edges {
			if i == j {
				continue
			}
			if dagEdgeNewer(x, y) == dagEdgeNewer(y, x) {
				t.Errorf("dagEdgeNewer(%+v, %+v) and its reverse agree; one edge must be strictly newer", x, y)
			}
		}
	}
}

func TestNewestEdgeWithinPicksNewestInsideComponent(t *testing.T) {
	edges := []dagEdge{
		{beadID: "a", dependsOnID: "b", createdAt: "1"},
		{beadID: "b", dependsOnID: "a", createdAt: "2"},
		{beadID: "a", dependsOnID: "out", createdAt: "9"},
		{beadID: "out", dependsOnID: "b", createdAt: "9"},
	}
	got := newestEdgeWithin(edges, []string{"a", "b"})
	if got == nil || got.beadID != "b" || got.dependsOnID != "a" {
		t.Errorf("newestEdgeWithin = %+v, want b -> a", got)
	}
	if got := newestEdgeWithin(edges, []string{"x", "y"}); got != nil {
		t.Errorf("newestEdgeWithin of an unrelated component = %+v, want nil", got)
	}
}

func TestNewestCycleEdgeAcrossComponentsPicksNewest(t *testing.T) {
	edges := []dagEdge{
		{beadID: "a", dependsOnID: "b", createdAt: "1"},
		{beadID: "b", dependsOnID: "a", createdAt: "3"},
		{beadID: "c", dependsOnID: "d", createdAt: "2"},
		{beadID: "d", dependsOnID: "c", createdAt: "5"},
		{beadID: "e", dependsOnID: "f", createdAt: "9"},
	}
	got := newestCycleEdge(edges)
	if got == nil || got.beadID != "d" || got.dependsOnID != "c" {
		t.Errorf("newestCycleEdge = %+v, want d -> c", got)
	}
	if got := newestCycleEdge(edges[4:]); got != nil {
		t.Errorf("newestCycleEdge of an acyclic graph = %+v, want nil", got)
	}
}

func doneTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	return tx
}

func wantErrContaining(t *testing.T, name string, err error, sub string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sub) {
		t.Errorf("%s: err = %v, want containing %q", name, err, sub)
	}
}

func TestDagFunctionsPropagateTxErrors(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tx := doneTx(t, db)
	opt := Options{Actor: "test", Now: "n0"}
	edge := dagEdge{beadID: "a", dependsOnID: "b", createdAt: "1", stateAt: "1"}

	_, err := loadActiveEdges(ctx, tx, domain.BlocksDepType)
	wantErrContaining(t, "loadActiveEdges", err, "load active blocks edges")

	wantErrContaining(t, "breakCycles", breakCycles(ctx, tx, domain.BlocksDepType, opt, &Summary{}), "load active")
	wantErrContaining(t, "resolveDuplicateParents", resolveDuplicateParents(ctx, tx, opt, &Summary{}), "load active")
	wantErrContaining(t, "resolveCyclesAndParents", resolveCyclesAndParents(ctx, tx, opt, &Summary{}), "load active")
	wantErrContaining(t, "removeDependencyEdge", removeDependencyEdge(ctx, tx, edge, domain.BlocksDepType, opt), "remove cycle edge")
	wantErrContaining(t, "removeDuplicateParentEdge", removeDuplicateParentEdge(ctx, tx, edge, opt), "remove duplicate parent edge")

	_, err = cycleEdgeLine(ctx, tx, edge, domain.BlocksDepType)
	wantErrContaining(t, "cycleEdgeLine", err, "lookup namespace")

	_, err = countDangling(ctx, tx)
	wantErrContaining(t, "countDangling", err, "count dangling")
}

func TestCycleEdgeLineReportsMissingEndpoint(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	now := "2000-01-01T00:00:00.000Z"
	a := mustCreate(t, db, domain.CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = cycleEdgeLine(ctx, tx, dagEdge{beadID: "missing", dependsOnID: a}, domain.BlocksDepType)
	wantErrContaining(t, "missing source", err, "lookup namespace")

	_, err = cycleEdgeLine(ctx, tx, dagEdge{beadID: a, dependsOnID: "missing"}, domain.BlocksDepType)
	wantErrContaining(t, "missing target", err, "lookup namespace")

	line, err := cycleEdgeLine(ctx, tx, dagEdge{beadID: a, dependsOnID: a}, domain.BlocksDepType)
	if err != nil || !strings.HasPrefix(line, "Cycle resolved: ") || !strings.HasSuffix(line, "(blocks) removed") {
		t.Errorf("cycleEdgeLine = %q, %v, want a Cycle resolved line", line, err)
	}
}

func TestCountDanglingCountsOnlyMissingTargets(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	now := "2000-01-01T00:00:00.000Z"
	a := mustCreate(t, db, domain.CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	b := mustCreate(t, db, domain.CreateInput{Title: "b", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	inTx(t, db, func(tx *sql.Tx) error {
		return domain.AddLink(ctx, tx, "u", a, b, domain.BlocksDepType, now, "")
	})
	count := func() int {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		n, err := countDangling(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(); n != 0 {
		t.Fatalf("countDangling with every target present = %d, want 0", n)
	}
	inTx(t, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed) VALUES (?, 'gone', 'blocks', ?, 0)`, a, now)
		return err
	})
	if n := count(); n != 1 {
		t.Errorf("countDangling with one missing target = %d, want 1", n)
	}
}
