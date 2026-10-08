// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

type openBlocksBlockersQueryOverride struct {
	*sql.DB

	forceRowsSQL string
}

func (q openBlocksBlockersQueryOverride) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "b.priority ASC, b.created_at ASC, b.id ASC") && q.forceRowsSQL != "" {
		return q.DB.QueryContext(ctx, q.forceRowsSQL)
	}
	return q.DB.QueryContext(ctx, query, args...)
}

func displayIDOf(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	n, err := LoadShortIDs(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return DisplayID("lm", id, n)
}

func assertOpenUnassigned(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusOpen || b.ClaimedBy.Valid {
		t.Errorf("bead = %+v, want status=open claimed_by=NULL after a rejected claim", b)
	}
}

func TestClaimRejectedByOpenBlocksDependenciesInPriorityOrder(t *testing.T) {
	db := testDB(t)
	target := createForUpdate(t, db)
	low := mustCreate(t, db, CreateInput{Title: "low", Priority: 3, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	high := mustCreate(t, db, CreateInput{Title: "high", Priority: 1, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n2"})
	mustNoErr(t, mustAddLinkAt(t, db, target, low, BlocksDepType, "n3"))
	mustNoErr(t, mustAddLinkAt(t, db, target, high, BlocksDepType, "n3"))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: low, Claim: true, Actor: "carol", Now: "n4"}))

	err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Actor: "alice", Now: "n5"})
	want := "blocked by " + displayIDOf(t, db, high) + ", " + displayIDOf(t, db, low)
	if err == nil || err.Error() != want {
		t.Fatalf("claim err = %v, want %q", err, want)
	}
	assertOpenUnassigned(t, db, target)
}

func TestClaimRejectedByOpenGate(t *testing.T) {
	db := testDB(t)
	target := createForUpdate(t, db)
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "u", Now: "n1"})

	err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Actor: "alice", Now: "n2"})
	if want := "blocked by " + displayIDOf(t, db, gateID); err == nil || err.Error() != want {
		t.Fatalf("claim err = %v, want %q", err, want)
	}
	assertOpenUnassigned(t, db, target)
}

func TestClaimIgnoresTerminalBlockersAndOpenChildren(t *testing.T) {
	db := testDB(t)
	target := createForUpdate(t, db)
	done := createForUpdate(t, db)
	dropped := createForUpdate(t, db)
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: dropped, Cancel: true, Actor: "u", Now: "n1"}))
	mustNoErr(t, mustAddLinkAt(t, db, target, done, BlocksDepType, "n1"))
	mustNoErr(t, mustAddLinkAt(t, db, target, dropped, BlocksDepType, "n1"))
	_ = mustCreate(t, db, CreateInput{Title: "child", Priority: 2, BeadType: "task", Namespace: "lm", Parent: target, Actor: "u", Now: "n1"})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mustNoErr(t, CloseBead(context.Background(), tx, CloseInput{ID: done, Actor: "u", Now: "n2"}))
	mustNoErr(t, tx.Commit())

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Actor: "alice", Now: "n3"}))
}

func TestClaimForceRequiresReason(t *testing.T) {
	db := testDB(t)
	target := createForUpdate(t, db)
	blocker := createForUpdate(t, db)
	mustNoErr(t, mustAddLinkAt(t, db, target, blocker, BlocksDepType, "n1"))

	err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Force: true, Actor: "alice", Now: "n2"})
	if want := "--claim --force requires --reason"; err == nil || err.Error() != want {
		t.Fatalf("claim --force without reason err = %v, want %q", err, want)
	}
	assertOpenUnassigned(t, db, target)
}

func TestClaimForceRecordsForceClaim(t *testing.T) {
	db := testDB(t)
	target := createForUpdate(t, db)
	blocker := createForUpdate(t, db)
	mustNoErr(t, mustAddLinkAt(t, db, target, blocker, BlocksDepType, "n1"))

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Force: true, Reason: "fix the PR", Actor: "alice", Now: "n2"}))
	b, err := GetBead(context.Background(), db, target)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusInProgress || b.ClaimedBy.String != "alice" {
		t.Errorf("bead = %+v, want status=in_progress claimed_by=alice", b)
	}
	rows, err := AuditHistory(context.Background(), db, target)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		switch r.Field.String {
		case FieldForceClaim:
			found = r.NewValue.String == "alice" && r.Reason.String == "fix the PR"
		case FieldClaim:
			t.Errorf("plain claim field recorded for a forced claim: %+v", r)
		}
	}
	if !found {
		t.Error("no force_claim audit record with actor alice and the reason")
	}
}

func TestClaimForceDoesNotTakeAnotherActorsClaim(t *testing.T) {
	db := testDB(t)
	target := createForUpdate(t, db)
	blocker := createForUpdate(t, db)
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Actor: "alice", Now: "n1"}))
	mustNoErr(t, mustAddLinkAt(t, db, target, blocker, BlocksDepType, "n2"))

	err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Force: true, Reason: "r", Actor: "bob", Now: "n3"})
	if want := "already claimed by alice"; err == nil || err.Error() != want {
		t.Fatalf("claim --force over alice err = %v, want %q", err, want)
	}
}

func TestOpenBlocksBlockersQueryErrorOnClosedDB(t *testing.T) {
	db := testDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openBlocksBlockers(context.Background(), db, "x"); err == nil {
		t.Fatal("openBlocksBlockers on a closed DB: err = nil, want an error")
	}
}

func TestOpenBlocksBlockersScanErrorIsWrapped(t *testing.T) {
	db := testDB(t)
	q := openBlocksBlockersQueryOverride{DB: db, forceRowsSQL: "SELECT 1, 2"}
	_, err := openBlocksBlockers(context.Background(), q, "x")
	if err == nil || !strings.Contains(err.Error(), "open blocks blockers: scan:") {
		t.Fatalf("err = %v, want it to contain %q", err, "open blocks blockers: scan:")
	}
}

func TestClaimOpenBlocksBlockersQueryErrorPropagates(t *testing.T) {
	db := testDB(t)
	target := createForUpdate(t, db)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `DROP TABLE dependencies`); err != nil {
		t.Fatal(err)
	}
	_, err = UpdateBead(context.Background(), tx, UpdateInput{Priority: -1, ID: target, Claim: true, Actor: "alice", Now: "n1"})
	if err == nil || !strings.Contains(err.Error(), "open blocks blockers: query:") {
		t.Fatalf("claim err = %v, want it to contain %q", err, "open blocks blockers: query:")
	}
	_ = tx.Rollback()
}
