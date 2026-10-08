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

func setLabels(t *testing.T, db *sql.DB, id string, labels ...string) {
	t.Helper()
	enc, err := EncodeStringList(labels)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET labels = ? WHERE id = ?`, enc, id); err != nil {
		t.Fatal(err)
	}
}

func displayID(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var d string
	if err := inTx(t, db, func(tx *sql.Tx) error {
		var err error
		d, err = DisplayIDFor(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return d
}

func gateCycles(t *testing.T, db *sql.DB) (GateCycleReport, error) {
	t.Helper()
	var r GateCycleReport
	err := inTx(t, db, func(tx *sql.Tx) error {
		var err error
		r, err = GateCycles(context.Background(), tx)
		return err
	})
	return r, err
}

func TestGateCyclesListsStoredViolations(t *testing.T) {
	db := testDB(t)
	const lost, other = "https://github.com/o/r/pull/9", "https://github.com/o/r/pull/8"

	p := newTask(t, db, "owner", docPR)
	x := newTask(t, db, "mid")
	if err := addBlocks(t, db, p, x); err != nil {
		t.Fatal(err)
	}
	g1, err := gateCreate(t, db, []string{x})
	if err != nil {
		t.Fatal(err)
	}
	setLabels(t, db, g1, "kind:adjudicate", MaterialPrefix+docPR, "wait:pr-merged:"+docPR)

	newTask(t, db, "unrelated", other)
	g2, err := gateCreate(t, db, []string{newTask(t, db, "b")})
	if err != nil {
		t.Fatal(err)
	}
	setLabels(t, db, g2, "wait:ci-green:"+lost, MaterialPrefix+other, "wait:date:2999-01-01")

	got, err := gateCycles(t, db)
	if err != nil {
		t.Fatal(err)
	}
	dg1 := displayID(t, db, g1)
	wantCycle := strings.Join([]string{dg1, displayID(t, db, x), displayID(t, db, p), dg1}, " -> ")
	if !slices.Equal(got.Cycles, []string{wantCycle}) {
		t.Errorf("Cycles = %q, want [%q] once despite two labels naming the PR", got.Cycles, wantCycle)
	}
	wantNoOwner := displayID(t, db, g2) + ` "` + lost + `"`
	if !slices.Equal(got.NoOwner, []string{wantNoOwner}) {
		t.Errorf("NoOwner = %q, want [%q]", got.NoOwner, wantNoOwner)
	}
	if got.Empty() {
		t.Error("Empty() = true with violations")
	}

	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET status = ? WHERE id IN (?, ?)`, StatusClosed, g1, g2); err != nil {
		t.Fatal(err)
	}
	got, err = gateCycles(t, db)
	if err != nil || !got.Empty() {
		t.Errorf("after close: %+v, %v; want empty", got, err)
	}
}

func TestGateIsNeverAnOwner(t *testing.T) {
	db := testDB(t)
	owner := newTask(t, db, "owner", docPR)
	g, err := gateCreate(t, db, []string{newTask(t, db, "x")}, "kind:adjudicate", "wait:pr-merged:"+docPR)
	if err != nil {
		t.Fatal(err)
	}
	if err := update(t, db, UpdateInput{ID: g, AddExternalRefs: []string{docPR}}); err != nil {
		t.Fatalf("Gate taking its own wait target as a ref: %v", err)
	}
	if err := update(t, db, UpdateInput{ID: owner, Cancel: true}); err != nil {
		t.Fatal(err)
	}
	got, err := gateCycles(t, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cycles) != 0 {
		t.Errorf("Cycles = %q, want none", got.Cycles)
	}
	wantNoOwner := displayID(t, db, g) + ` "` + docPR + `"`
	if !slices.Equal(got.NoOwner, []string{wantNoOwner}) {
		t.Errorf("NoOwner = %q, want [%q]", got.NoOwner, wantNoOwner)
	}
	_, err = gateCreate(t, db, []string{newTask(t, db, "y")}, "kind:adjudicate", MaterialPrefix+docPR)
	if e := new(*WaitTargetNoOwnerError); !errors.As(err, e) {
		t.Errorf("gate create on a target only a Gate holds: err = %v, want WaitTargetNoOwnerError", err)
	}
}

func TestGateCyclesSortsByCanonicalID(t *testing.T) {
	db := testDB(t)
	ids := make([]string, 0, 3)
	for range 3 {
		g, err := gateCreate(t, db, []string{newTask(t, db, "b")})
		if err != nil {
			t.Fatal(err)
		}
		setLabels(t, db, g, "wait:pr-merged:z", "wait:pr-merged:a")
		ids = append(ids, g)
	}
	slices.Sort(ids)
	want := make([]string, 0, 2*len(ids))
	for _, g := range ids {
		d := displayID(t, db, g)
		want = append(want, d+` "a"`, d+` "z"`)
	}
	got, err := gateCycles(t, db)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.NoOwner, want) {
		t.Errorf("NoOwner = %q, want %q", got.NoOwner, want)
	}
}

func TestGateCyclesFailsOnReadErrors(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	done, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = done.Rollback()
	if _, err := GateCycles(ctx, done); err == nil {
		t.Error("GateCycles on a finished tx = nil")
	}

	newTask(t, db, "owner", docPR)
	g, err := gateCreate(t, db, []string{newTask(t, db, "b")})
	if err != nil {
		t.Fatal(err)
	}
	setLabels(t, db, g, MaterialPrefix+docPR)
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET external_refs = 'not json' WHERE external_refs IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := gateCycles(t, db); err == nil || !strings.Contains(err.Error(), "wait edges: read external_refs") {
		t.Errorf("unreadable owner index: err = %v", err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET labels = 'not json' WHERE id = ?`, g); err != nil {
		t.Fatal(err)
	}
	if _, err := gateCycles(t, db); err == nil || !strings.Contains(err.Error(), "gate cycles: read gate") {
		t.Errorf("unreadable labels: err = %v", err)
	}
}
