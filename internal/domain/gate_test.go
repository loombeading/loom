// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func mustGateCreate(t *testing.T, db *sql.DB, in GateCreateInput) string {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := GateCreate(context.Background(), tx, in)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("GateCreate: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGateCreateBlocksTargetsInOneTransaction(t *testing.T) {
	db := testDB(t)
	target1 := newBead(t, db, "n0")
	target2 := newBead(t, db, "n0")

	gateID := mustGateCreate(t, db, GateCreateInput{
		Blocks:    []string{target1, target2},
		Fields:    GateFields{Subject: "CI green"},
		Namespace: "lm",
		Actor:     "u",
		Now:       "n1",
	})

	g, err := GetBead(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	if g.BeadType != "gate" {
		t.Errorf("gate type = %q, want gate", g.BeadType)
	}
	if !g.Description.Valid || g.Description.String != "CI green" {
		t.Errorf("gate description = %+v, want %q", g.Description, "CI green")
	}
	if want := "gate: CI green"; g.Title != want {
		t.Errorf("gate title = %q, want %q", g.Title, want)
	}
	if g.Status != StatusOpen {
		t.Errorf("gate status = %q, want open", g.Status)
	}

	ids, err := ReadyIDSet(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if ids[target1] || ids[target2] {
		t.Errorf("ready ids = %v, want target1/target2 blocked by the gate", ids)
	}
}

func TestGateCreateRequiresBlocksAndAwait(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := GateCreate(context.Background(), tx, GateCreateInput{Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "u", Now: "n1"}); err == nil {
		t.Error("GateCreate with no --blocks = nil error, want an error")
	}
	if _, err := GateCreate(context.Background(), tx, GateCreateInput{Blocks: []string{target}, Namespace: "lm", Actor: "u", Now: "n1"}); err == nil {
		t.Error("GateCreate with no --await = nil error, want an error")
	}
}

func TestGateResolveRequiresReason(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "u", Now: "n1"})

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := GateResolve(context.Background(), tx, GateResolveInput{ID: gateID, Actor: "u", Now: "n2"}); err == nil {
		t.Fatal("GateResolve with no reason = nil error, want an error")
	}
}

func TestGateResolveOpensBlockedTargetAndAudits(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "u", Now: "n1"})

	newly, err := NewlyReadyDiff(context.Background(), db, func() error {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			return err
		}
		if err := GateResolve(context.Background(), tx, GateResolveInput{ID: gateID, Reason: "CI passed", Actor: "watcher", Now: "n2"}); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsID(newly, target) {
		t.Errorf("newly ready = %v, want %q", newly, target)
	}

	g, err := GetBead(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusClosed {
		t.Errorf("gate status after resolve = %q, want closed", g.Status)
	}

	rows, err := AuditHistory(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Field.Valid && r.Field.String == FieldGateResolve && r.NewValue.Valid && r.NewValue.String == StatusClosed {
			if !r.Reason.Valid || r.Reason.String != "CI passed" {
				t.Errorf("gate_resolve audit reason = %+v, want %q", r.Reason, "CI passed")
			}
			found = true
		}
		if r.Field.Valid && r.Field.String == "status" && r.NewValue.Valid && r.NewValue.String == StatusClosed {
			t.Error("gate resolve wrote a plain field=status record; want it distinguished as field=gate_resolve only")
		}
	}
	if !found {
		t.Errorf("audit history = %+v, want a field=gate_resolve record", rows)
	}
}

func TestGateResolveByAnyActor(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "creator", Now: "n1"})

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := GateResolve(context.Background(), tx, GateResolveInput{ID: gateID, Reason: "ok", Actor: "someone-else", Now: "n2"}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("GateResolve by a different actor: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestGateClaimRejected(t *testing.T) {
	db := testDB(t)
	gateID := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})

	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: gateID, Claim: true, Actor: "u", Now: "n1"}); err == nil {
		t.Fatal("claiming a gate Bead = nil error, want an error")
	}
}

func TestGateCancelAndReopenRejected(t *testing.T) {
	db := testDB(t)
	gateID := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})

	err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: gateID, Cancel: true, Actor: "u", Now: "n1"})
	if !errors.Is(err, ErrGateTerminalOnlyResolve) {
		t.Fatalf("cancelling a gate Bead = %v, want ErrGateTerminalOnlyResolve", err)
	}
	g, gerr := GetBead(context.Background(), db, gateID)
	if gerr != nil {
		t.Fatal(gerr)
	}
	if g.Status != StatusOpen {
		t.Errorf("gate status after rejected cancel = %q, want open (unchanged)", g.Status)
	}

	err = mustUpdate(t, db, UpdateInput{Priority: -1, ID: gateID, Reopen: true, Actor: "u", Now: "n2"})
	if !errors.Is(err, ErrGateTerminalOnlyResolve) {
		t.Fatalf("reopening a gate Bead = %v, want ErrGateTerminalOnlyResolve", err)
	}
}

func mustGateReject(t *testing.T, db *sql.DB, in GateRejectInput) []string {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := GateReject(context.Background(), tx, in)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("GateReject: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestGateRejectRequiresReason(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "u", Now: "n1"})

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := GateReject(context.Background(), tx, GateRejectInput{ID: gateID, Actor: "u", Now: "n2"}); err == nil {
		t.Fatal("GateReject with no reason = nil error, want an error")
	}
}

func TestGateRejectRejectsNonGate(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "t", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := GateReject(context.Background(), tx, GateRejectInput{ID: id, Reason: "no", Actor: "u", Now: "n1"}); err == nil {
		t.Fatal("GateReject on a non-gate Bead = nil error, want an error")
	}
}

func TestGateRejectCancelsBlockedOpenAndInProgressBeadsAndTheGate(t *testing.T) {
	db := testDB(t)
	target1 := newBead(t, db, "n0")
	target2 := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target1, target2}, Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "u", Now: "n1"})

	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: target2, Claim: true, Force: true, Reason: "r", Actor: "alice", Now: "n2"}); err != nil {
		t.Fatal(err)
	}

	cancelled := mustGateReject(t, db, GateRejectInput{ID: gateID, Reason: "won't do", Actor: "u", Now: "n3"})
	if !containsID(cancelled, target1) || !containsID(cancelled, target2) {
		t.Errorf("cancelled = %v, want both %q and %q", cancelled, target1, target2)
	}

	g, err := GetBead(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != StatusCancelled {
		t.Errorf("gate status after reject = %q, want cancelled", g.Status)
	}

	b1, err := GetBead(context.Background(), db, target1)
	if err != nil {
		t.Fatal(err)
	}
	if b1.Status != StatusCancelled {
		t.Errorf("target1 status = %q, want cancelled", b1.Status)
	}

	b2, err := GetBead(context.Background(), db, target2)
	if err != nil {
		t.Fatal(err)
	}
	if b2.Status != StatusCancelled {
		t.Errorf("target2 status = %q, want cancelled", b2.Status)
	}
	if b2.ClaimedBy.Valid && b2.ClaimedBy.String != "" {
		t.Errorf("target2 claimed_by = %+v, want cleared (claim released before cancel)", b2.ClaimedBy)
	}

	rows, err := AuditHistory(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Field.Valid && r.Field.String == FieldGateReject && r.NewValue.Valid && r.NewValue.String == StatusCancelled {
			if !r.Reason.Valid || r.Reason.String != "won't do" {
				t.Errorf("gate_reject audit reason = %+v, want %q", r.Reason, "won't do")
			}
			found = true
		}
	}
	if !found {
		t.Errorf("gate audit history = %+v, want a field=gate_reject record", rows)
	}

	targetRows, err := AuditHistory(context.Background(), db, target1)
	if err != nil {
		t.Fatal(err)
	}
	foundCancel := false
	for _, r := range targetRows {
		if r.Field.Valid && r.Field.String == "status" && r.NewValue.Valid && r.NewValue.String == StatusCancelled {
			if !r.Reason.Valid || r.Reason.String != "won't do" {
				t.Errorf("target1 cancel audit reason = %+v, want %q", r.Reason, "won't do")
			}
			foundCancel = true
		}
	}
	if !foundCancel {
		t.Errorf("target1 audit history = %+v, want a field=status/cancelled record", targetRows)
	}
}

func TestGateRejectRefusesUnfinishedDependents(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	downstream := newBead(t, db, "n0")
	linkTx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddLink(context.Background(), linkTx, "u", downstream, target, BlocksDepType, "n0", ""); err != nil {
		_ = linkTx.Rollback()
		t.Fatal(err)
	}
	if err := linkTx.Commit(); err != nil {
		t.Fatal(err)
	}
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "u", Now: "n1"})

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = GateReject(context.Background(), tx, GateRejectInput{ID: gateID, Reason: "won't do", Actor: "u", Now: "n2"})
	_ = tx.Rollback()
	var inv *TerminalInvariantError
	if !errors.As(err, &inv) || inv.Reason != "unfinished dependents: "+displayIDOf(t, db, downstream) {
		t.Fatalf("GateReject err = %v, want TerminalInvariantError naming %q", err, downstream)
	}
	for _, id := range []string{gateID, target} {
		b, err := GetBead(context.Background(), db, id)
		if err != nil || b.Status != StatusOpen {
			t.Errorf("GetBead(%q) = %v, %v; want status %q after a refused reject", id, b.Status, err, StatusOpen)
		}
	}
}

func TestGateListShowsOpenGatesWithBlockingTargets(t *testing.T) {
	db := testDB(t)
	target1 := newBead(t, db, "n0")
	target2 := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target1, target2}, Fields: GateFields{Subject: "wait for it"}, Namespace: "lm", Actor: "u", Now: "n1"})
	bareGateID := mustCreate(t, db, CreateInput{Title: "bare", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n1"})

	entries, _, err := GateListEffective(context.Background(), db, "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]GateListEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	e, ok := byID[gateID]
	if !ok {
		t.Fatalf("gate list = %+v, want an entry for %q", entries, gateID)
	}
	if e.Fields.Subject != "wait for it" {
		t.Errorf("gate entry Fields.Subject = %q, want %q", e.Fields.Subject, "wait for it")
	}
	if len(e.Blocking) != 2 {
		t.Errorf("gate entry Blocking = %+v, want 2 targets", e.Blocking)
	}
	if _, ok := byID[bareGateID]; !ok {
		t.Errorf("gate list = %+v, want the blocks-less Gate %q to still appear", entries, bareGateID)
	}
}

func TestGatePrereqBlockersFindsOtherOpenBlocker(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	prereq := newBead(t, db, "n0")
	mustNoErr(t, mustAddLink(t, db, target, prereq, BlocksDepType))
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "ship it"}, Namespace: "lm", Actor: "u", Now: "n1"})

	blocking, err := GateBlocking(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GatePrereqBlockers(context.Background(), db, gateID, blocking)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != prereq {
		t.Errorf("GatePrereqBlockers = %+v, want exactly [%q]", got, prereq)
	}
}

func TestGatePrereqBlockersEmptyOnceOnlyGateBlocks(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "ship it"}, Namespace: "lm", Actor: "u", Now: "n1"})

	blocking, err := GateBlocking(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GatePrereqBlockers(context.Background(), db, gateID, blocking)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("GatePrereqBlockers = %+v, want none once the Gate is the only blocker", got)
	}
}

func TestGatePrereqBlockersEmptyWhenAnyTargetIsFreedByGateAlone(t *testing.T) {
	db := testDB(t)
	held := newBead(t, db, "n0")
	prereq := newBead(t, db, "n0")
	mustNoErr(t, mustAddLink(t, db, held, prereq, BlocksDepType))
	free := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{held, free}, Fields: GateFields{Subject: "ship it"}, Namespace: "lm", Actor: "u", Now: "n1"})

	blocking, err := GateBlocking(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GatePrereqBlockers(context.Background(), db, gateID, blocking)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("GatePrereqBlockers = %+v, want none when resolving the Gate frees one of its targets", got)
	}
}

func TestGateListBreaksHumanPrereqCycleAtFirstGate(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	first := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "a"}, Labels: []string{"kind:human", judgeLabel(t, db)}, Namespace: "lm", Actor: "u", Now: "n1"})
	second := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "b"}, Labels: []string{"kind:confirm", judgeLabel(t, db)}, Namespace: "lm", Actor: "u", Now: "n2"})

	entries, _, err := GateListEffective(context.Background(), db, "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]GateListEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	if got := byID[first].PrereqBlockers; len(got) != 0 {
		t.Errorf("first Gate PrereqBlockers = %+v, want none (cycle head goes to 操作待ち)", got)
	}
	if got := byID[second].PrereqBlockers; len(got) != 1 || got[0].ID != first {
		t.Errorf("second Gate PrereqBlockers = %+v, want exactly [%q]", got, first)
	}
}

func TestGateListKeepsPrereqBehindNonHumanGate(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	human := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "a"}, Labels: []string{"kind:human", judgeLabel(t, db)}, Namespace: "lm", Actor: "u", Now: "n1"})
	external := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "b", Resolver: "ci"}, Labels: []string{"kind:external"}, Namespace: "lm", Actor: "u", Now: "n2"})

	entries, _, err := GateListEffective(context.Background(), db, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ID == human && (len(e.PrereqBlockers) != 1 || e.PrereqBlockers[0].ID != external) {
			t.Errorf("human Gate PrereqBlockers = %+v, want exactly [%q]", e.PrereqBlockers, external)
		}
	}
}

func TestGatePrereqBlockersIgnoresTerminalOtherBlocker(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	prereq := newBead(t, db, "n0")
	mustNoErr(t, mustAddLink(t, db, target, prereq, BlocksDepType))
	mustNoErr(t, mustCloseBead(t, db, prereq, "n1"))
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "ship it"}, Namespace: "lm", Actor: "u", Now: "n2"})

	blocking, err := GateBlocking(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GatePrereqBlockers(context.Background(), db, gateID, blocking)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("GatePrereqBlockers = %+v, want none once the other blocker is closed", got)
	}
}

func TestGatePrereqBlockersIncludesGatesOwnBlocker(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	other := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "ship it"}, Namespace: "lm", Actor: "u", Now: "n1"})
	extGate := mustGateCreate(t, db, GateCreateInput{Blocks: []string{other}, Fields: GateFields{Subject: "wait for limit", Resolver: "runner"}, Labels: []string{"kind:external"}, Namespace: "lm", Actor: "u", Now: "n2"})
	mustNoErr(t, mustAddLink(t, db, gateID, extGate, BlocksDepType))

	blocking, err := GateBlocking(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GatePrereqBlockers(context.Background(), db, gateID, blocking)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != extGate {
		t.Errorf("GatePrereqBlockers = %+v, want exactly [%q] (the Gate's own blocker)", got, extGate)
	}
}

func TestGatePrereqBlockersSkipsClosedBlockee(t *testing.T) {
	db := testDB(t)
	open := newBead(t, db, "n0")
	prereq := newBead(t, db, "n0")
	mustNoErr(t, mustAddLink(t, db, open, prereq, BlocksDepType))
	closedTarget := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{open, closedTarget}, Fields: GateFields{Subject: "ship it"}, Namespace: "lm", Actor: "u", Now: "n1"})
	mustNoErr(t, mustCloseBead(t, db, closedTarget, "n2"))

	blocking, err := GateBlocking(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GatePrereqBlockers(context.Background(), db, gateID, blocking)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != prereq {
		t.Errorf("GatePrereqBlockers = %+v, want exactly [%q] (closed blockee ignored, not counted as freeing the Gate)", got, prereq)
	}
}

func TestGatePrereqBlockersQueryError(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "ship it"}, Namespace: "lm", Actor: "u", Now: "n1"})
	blocking, err := GateBlocking(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	q := &errAfterNQueries{Querier: db, n: 1}
	if _, err := GatePrereqBlockers(context.Background(), q, gateID, blocking); err == nil {
		t.Fatal("GatePrereqBlockers with a failing query: err = nil, want an error")
	}
}

func TestGatePrereqBlockersTerminalCheckQueryError(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "ship it"}, Namespace: "lm", Actor: "u", Now: "n1"})
	blocking, err := GateBlocking(context.Background(), db, gateID)
	if err != nil {
		t.Fatal(err)
	}
	q := &errAfterNQueries{Querier: db, n: 3}
	if _, err := GatePrereqBlockers(context.Background(), q, gateID, blocking); err == nil {
		t.Fatal("GatePrereqBlockers with a failing terminal-check query: err = nil, want an error")
	}
}

func TestGateListPropagatesPrereqBlockersQueryError(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	_ = mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "ship it"}, Namespace: "lm", Actor: "u", Now: "n1"})
	q := &errAfterNQueries{Querier: db, n: 3}
	if _, _, err := GateListEffective(context.Background(), q, ""); err == nil {
		t.Fatal("GateListEffective with a failing prereq-blockers query: err = nil, want an error")
	}
}

func TestGateExcludedFromReadyAndBlocked(t *testing.T) {
	db := testDB(t)
	blocker := newBead(t, db, "n0")
	gateID := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})
	mustNoErr(t, mustAddLink(t, db, gateID, blocker, BlocksDepType))

	blocked, _, _, err := BlockedBeadsEffective(context.Background(), db, ReadyFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range blocked {
		if b.ID == gateID {
			t.Errorf("blocked = %+v, must not include the gate %q", blocked, gateID)
		}
	}
}

func TestReadyBlockedUnionExcludesGatesAndUnfinishedEpics(t *testing.T) {
	db := testDB(t)
	plain := newBead(t, db, "n0")
	blockedPlain := newBead(t, db, "n0")
	blockerForPlain := newBead(t, db, "n0")
	mustNoErr(t, mustAddLink(t, db, blockedPlain, blockerForPlain, BlocksDepType))

	gate := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})

	epic := mustCreate(t, db, CreateInput{Title: "e", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})
	child := mustCreate(t, db, CreateInput{Title: "c", Priority: 2, BeadType: "task", Namespace: "lm", Parent: epic, Actor: "u", Now: "n0"})
	_ = child

	rows, err := db.QueryContext(context.Background(), `SELECT id FROM beads WHERE status = ?`, StatusOpen)
	if err != nil {
		t.Fatal(err)
	}
	openIDs, err := collectRows(rows, scanString)
	if err != nil {
		t.Fatal(err)
	}

	readyIDs, err := ReadyIDSet(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	blockedRows, _, _, err := BlockedBeadsEffective(context.Background(), db, ReadyFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	blockedIDs := map[string]bool{}
	for _, b := range blockedRows {
		blockedIDs[b.ID] = true
	}

	union := map[string]bool{}
	for id := range readyIDs {
		union[id] = true
	}
	for id := range blockedIDs {
		if readyIDs[id] {
			t.Errorf("id %q is in both ready and blocked", id)
		}
		union[id] = true
	}

	if !blockedIDs[epic] {
		t.Errorf("blocked ids = %v, want the unfinished-child epic %q to appear in blocked (not excluded from the union)", blockedIDs, epic)
	}

	excluded := map[string]bool{gate: true}
	for _, id := range openIDs {
		if excluded[id] {
			if union[id] {
				t.Errorf("id %q (gate) unexpectedly in ready/blocked union", id)
			}
			continue
		}
		if !union[id] {
			t.Errorf("id %q missing from ready/blocked union", id)
		}
	}
	_ = plain
	_ = blockedPlain
	_ = blockerForPlain
}

func TestGateTitle(t *testing.T) {
	short := "CI green"
	if got, want := GateTitle(short), "gate: CI green"; got != want {
		t.Errorf("GateTitle(%q) = %q, want %q", short, got, want)
	}

	exact60 := strings.Repeat("a", 60)
	if got, want := GateTitle(exact60), "gate: "+exact60; got != want {
		t.Errorf("GateTitle(60 runes) = %q, want %q (no truncation at exactly 60)", got, want)
	}

	over60 := strings.Repeat("a", 61)
	wantOver := "gate: " + strings.Repeat("a", 60) + "…"
	if got := GateTitle(over60); got != wantOver {
		t.Errorf("GateTitle(61 runes) = %q, want %q", got, wantOver)
	}

	multiByte := strings.Repeat("承", 65)
	wantMultiByte := "gate: " + strings.Repeat("承", 60) + "…"
	if got := GateTitle(multiByte); got != wantMultiByte {
		t.Errorf("GateTitle(65 multi-byte runes) = %q, want %q", got, wantMultiByte)
	}
}

func TestGateKindOf(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		await  string
		want   string
	}{
		{"no label", nil, "CI green", "unknown"},
		{"no label, agent-shaped await text is not read as a prefix", nil, "agent: 別セッションの完了待ち", "unknown"},
		{"kind:human label", []string{"kind:human"}, "承認待ち", "human"},
		{"kind:confirm label", []string{"kind:confirm"}, "承認待ち", "human"},
		{"kind:adjudicate label", []string{"kind:adjudicate"}, "判定待ち", "adjudicate"},
		{"kind:adjudicate escalated with kind:human is human", []string{"kind:adjudicate", "kind:human"}, "判定待ち", "human"},
		{"kind:agent label is unrecognized now that agent is removed", []string{"kind:agent"}, "CI green", "unknown"},
		{"kind:external label", []string{"kind:external"}, "webhook fired", "external"},
		{"kind:external label overrides agent-shaped await text", []string{"kind:external"}, "agent: someone will resolve", "external"},
		{"kind:confirm and kind:external both present, external wins", []string{"kind:confirm", "kind:external"}, "plain wait", "external"},
		{"unrecognized label alone is unknown", []string{"some-other-label"}, "plain wait", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GateKindOf(tc.labels, tc.await); got != tc.want {
				t.Errorf("GateKindOf(%v, %q) = %q, want %q", tc.labels, tc.await, got, tc.want)
			}
		})
	}
}

func TestGateWaitProgressQueryErrorOnClosedDB(t *testing.T) {
	db := testDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := GateWaitProgress(context.Background(), db, "g1"); err == nil {
		t.Fatal("GateWaitProgress on a closed DB: err = nil, want an error")
	}
}

func TestGateWaitProgressPropagatesChildQueryError(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "CI green", Resolver: "watcher"}, Namespace: "lm", Labels: []string{"kind:external"}, Actor: "u", Now: "n1"})
	q := &errAfterNQueries{Querier: db, n: 2}

	if _, err := GateWaitProgress(context.Background(), q, gateID); err == nil {
		t.Fatal("GateWaitProgress with the children query failing: err = nil, want an error")
	}
	if q.count < 2 {
		t.Fatalf("errAfterNQueries.count = %d, want at least 2 (blocked query then children query ran)", q.count)
	}
}

func dropNotNull(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	mustExec(t, db, fmt.Sprintf("ALTER TABLE %s RENAME TO %s_orig", table, table))
	mustExec(t, db, fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM %s_orig", table, table))
	mustExec(t, db, fmt.Sprintf("DROP TABLE %s_orig", table))
}

func TestGateWaitProgressScanErrorOnBlockedBead(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "CI green", Resolver: "watcher"}, Namespace: "lm", Labels: []string{"kind:external"}, Actor: "u", Now: "n1"})
	dropNotNull(t, db, "beads")
	mustExec(t, db, "UPDATE beads SET namespace = NULL WHERE id = ?", target)

	if _, err := GateWaitProgress(context.Background(), db, gateID); err == nil {
		t.Fatal("GateWaitProgress with the blocked Bead's namespace NULL: err = nil, want a scan error")
	}
}

func TestGateWaitProgressScanErrorOnChild(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := newBead(t, db, "n0")
	mustNoErr(t, mustAddLink(t, db, child, parent, ParentChildDepType))
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{parent}, Fields: GateFields{Subject: "子 Bead の完了待ち", Resolver: "watcher"}, Namespace: "lm", Labels: []string{"kind:external"}, Actor: "u", Now: "n1"})
	dropNotNull(t, db, "beads")
	mustExec(t, db, "UPDATE beads SET namespace = NULL WHERE id = ?", child)

	if _, err := GateWaitProgress(context.Background(), db, gateID); err == nil {
		t.Fatal("GateWaitProgress with a child's namespace NULL: err = nil, want a scan error")
	}
}
