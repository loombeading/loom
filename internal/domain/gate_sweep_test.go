// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("mustExec(%q): %v", query, err)
	}
}

type errAfterNQueries struct {
	Querier

	n     int
	count int
}

func (e *errAfterNQueries) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	e.count++
	if e.count >= e.n {
		return nil, errors.New("errAfterNQueries: injected query failure")
	}
	return e.Querier.QueryContext(ctx, query, args...)
}

func TestEvaluateGateWaitBlockedAllTerminal(t *testing.T) {
	db := testDB(t)
	target1 := newBead(t, db, "n0")
	target2 := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target1, target2}, Fields: GateFields{Subject: "CI green"}, Namespace: "lm", Actor: "u", Now: "n1"})

	mustNoErr(t, mustCloseBead(t, db, target1, "n2"))
	mustNoErr(t, mustCloseBead(t, db, target2, "n2"))

	met, err := evaluateGateWait(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	if !met {
		t.Fatalf("evaluateGateWait = false, want true (both blocked Beads are terminal)")
	}
}

func TestEvaluateGateWaitNoBlockedNeverResolves(t *testing.T) {
	db := testDB(t)
	gateID := mustCreate(t, db, CreateInput{Title: "bare", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})

	met, err := evaluateGateWait(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	if met {
		t.Errorf("evaluateGateWait = true, want false (no blocked Beads)")
	}
}

func TestGateBlockedBeadsQueryErrorOnClosedDB(t *testing.T) {
	db := testDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := gateBlockedBeads(context.Background(), db, "g1"); err == nil {
		t.Fatal("gateBlockedBeads on a closed DB: err = nil, want an error")
	}
}

func TestEvaluateGateWaitPropagatesGateBlockedBeadsError(t *testing.T) {
	db := testDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := evaluateGateWait(context.Background(), db, "g1"); err == nil {
		t.Fatal("evaluateGateWait on a closed DB: err = nil, want an error")
	}
}

func TestResolveGateIfWaitMetErrorFromEvaluateGateWait(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "CI green"}, Namespace: "lm", Actor: "u", Now: "n1"})
	mustExec(t, db, "DROP TABLE dependencies")

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := resolveGateIfWaitMet(context.Background(), tx, gateID, "u", "n2"); err == nil {
		t.Fatal("resolveGateIfWaitMet with dependencies dropped: err = nil, want an error")
	}
}

func TestResolveGateIfWaitMetErrorFromGateResolve(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "CI green"}, Namespace: "lm", Actor: "u", Now: "n1"})
	mustNoErr(t, mustCloseBead(t, db, target, "n2"))

	func() {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := GateResolve(context.Background(), tx, GateResolveInput{ID: gateID, Reason: "manual", Actor: "u", Now: "n3"}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := resolveGateIfWaitMet(context.Background(), tx, gateID, "u", "n4"); err == nil {
		t.Fatal("resolveGateIfWaitMet on an already-closed gate: err = nil, want a CanTransition rejection")
	}
}

func TestAutoResolveGatesForBeadErrorFromCandidateGates(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	mustExec(t, db, "DROP TABLE dependencies")

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := AutoResolveGatesForBead(context.Background(), tx, target, "u", "n1"); err == nil {
		t.Fatal("AutoResolveGatesForBead with dependencies dropped: err = nil, want an error")
	}
}

func TestGateSweepQueryErrorOnOpenGates(t *testing.T) {
	db := testDB(t)
	mustExec(t, db, "DROP TABLE beads")

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := GateSweep(context.Background(), tx, "u", "n1"); err == nil {
		t.Fatal("GateSweep with beads dropped: err = nil, want an error")
	}
}

func TestGateSweepEvaluateWaitErrorPropagates(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	_ = mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "CI green"}, Namespace: "lm", Actor: "u", Now: "n1"})
	mustExec(t, db, "DROP TABLE dependencies")

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := GateSweep(context.Background(), tx, "u", "n2"); err == nil {
		t.Fatal("GateSweep with dependencies dropped: err = nil, want an error")
	}
}

func TestLookupGateInfoErrorNoSuchGate(t *testing.T) {
	db := testDB(t)
	if _, err := lookupGateInfo(context.Background(), db, "no-such-gate"); err == nil {
		t.Fatal("lookupGateInfo(no-such-gate): err = nil, want an error")
	}
}

func TestLookupGateInfoDecodeLabelsError(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "s"}, Namespace: "lm", Actor: "u", Now: "n1"})
	mustExec(t, db, `UPDATE beads SET labels = 'not json' WHERE id = ?`, gateID)

	if _, err := lookupGateInfo(context.Background(), db, gateID); err == nil {
		t.Fatal("lookupGateInfo with corrupt labels: err = nil, want an error")
	}
}

func TestGateHasDiscoveredFromBeadQueryError(t *testing.T) {
	db := testDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := gateHasDiscoveredFromBead(context.Background(), db, "g1"); err == nil {
		t.Fatal("gateHasDiscoveredFromBead on a closed DB: err = nil, want an error")
	}
}

func TestSpawnAdjudicateNoteHandoffIfNeededErrorFromLookupGateInfo(t *testing.T) {
	db := testDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := spawnAdjudicateNoteHandoffIfNeeded(context.Background(), tx, "no-such-gate", "u", "n1"); err == nil {
		t.Fatal("spawnAdjudicateNoteHandoffIfNeeded(no-such-gate): err = nil, want an error")
	}
}

func TestSpawnAdjudicateNoteHandoffIfNeededErrorFromGateHasDiscoveredFromBead(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "s"}, Namespace: "lm", Labels: []string{"kind:adjudicate"}, Actor: "u", Now: "n1"})
	mustExec(t, db, "DROP TABLE dependencies")

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := spawnAdjudicateNoteHandoffIfNeeded(context.Background(), tx, gateID, "u", "n2"); err == nil {
		t.Fatal("spawnAdjudicateNoteHandoffIfNeeded with dependencies dropped: err = nil, want an error")
	}
}

func TestSpawnAdjudicateNoteHandoffInheritsUnaffiliated(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "s"}, Namespace: "lm", Labels: []string{"kind:adjudicate"}, Actor: "u", Now: "n1"})

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNoErr(t, spawnAdjudicateNoteHandoffIfNeeded(context.Background(), tx, gateID, "u", "n2"))
	mustNoErr(t, tx.Commit())

	var labels string
	if err := db.QueryRowContext(t.Context(), `
		SELECT b.labels FROM beads b JOIN dependencies d ON d.bead_id = b.id
		WHERE d.depends_on_id = ? AND d.type = ? AND d.removed = 0`, gateID, DiscoveredFromDepType).Scan(&labels); err != nil {
		t.Fatal(err)
	}
	if labels != `["`+ExemptMilestoneLabel+`"]` {
		t.Fatalf("labels of the note handoff Bead = %s, want [%q]", labels, ExemptMilestoneLabel)
	}
}

func TestSpawnAdjudicateNoteHandoffIfNeededErrorFromCreateBead(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "s"}, Namespace: "lm", Labels: []string{"kind:adjudicate"}, Actor: "u", Now: "n1"})
	mustExec(t, db, `UPDATE beads SET namespace = '' WHERE id = ?`, gateID)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := spawnAdjudicateNoteHandoffIfNeeded(context.Background(), tx, gateID, "u", "n2"); err == nil {
		t.Fatal("spawnAdjudicateNoteHandoffIfNeeded with corrupt namespace: err = nil, want an error")
	}
}

func TestResolveGateIfWaitMetErrorFromSpawnAdjudicateNoteHandoff(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "s"}, Namespace: "lm", Labels: []string{"kind:adjudicate"}, Actor: "u", Now: "n1"})
	mustNoErr(t, mustCloseBead(t, db, target, "n2"))
	mustExec(t, db, `UPDATE beads SET namespace = '' WHERE id = ?`, gateID)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := resolveGateIfWaitMet(context.Background(), tx, gateID, "u", "n3"); err == nil {
		t.Fatal("resolveGateIfWaitMet with corrupt gate namespace: err = nil, want an error")
	}
}

func TestGateSweepErrorFromSpawnAdjudicateNoteHandoff(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "s"}, Namespace: "lm", Labels: []string{"kind:adjudicate"}, Actor: "u", Now: "n1"})
	mustNoErr(t, mustCloseBead(t, db, target, "n2"))
	mustExec(t, db, `UPDATE beads SET namespace = '' WHERE id = ?`, gateID)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := GateSweep(context.Background(), tx, "u", "n3"); err == nil {
		t.Fatal("GateSweep with corrupt gate namespace: err = nil, want an error")
	}
}

func discoveredFromChild(t *testing.T, db *sql.DB, gateID string) (string, bool) {
	t.Helper()
	var id string
	err := db.QueryRowContext(context.Background(), `
		SELECT bead_id FROM dependencies
		WHERE depends_on_id = ? AND type = ? AND removed = 0`, gateID, DiscoveredFromDepType).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("discoveredFromChild(%s): %v", gateID, err)
	}
	return id, true
}

func TestGateSweepAdjudicateSpawnsNoteHandoff(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{
		Blocks:    []string{target},
		Fields:    GateFields{Subject: "採用するモデルの選定", Current: "現状の文", Proposal: "提案の文", Check: "チェック内容"},
		Namespace: "lm",
		Labels:    []string{"kind:adjudicate"},
		Actor:     "u", Now: "n1",
	})
	mustNoErr(t, mustCloseBead(t, db, target, "n2"))

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GateSweep(context.Background(), tx, "u", "n3"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	childID, found := discoveredFromChild(t, db, gateID)
	if !found {
		t.Fatal("GateSweep on a kind:adjudicate Gate: no discovered-from Bead spawned")
	}
	child, err := GetBead(context.Background(), db, childID)
	if err != nil {
		t.Fatal(err)
	}
	wantTitle := "判定ノート引き継ぎ: 採用するモデルの選定"
	if child.Title != wantTitle {
		t.Errorf("spawned Bead Title = %q, want %q", child.Title, wantTitle)
	}
	gateInfo, err := lookupGateInfo(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	if !child.Description.Valid || child.Description.String != gateInfo.Description {
		t.Errorf("spawned Bead Description = %q, want verbatim Gate description %q", child.Description.String, gateInfo.Description)
	}
	if child.BeadType != "task" {
		t.Errorf("spawned Bead BeadType = %q, want %q", child.BeadType, "task")
	}
}

func TestGateSweepNonAdjudicateSkipsNoteHandoff(t *testing.T) {
	for _, tc := range []struct {
		name        string
		extraLabels func(t *testing.T, db *sql.DB) []string
		labels      []string
	}{
		{"human", nil, []string{"kind:human"}},
		{"confirm", nil, []string{"kind:confirm"}},
		{"external", nil, []string{"kind:external"}},
		{"none", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			target := newBead(t, db, "n0")
			labels := tc.labels
			if tc.name == "human" || tc.name == "confirm" {
				judgeTarget := newBead(t, db, "n0")
				judgeID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{judgeTarget}, Fields: GateFields{Subject: "judge"}, Namespace: "lm", Labels: []string{"kind:adjudicate"}, Actor: "u", Now: "n0"})
				mustNoErr(t, mustCloseBead(t, db, judgeTarget, "n0a"))
				func() {
					tx, err := db.BeginTx(context.Background(), nil)
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = tx.Rollback() }()
					mustNoErr(t, GateResolve(context.Background(), tx, GateResolveInput{ID: judgeID, Reason: "judged", Actor: "u", Now: "n0b"}))
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
				}()
				labels = append(labels, AdjudicatedByPrefix+judgeID)
			}
			gateID := mustCreate(t, db, CreateInput{
				Title:       GateTitle("subject"),
				Description: ComposeGateDescription(GateFields{Subject: "subject"}),
				Priority:    DefaultPriority, BeadType: "gate", Namespace: "lm", Labels: labels, Actor: "u", Now: "n1",
			})
			func() {
				tx, err := db.BeginTx(context.Background(), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback() }()
				mustNoErr(t, AddLink(context.Background(), tx, "u", target, gateID, BlocksDepType, "n1", "wire up"))
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}()
			mustNoErr(t, mustCloseBead(t, db, target, "n2"))

			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := GateSweep(context.Background(), tx, "u", "n3"); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}

			if _, found := discoveredFromChild(t, db, gateID); found {
				t.Errorf("GateSweep on a %s Gate: spawned a discovered-from Bead, want none", tc.name)
			}
		})
	}
}

func TestSpawnAdjudicateNoteHandoffIdempotent(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{
		Blocks: []string{target}, Fields: GateFields{Subject: "subject"}, Namespace: "lm", Labels: []string{"kind:adjudicate"}, Actor: "u", Now: "n1",
	})

	func() {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := spawnAdjudicateNoteHandoffIfNeeded(context.Background(), tx, gateID, "u", "n2"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}()

	func() {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := spawnAdjudicateNoteHandoffIfNeeded(context.Background(), tx, gateID, "u", "n3"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}()

	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM dependencies WHERE depends_on_id = ? AND type = ? AND removed = 0`,
		gateID, DiscoveredFromDepType).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("discovered-from links for gate = %d, want 1 (idempotent across repeated sweeps)", count)
	}
}

func TestAdjudicateNoteTitleTruncation(t *testing.T) {
	short := "短い件名"
	if got, want := adjudicateNoteTitle(short), "判定ノート引き継ぎ: "+short; got != want {
		t.Errorf("adjudicateNoteTitle(%q) = %q, want %q", short, got, want)
	}

	long := strings.Repeat("字", 70)
	got := adjudicateNoteTitle(long)
	wantRunes := []rune(long)[:60]
	want := "判定ノート引き継ぎ: " + string(wantRunes) + "…"
	if got != want {
		t.Errorf("adjudicateNoteTitle(70 runes) = %q, want %q", got, want)
	}
}
