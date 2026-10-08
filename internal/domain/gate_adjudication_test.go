// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
)

func inTx(t *testing.T, db *sql.DB, fn func(tx *sql.Tx) error) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func newJudge(t *testing.T, db *sql.DB, labels ...string) string {
	t.Helper()
	return mustCreate(t, db, CreateInput{Title: "judge", Priority: 2, BeadType: "gate", Labels: labels, Namespace: "lm", Actor: "u", Now: "n0"})
}

func resolveJudge(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if err := inTx(t, db, func(tx *sql.Tx) error {
		return GateResolve(context.Background(), tx, GateResolveInput{ID: id, Reason: "judged", Actor: "u", Now: "n0"})
	}); err != nil {
		t.Fatal(err)
	}
}

func judgeLabel(t *testing.T, db *sql.DB) string {
	t.Helper()
	id := newJudge(t, db, "kind:adjudicate")
	resolveJudge(t, db, id)
	return AdjudicatedByPrefix + id
}

func wantPath(t *testing.T, err error, path int) {
	t.Helper()
	var e *GateNotAdjudicatedError
	if !errors.As(err, &e) || e.Path != path {
		t.Fatalf("err = %v, want GateNotAdjudicatedError path %d", err, path)
	}
	if !errors.Is(err, ErrGateNotAdjudicated) {
		t.Fatalf("err = %v, want errors.Is ErrGateNotAdjudicated", err)
	}
}

func TestGateAdjudicationRejectsFourPathsOnEveryEntry(t *testing.T) {
	type setup func(t *testing.T, db *sql.DB) []string
	cases := []struct {
		name  string
		extra setup
		path  int
	}{
		{"missing", func(*testing.T, *sql.DB) []string { return nil }, AdjudicationMissing},
		{"empty value", func(*testing.T, *sql.DB) []string { return []string{AdjudicatedByPrefix} }, AdjudicationUnresolved},
		{"unknown bead", func(*testing.T, *sql.DB) []string { return []string{AdjudicatedByPrefix + "lm-zzzz"} }, AdjudicationUnresolved},
		{"not a gate", func(t *testing.T, db *sql.DB) []string {
			t.Helper()
			id := mustCreate(t, db, CreateInput{Title: "task", Priority: 2, BeadType: "task", Labels: []string{"kind:adjudicate"}, Namespace: "lm", Actor: "u", Now: "n0"})
			if err := inTx(t, db, func(tx *sql.Tx) error {
				_, err := UpdateBead(context.Background(), tx, UpdateInput{ID: id, Priority: -1, Cancel: true, Actor: "u", Now: "n0"})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			return []string{AdjudicatedByPrefix + id}
		}, AdjudicationNotJudge},
		{"not adjudicate", func(t *testing.T, db *sql.DB) []string {
			t.Helper()
			id := newJudge(t, db, "kind:external")
			resolveJudge(t, db, id)
			return []string{AdjudicatedByPrefix + id}
		}, AdjudicationNotJudge},
		{"open judge", func(t *testing.T, db *sql.DB) []string {
			t.Helper()
			return []string{AdjudicatedByPrefix + newJudge(t, db, "kind:adjudicate")}
		}, AdjudicationNotClosed},
		{"cancelled judge", func(t *testing.T, db *sql.DB) []string {
			t.Helper()
			target := newBead(t, db, "n0")
			id := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "j"}, Labels: []string{"kind:adjudicate"}, Namespace: "lm", Actor: "u", Now: "n0"})
			if err := inTx(t, db, func(tx *sql.Tx) error {
				_, err := GateReject(context.Background(), tx, GateRejectInput{ID: id, Reason: "no", Actor: "u", Now: "n0"})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			return []string{AdjudicatedByPrefix + id}
		}, AdjudicationNotClosed},
	}
	entries := []struct {
		name string
		run  func(t *testing.T, db *sql.DB, labels []string) error
	}{
		{"gate create", func(t *testing.T, db *sql.DB, labels []string) error {
			t.Helper()
			target := newBead(t, db, "n0")
			return inTx(t, db, func(tx *sql.Tx) error {
				_, err := GateCreate(context.Background(), tx, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "s"}, Labels: append([]string{"kind:human"}, labels...), Namespace: "lm", Actor: "u", Now: "n1"})
				return err
			})
		}},
		{"create --type gate", func(t *testing.T, db *sql.DB, labels []string) error {
			t.Helper()
			return inTx(t, db, func(tx *sql.Tx) error {
				_, err := CreateBead(context.Background(), tx, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Labels: append([]string{"kind:confirm"}, labels...), Namespace: "lm", Actor: "u", Now: "n1"})
				return err
			})
		}},
		{"update --add-label", func(t *testing.T, db *sql.DB, labels []string) error {
			t.Helper()
			g := newJudge(t, db, "kind:adjudicate")
			return inTx(t, db, func(tx *sql.Tx) error {
				_, err := UpdateBead(context.Background(), tx, UpdateInput{ID: g, Priority: -1, AddLabels: append([]string{"kind:human"}, labels...), Actor: "u", Now: "n1"})
				return err
			})
		}},
	}
	for _, e := range entries {
		for _, c := range cases {
			t.Run(e.name+"/"+c.name, func(t *testing.T) {
				db := testDB(t)
				labels := c.extra(t, db)
				before := countGates(t, db)
				wantPath(t, e.run(t, db, labels), c.path)
				if e.name != "update --add-label" && countGates(t, db) != before {
					t.Fatalf("a rejected create wrote a Gate")
				}
			})
		}
		t.Run(e.name+"/accepted", func(t *testing.T) {
			db := testDB(t)
			if err := e.run(t, db, []string{judgeLabel(t, db)}); err != nil {
				t.Fatalf("closed kind:adjudicate judge rejected: %v", err)
			}
		})
	}
}

func countGates(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM beads WHERE type = 'gate'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestGateAdjudicationStoresResolvedBeadID(t *testing.T) {
	db := testDB(t)
	judge := newJudge(t, db, "kind:adjudicate")
	resolveJudge(t, db, judge)
	alias := mustDisplayID(t, db, judge)
	g := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Labels: []string{"kind:human", AdjudicatedByPrefix + alias}, Namespace: "lm", Actor: "u", Now: "n1"})
	b, err := GetBead(context.Background(), db, g)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(b.Labels, AdjudicatedByPrefix+judge) {
		t.Fatalf("labels = %v, want %s%s", b.Labels, AdjudicatedByPrefix, judge)
	}
}

func TestGateAdjudicationRejectsRemovingRecordFromPersonGate(t *testing.T) {
	db := testDB(t)
	label := judgeLabel(t, db)
	g := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Labels: []string{"kind:human", label}, Namespace: "lm", Actor: "u", Now: "n1"})
	err := inTx(t, db, func(tx *sql.Tx) error {
		_, err := UpdateBead(context.Background(), tx, UpdateInput{ID: g, Priority: -1, RemoveLabels: []string{label}, Actor: "u", Now: "n2"})
		return err
	})
	wantPath(t, err, AdjudicationMissing)
}

func TestGateAdjudicationLeavesOtherLabelChangesAlone(t *testing.T) {
	db := testDB(t)
	g := newJudge(t, db, "kind:adjudicate")
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET labels = '["kind:human"]' WHERE id = ?`, g); err != nil {
		t.Fatal(err)
	}
	for _, in := range []UpdateInput{
		{ID: g, Priority: -1, AddLabels: []string{"wait:date:2026-10-01"}, Actor: "u", Now: "n1"},
		{ID: g, Priority: -1, RemoveLabels: []string{"kind:human"}, Actor: "u", Now: "n2"},
		{ID: newBead(t, db, "n0"), Priority: -1, AddLabels: []string{"kind:human"}, Actor: "u", Now: "n3"},
	} {
		if err := inTx(t, db, func(tx *sql.Tx) error { _, err := UpdateBead(context.Background(), tx, in); return err }); err != nil {
			t.Fatalf("update %+v: %v", in, err)
		}
	}
}
