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

type unfinishedChildrenQueryOverride struct {
	*sql.DB

	forceQueryErr error
	forceRowsSQL  string
}

func (q unfinishedChildrenQueryOverride) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "pc.depends_on_id = ?") {
		if q.forceQueryErr != nil {
			return nil, q.forceQueryErr
		}
		if q.forceRowsSQL != "" {
			return q.DB.QueryContext(ctx, q.forceRowsSQL)
		}
	}
	return q.DB.QueryContext(ctx, query, args...)
}

func readyIDs(t *testing.T, db *sql.DB) []string {
	t.Helper()
	beads, _, err := ReadyBeads(context.Background(), db, ReadyFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(beads))
	for i, b := range beads {
		ids[i] = b.ID
	}
	return ids
}

func containsID(ids []string, id string) bool {
	return slices.Contains(ids, id)
}

func TestReadyExcludesBlockedByOpenDependency(t *testing.T) {
	db := testDB(t)
	blocker := newBead(t, db, "n0")
	blocked := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, blocked, blocker, BlocksDepType))

	ids := readyIDs(t, db)
	if containsID(ids, blocked) {
		t.Errorf("ready ids = %v, must not include the blocked Bead %q", ids, blocked)
	}
	if !containsID(ids, blocker) {
		t.Errorf("ready ids = %v, must include the unblocked blocker %q", ids, blocker)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseBead(context.Background(), tx, CloseInput{ID: blocker, Actor: "u", Now: "n2"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	ids = readyIDs(t, db)
	if !containsID(ids, blocked) {
		t.Errorf("ready ids after closing the blocker = %v, want it to include %q", ids, blocked)
	}
}

func TestReadyIgnoresCancelledBlocker(t *testing.T) {
	db := testDB(t)
	blocker := newBead(t, db, "n0")
	blocked := newBead(t, db, "n1")
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: blocker, Cancel: true, Actor: "u", Now: "n2"}))
	mustNoErr(t, mustAddLink(t, db, blocked, blocker, BlocksDepType))

	ids := readyIDs(t, db)
	if !containsID(ids, blocked) {
		t.Errorf("ready ids = %v, want the Bead blocked only by a cancelled Bead to be Ready", ids)
	}
}

func TestReadyIgnoresRemovedLink(t *testing.T) {
	db := testDB(t)
	blocker := newBead(t, db, "n0")
	blocked := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, blocked, blocker, BlocksDepType))

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(context.Background(), tx, "u", blocked, blocker, BlocksDepType, "n2", ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	ids := readyIDs(t, db)
	if !containsID(ids, blocked) {
		t.Errorf("ready ids = %v, want it to include %q once its blocking link is removed", ids, blocked)
	}
}

func TestReadyIgnoresDanglingDependency(t *testing.T) {
	db := testDB(t)
	blocked := newBead(t, db, "n0")

	_, err := db.ExecContext(t.Context(), `
		INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed)
		VALUES (?, 'deadbeefdeadbeefdeadbeefdeadbeef', 'blocks', 'n1', 0)`, blocked)
	if err != nil {
		t.Fatal(err)
	}

	ids := readyIDs(t, db)
	if !containsID(ids, blocked) {
		t.Errorf("ready ids = %v, want a Bead with only a dangling blocker to be Ready", ids)
	}
}

func TestReadyExcludesGate(t *testing.T) {
	db := testDB(t)
	gate := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})

	ids := readyIDs(t, db)
	if containsID(ids, gate) {
		t.Errorf("ready ids = %v, must not include the gate %q", ids, gate)
	}
}

func TestReadyAndBlockedArePartition(t *testing.T) {
	db := testDB(t)
	blocker := newBead(t, db, "n0")
	blocked := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, blocked, blocker, BlocksDepType))
	independent := newBead(t, db, "n2")
	closedBead := mustCreate(t, db, CreateInput{Title: "c", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n3"})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseBead(context.Background(), tx, CloseInput{ID: closedBead, Actor: "u", Now: "n4"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	ready, _, err := ReadyBeads(context.Background(), db, ReadyFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	blockedBeads, _, _, err := BlockedBeadsEffective(context.Background(), db, ReadyFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}

	readySet := map[string]bool{}
	for _, b := range ready {
		readySet[b.ID] = true
	}
	blockedSet := map[string]bool{}
	for _, b := range blockedBeads {
		if readySet[b.ID] {
			t.Errorf("bead %q is in both ready and blocked", b.ID)
		}
		blockedSet[b.ID] = true
	}

	all, _, err := ListBeads(context.Background(), db, ListFilter{Status: StatusOpen, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range all {
		if !readySet[b.ID] && !blockedSet[b.ID] {
			t.Errorf("open bead %q is in neither ready nor blocked", b.ID)
		}
	}
	if !readySet[blocker] || !readySet[independent] {
		t.Errorf("readySet = %v, want it to include the blocker and the independent Bead", readySet)
	}
	if !blockedSet[blocked] {
		t.Errorf("blockedSet = %v, want it to include %q", blockedSet, blocked)
	}
	if readySet[closedBead] || blockedSet[closedBead] {
		t.Errorf("closed Bead %q must appear in neither set", closedBead)
	}
}

func TestReadyExcludesParentWithOpenChild(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, child, parent, ParentChildDepType))

	ids := readyIDs(t, db)
	if containsID(ids, parent) {
		t.Errorf("ready ids = %v, must not include the parent %q with an open child", ids, parent)
	}
	if !containsID(ids, child) {
		t.Errorf("ready ids = %v, want it to include the open child %q", ids, child)
	}
}

func TestReadyExcludesParentWithInProgressChild(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, child, parent, ParentChildDepType))

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: child, Claim: true, Actor: "u", Now: "n2"}))

	ids := readyIDs(t, db)
	if containsID(ids, parent) {
		t.Errorf("ready ids = %v, must not include the parent %q while its child is in_progress (not terminal)", ids, parent)
	}
}

func TestReadyIncludesParentWhenAllChildrenClosed(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, child, parent, ParentChildDepType))

	ids := readyIDs(t, db)
	if containsID(ids, parent) {
		t.Errorf("ready ids = %v, must not include the parent %q before its child closes", ids, parent)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseBead(context.Background(), tx, CloseInput{ID: child, Actor: "u", Now: "n2"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	ids = readyIDs(t, db)
	if !containsID(ids, parent) {
		t.Errorf("ready ids = %v, want it to include the parent %q once its only child is closed", ids, parent)
	}
}

func TestBlockedIncludesParentWithOpenChild(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, child, parent, ParentChildDepType))

	rows, _, _, err := BlockedBeadsEffective(context.Background(), db, ReadyFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	var got *BlockedBead
	for i := range rows {
		if rows[i].ID == parent {
			got = &rows[i]
		}
	}
	if got == nil {
		t.Fatalf("blocked beads = %v, want an entry for the parent %q", rows, parent)
	}
	found := false
	for _, bl := range got.Blockers {
		if bl.ID == child {
			found = true
		}
	}
	if !found {
		t.Errorf("blockers of parent %q = %+v, want the open child %q listed", parent, got.Blockers, child)
	}
}

func TestReadyAndBlockedArePartitionWithParentChild(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, child, parent, ParentChildDepType))
	independent := newBead(t, db, "n2")

	ready, _, err := ReadyBeads(context.Background(), db, ReadyFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	blockedBeads, _, _, err := BlockedBeadsEffective(context.Background(), db, ReadyFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}

	readySet := map[string]bool{}
	for _, b := range ready {
		readySet[b.ID] = true
	}
	blockedSet := map[string]bool{}
	for _, b := range blockedBeads {
		if readySet[b.ID] {
			t.Errorf("bead %q is in both ready and blocked", b.ID)
		}
		blockedSet[b.ID] = true
	}

	all, _, err := ListBeads(context.Background(), db, ListFilter{Status: StatusOpen, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range all {
		if !readySet[b.ID] && !blockedSet[b.ID] {
			t.Errorf("open bead %q is in neither ready nor blocked", b.ID)
		}
	}
	if !readySet[child] || !readySet[independent] {
		t.Errorf("readySet = %v, want it to include the child and the independent Bead", readySet)
	}
	if !blockedSet[parent] {
		t.Errorf("blockedSet = %v, want it to include the parent %q", blockedSet, parent)
	}
}

func TestBlockedBeadsListsBlockersWithGatePrefix(t *testing.T) {
	db := testDB(t)
	gate := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})
	task := newBead(t, db, "n1")
	blocked := newBead(t, db, "n2")
	mustNoErr(t, mustAddLink(t, db, blocked, gate, BlocksDepType))
	mustNoErr(t, mustAddLink(t, db, blocked, task, BlocksDepType))

	rows, _, _, err := BlockedBeadsEffective(context.Background(), db, ReadyFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	var got *BlockedBead
	for i := range rows {
		if rows[i].ID == blocked {
			got = &rows[i]
		}
	}
	if got == nil {
		t.Fatalf("blocked beads = %v, want an entry for %q", rows, blocked)
	}
	if len(got.Blockers) != 2 {
		t.Fatalf("blockers = %+v, want 2", got.Blockers)
	}
	foundGate, foundTask := false, false
	for _, bl := range got.Blockers {
		if bl.ID == gate && bl.IsGate {
			foundGate = true
		}
		if bl.ID == task && !bl.IsGate {
			foundTask = true
		}
	}
	if !foundGate || !foundTask {
		t.Errorf("blockers = %+v, want one gate blocker and one plain-task blocker", got.Blockers)
	}
}

func TestNewlyReadyDiffOmitsAlreadyReady(t *testing.T) {
	db := testDB(t)
	blocker := newBead(t, db, "n0")
	blocked := newBead(t, db, "n1")
	alreadyReady := newBead(t, db, "n2")
	mustNoErr(t, mustAddLink(t, db, blocked, blocker, BlocksDepType))

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	newly, err := NewlyReadyDiff(context.Background(), tx, func() error {
		return CloseBead(context.Background(), tx, CloseInput{ID: blocker, Actor: "u", Now: "n3"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if !containsID(newly, blocked) {
		t.Errorf("newly ready = %v, want it to include %q", newly, blocked)
	}
	if containsID(newly, alreadyReady) {
		t.Errorf("newly ready = %v, must not include the already-Ready Bead %q", newly, alreadyReady)
	}
	if containsID(newly, blocker) {
		t.Errorf("newly ready = %v, must not include the Bead that was closed (it's terminal, not open)", newly)
	}
}

func TestNewlyReadyDiffOmitsPreviouslyReadyAcrossTransactions(t *testing.T) {
	db := testDB(t)
	blocker := newBead(t, db, "n0")
	blocked := newBead(t, db, "n1")
	unrelated := newBead(t, db, "n2")
	mustNoErr(t, mustAddLink(t, db, blocked, blocker, BlocksDepType))

	tx1, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	newly1, err := NewlyReadyDiff(context.Background(), tx1, func() error {
		return CloseBead(context.Background(), tx1, CloseInput{ID: blocker, Actor: "u", Now: "n3"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatal(err)
	}
	if !containsID(newly1, blocked) {
		t.Fatalf("first close's newly ready = %v, want it to include %q", newly1, blocked)
	}

	tx2, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	newly2, err := NewlyReadyDiff(context.Background(), tx2, func() error {
		return CloseBead(context.Background(), tx2, CloseInput{ID: unrelated, Actor: "u", Now: "n4"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}
	if containsID(newly2, blocked) {
		t.Errorf("second, unrelated close's newly ready = %v, must not include %q (it became Ready in an earlier, already-committed close)", newly2, blocked)
	}
}

func TestUnfinishedChildrenQueryErrorIsWrapped(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, child, parent, ParentChildDepType))

	q := unfinishedChildrenQueryOverride{DB: db, forceQueryErr: errors.New("forced")}
	blocked, _, _, err := BlockedBeadsEffective(context.Background(), q, ReadyFilter{Priority: -1})
	_ = blocked
	if err == nil || !strings.Contains(err.Error(), "unfinished children: query:") {
		t.Fatalf("err = %v, want it to contain %q", err, "unfinished children: query:")
	}
}

func TestUnfinishedChildrenScanErrorIsWrapped(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, child, parent, ParentChildDepType))

	q := unfinishedChildrenQueryOverride{DB: db, forceRowsSQL: "SELECT 'x'"}
	blocked, _, _, err := BlockedBeadsEffective(context.Background(), q, ReadyFilter{Priority: -1})
	_ = blocked
	if err == nil || !strings.Contains(err.Error(), "unfinished children: scan:") {
		t.Fatalf("err = %v, want it to contain %q", err, "unfinished children: scan:")
	}
}
