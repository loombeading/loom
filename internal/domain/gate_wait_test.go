// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
)

const docPR = "https://github.com/o/r/pull/7"

func newTask(t *testing.T, db *sql.DB, title string, refs ...string) string {
	t.Helper()
	return mustCreate(t, db, CreateInput{Title: title, Priority: 2, BeadType: "task", Namespace: "lm", ExternalRefs: refs, Actor: "u", Now: "n0"})
}

func gateCreate(t *testing.T, db *sql.DB, blocks []string, labels ...string) (string, error) {
	t.Helper()
	var id string
	err := inTx(t, db, func(tx *sql.Tx) error {
		var err error
		id, err = GateCreate(context.Background(), tx, GateCreateInput{
			Blocks: blocks, Fields: GateFields{Subject: "wait"}, Namespace: "lm", Labels: labels, Actor: "u", Now: "n1",
		})
		return err
	})
	return id, err
}

func update(t *testing.T, db *sql.DB, in UpdateInput) error {
	t.Helper()
	in.Priority, in.Actor, in.Now = -1, "u", "n2"
	return inTx(t, db, func(tx *sql.Tx) error { _, err := UpdateBead(context.Background(), tx, in); return err })
}

func addBlocks(t *testing.T, db *sql.DB, bead, dependsOn string) error {
	t.Helper()
	return inTx(t, db, func(tx *sql.Tx) error {
		return AddLink(context.Background(), tx, "u", bead, dependsOn, BlocksDepType, "n3", "")
	})
}

func wantWaitCycle(t *testing.T, err error, n int) {
	t.Helper()
	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *CycleError", err)
	}
	if ce.DepType != WaitDepType {
		t.Fatalf("DepType = %q, want %q", ce.DepType, WaitDepType)
	}
	if len(ce.Cycle) != n-1 {
		t.Fatalf("cycle = %v, want a cycle of %d nodes shown as %d", ce.Cycle, n-1, n-1)
	}
	if !strings.Contains(err.Error(), "adding this wait link would create a cycle: ") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestWaitTargets(t *testing.T) {
	got := WaitTargets([]string{"kind:human", "wait:date:2999-01-01", "wait:pr-merged:a", "wait:ci-green:b", "material:c", "material:", "wait:runs:5"})
	if !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("WaitTargets = %v", got)
	}
}

func TestGateCreateRejectsWaitingOnBlockedOwner(t *testing.T) {
	db := testDB(t)
	p := newTask(t, db, "owner", docPR)
	_, err := gateCreate(t, db, []string{p}, "kind:adjudicate", MaterialPrefix+docPR)
	wantWaitCycle(t, err, 3)
	if n := countGates(t, db); n != 0 {
		t.Fatalf("gates = %d, want the rejected create to write nothing", n)
	}
}

func TestGateCreateRejectsTransitiveCycle(t *testing.T) {
	db := testDB(t)
	p := newTask(t, db, "owner", docPR)
	b := newTask(t, db, "b")
	if err := addBlocks(t, db, p, b); err != nil {
		t.Fatal(err)
	}
	_, err := gateCreate(t, db, []string{b}, "kind:adjudicate", MaterialPrefix+docPR)
	wantWaitCycle(t, err, 4)
}

func TestGateCreateAcceptsIndependentOwner(t *testing.T) {
	db := testDB(t)
	newTask(t, db, "doc bead", docPR)
	impl := newTask(t, db, "impl")
	if _, err := gateCreate(t, db, []string{impl}, "kind:adjudicate", MaterialPrefix+docPR); err != nil {
		t.Fatal(err)
	}
}

func TestGateCreateRejectsOwnerlessTarget(t *testing.T) {
	db := testDB(t)
	b := newTask(t, db, "b")
	closedOwner := newTask(t, db, "closed owner", docPR)
	if err := update(t, db, UpdateInput{ID: closedOwner, Cancel: true}); err != nil {
		t.Fatal(err)
	}
	_, err := gateCreate(t, db, []string{b}, "kind:adjudicate", MaterialPrefix+docPR)
	var e *WaitTargetNoOwnerError
	if !errors.As(err, &e) || e.Target != docPR {
		t.Fatalf("err = %v, want WaitTargetNoOwnerError for %s", err, docPR)
	}
}

func TestAddWaitLabelRejectsCycle(t *testing.T) {
	db := testDB(t)
	p := newTask(t, db, "owner", docPR)
	g, err := gateCreate(t, db, []string{p}, "kind:adjudicate")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"wait:pr-merged:" + docPR, "wait:ci-green:" + docPR} {
		wantWaitCycle(t, update(t, db, UpdateInput{ID: g, AddLabels: []string{l}}), 3)
	}
	if err := update(t, db, UpdateInput{ID: g, AddLabels: []string{"wait:date:2999-01-01"}}); err != nil {
		t.Fatalf("date wait makes no edge: %v", err)
	}
}

func TestAddExternalRefRejectsCycle(t *testing.T) {
	db := testDB(t)
	newTask(t, db, "first owner", docPR)
	p := newTask(t, db, "p")
	if _, err := gateCreate(t, db, []string{p}, "kind:adjudicate", "wait:pr-merged:"+docPR); err != nil {
		t.Fatal(err)
	}
	wantWaitCycle(t, update(t, db, UpdateInput{ID: p, AddExternalRefs: []string{docPR}}), 3)
	if err := update(t, db, UpdateInput{ID: p, AddExternalRefs: []string{"https://github.com/o/r/pull/8"}}); err != nil {
		t.Fatalf("unrelated ref: %v", err)
	}
}

func TestAddBlocksRejectsCycleThroughWaitEdge(t *testing.T) {
	db := testDB(t)
	p := newTask(t, db, "owner", docPR)
	b := newTask(t, db, "b")
	if _, err := gateCreate(t, db, []string{b}, "kind:adjudicate", MaterialPrefix+docPR); err != nil {
		t.Fatal(err)
	}
	wantWaitCycle(t, addBlocks(t, db, p, b), 4)
}

func TestGateMaterialRequired(t *testing.T) {
	db := testDB(t)
	b := newTask(t, db, "b")
	newTask(t, db, "doc bead", docPR)
	for _, f := range []GateFields{
		{Subject: "PR #1068 の承認"},
		{Subject: "承認", Proposal: "merge する"},
		{Subject: "承認", Check: "https://github.com/o/r/pull/1 を見る"},
	} {
		if err := humanGate(t, db, b, f); !errors.As(err, new(*GateMaterialRequiredError)) {
			t.Fatalf("%+v: err = %v, want GateMaterialRequiredError", f, err)
		}
		if err := humanGate(t, db, b, f, MaterialPrefix+docPR); err != nil {
			t.Fatalf("%+v with material: %v", f, err)
		}
	}
	for _, s := range []string{"prod への承認", "prereq の確認", "emergency 対応", "出荷判定"} {
		if err := humanGate(t, db, b, GateFields{Subject: s}); err != nil {
			t.Fatalf("%q mentions no PR: %v", s, err)
		}
	}
}

func humanGate(t *testing.T, db *sql.DB, b string, f GateFields, extra ...string) error {
	t.Helper()
	labels := append([]string{"kind:human", judgeLabel(t, db)}, extra...)
	return inTx(t, db, func(tx *sql.Tx) error {
		_, err := GateCreate(context.Background(), tx, GateCreateInput{Blocks: []string{b}, Fields: f, Namespace: "lm", Labels: labels, Actor: "u", Now: "n1"})
		return err
	})
}

func TestGateMaterialRequiredOnUpdate(t *testing.T) {
	db := testDB(t)
	b := newTask(t, db, "b")
	newTask(t, db, "doc bead", docPR)
	g := mustCreate(t, db, CreateInput{Title: "g", Description: "PR の裁定", Priority: 2, BeadType: "gate", Labels: []string{"kind:adjudicate"}, Namespace: "lm", Actor: "u", Now: "n0"})
	if err := addBlocks(t, db, b, g); err != nil {
		t.Fatal(err)
	}
	j := judgeLabel(t, db)
	if err := update(t, db, UpdateInput{ID: g, AddLabels: []string{"kind:human", j}}); !errors.As(err, new(*GateMaterialRequiredError)) {
		t.Fatalf("err = %v, want GateMaterialRequiredError", err)
	}
	if err := update(t, db, UpdateInput{ID: g, AddLabels: []string{"kind:human", j, MaterialPrefix + docPR}}); err != nil {
		t.Fatal(err)
	}
	if err := update(t, db, UpdateInput{ID: g, RemoveLabels: []string{MaterialPrefix + docPR}}); !errors.As(err, new(*GateMaterialRequiredError)) {
		t.Fatalf("removing the material: err = %v, want GateMaterialRequiredError", err)
	}
}

func TestWaitChecksFailClosedOnReadErrors(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	b := newTask(t, db, "b")
	newTask(t, db, "owner", docPR)
	g, err := gateCreate(t, db, []string{b}, "kind:adjudicate", MaterialPrefix+docPR)
	if err != nil {
		t.Fatal(err)
	}

	done, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = done.Rollback()
	if err := checkNewWaitTargets(ctx, done, g, nil, []string{MaterialPrefix + "x"}); err == nil {
		t.Error("checkNewWaitTargets on a finished tx = nil")
	}
	if err := checkNewOwnedRefs(ctx, done, b, StatusOpen, nil, []string{docPR}); err == nil {
		t.Error("checkNewOwnedRefs on a finished tx = nil")
	}
	if _, err := (&waitGraph{ctx: ctx, tx: done}).waitSuccessors(g); err == nil {
		t.Error("waitSuccessors on a finished tx = nil")
	}

	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET external_refs = 'not json' WHERE external_refs IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	other := newTask(t, db, "other")
	if err := addBlocks(t, db, other, g); err == nil || !strings.Contains(err.Error(), "wait edges: read external_refs") {
		t.Errorf("blocks walk over an unreadable owner index: err = %v", err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET labels = 'not json' WHERE id = ?`, g); err != nil {
		t.Fatal(err)
	}
	if err := update(t, db, UpdateInput{ID: other, AddExternalRefs: []string{docPR}}); err == nil || !strings.Contains(err.Error(), "wait edges: read") {
		t.Errorf("external ref over unreadable columns: err = %v", err)
	}
	if _, err := (&waitGraph{ctx: ctx, tx: mustBegin(t, db)}).waitSuccessors(g); err == nil {
		t.Error("waitSuccessors over unreadable labels = nil")
	}
}

func mustBegin(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}
