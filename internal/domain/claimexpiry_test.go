// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

var claimT0 = time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

func claimOf(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	return b.ClaimExpiresAt.String
}

func claimAuditCount(t *testing.T, db *sql.DB, id, field string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM audit_log WHERE bead_id = ? AND field = ?`, id, field).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func claimWithClaim(t *testing.T, db *sql.DB, id, actor string, at time.Time, claimTTL time.Duration) error {
	t.Helper()
	return mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: actor, Now: "n", Clock: at, ClaimTTL: claimTTL})
}

func TestClaimClaimWritesExpiryAndRenews(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0, 30*time.Minute))
	if got, want := claimOf(t, db, id), "2026-09-28T00:30:00.000Z"; got != want {
		t.Fatalf("claim = %q, want %q", got, want)
	}
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0.Add(10*time.Minute), time.Hour))
	if got, want := claimOf(t, db, id), "2026-09-28T01:10:00.000Z"; got != want {
		t.Fatalf("renewed claim = %q, want %q", got, want)
	}
	b, err := GetBead(context.Background(), db, id)
	mustNoErr(t, err)
	if b.ClaimedBy.String != "alice" || b.Status != StatusInProgress {
		t.Fatalf("bead = %+v, want in_progress by alice", b)
	}
	if n := claimAuditCount(t, db, id, FieldClaimTakeover); n != 0 {
		t.Fatalf("claim_takeover audits = %d, want 0", n)
	}
}

func TestClaimClaimWithoutClaimIsNull(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0, 0))
	if got := claimOf(t, db, id); got != "" {
		t.Fatalf("claim = %q, want NULL", got)
	}
}

func TestClaimTakeoverRejectedBeforeExpiryOrWithoutClaim(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0, 30*time.Minute))
	err := claimWithClaim(t, db, id, "bob", claimT0.Add(29*time.Minute), time.Hour)
	if err == nil || !strings.Contains(err.Error(), "already claimed by alice") {
		t.Fatalf("takeover before expiry err = %v", err)
	}

	none := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, none, "alice", claimT0, 0))
	err = claimWithClaim(t, db, none, "bob", claimT0.Add(1000*time.Hour), time.Hour)
	if err == nil || !strings.Contains(err.Error(), "already claimed by alice") {
		t.Fatalf("takeover of a claim-less claim err = %v", err)
	}
	err = mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Force: true, Reason: "r", Actor: "bob", Now: "n", Clock: claimT0.Add(time.Minute)})
	if err == nil || !strings.Contains(err.Error(), "already claimed by alice") {
		t.Fatalf("--force must not take a live claim, err = %v", err)
	}
}

func TestClaimTakeoverAfterExpiry(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0, 30*time.Minute))
	mustNoErr(t, claimWithClaim(t, db, id, "bob", claimT0.Add(30*time.Minute), time.Hour))
	b, err := GetBead(context.Background(), db, id)
	mustNoErr(t, err)
	if b.ClaimedBy.String != "bob" || b.Status != StatusInProgress {
		t.Fatalf("bead = %+v, want in_progress by bob", b)
	}
	if got, want := b.ClaimExpiresAt.String, "2026-09-28T01:30:00.000Z"; got != want {
		t.Fatalf("claim = %q, want %q", got, want)
	}
	if n := claimAuditCount(t, db, id, FieldClaimTakeover); n != 1 {
		t.Fatalf("claim_takeover audits = %d, want 1", n)
	}
	if n := claimAuditCount(t, db, id, FieldForceClaim); n != 0 {
		t.Fatalf("force_claim audits = %d, want 0", n)
	}
	mustNoErr(t, claimWithClaim(t, db, id, "carol", claimT0.Add(5*time.Hour), 0))
	if got := claimOf(t, db, id); got != "" {
		t.Fatalf("takeover without --claim-ttl claim = %q, want NULL", got)
	}
}

func TestClaimTakeoverPastBlockersNeedsForceAndRecordsBoth(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	blocker := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0, time.Minute))
	mustNoErr(t, mustAddLinkAt(t, db, id, blocker, BlocksDepType, "n3"))

	late := claimT0.Add(time.Hour)
	err := claimWithClaim(t, db, id, "bob", late, 0)
	if err == nil || !strings.Contains(err.Error(), "blocked by") {
		t.Fatalf("takeover past blocker err = %v", err)
	}
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Force: true, Reason: "r", Actor: "bob", Now: "n", Clock: late}))
	if n := claimAuditCount(t, db, id, FieldClaimTakeover); n != 1 {
		t.Fatalf("claim_takeover audits = %d, want 1", n)
	}
	if n := claimAuditCount(t, db, id, FieldForceClaim); n != 1 {
		t.Fatalf("force_claim audits = %d, want 1", n)
	}
}

func TestClaimInvalidInputsRejected(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	for name, in := range map[string]UpdateInput{
		"negative":   {Claim: true, ClaimTTL: -time.Second},
		"no claim":   {Release: true, ClaimTTL: time.Second},
		"claim only": {ClaimTTL: time.Second},
	} {
		in.ID, in.Priority, in.Actor, in.Now = id, -1, "alice", "n"
		if err := mustUpdate(t, db, in); err == nil {
			t.Errorf("%s: want a validation error", name)
		}
	}
	if got := claimOf(t, db, id); got != "" {
		t.Fatalf("claim = %q after rejected inputs, want NULL", got)
	}
}

func TestClaimClearedByReleaseReopenCancel(t *testing.T) {
	db := testDB(t)
	rel := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, rel, "alice", claimT0, time.Hour))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: rel, Release: true, Actor: "alice", Now: "n"}))
	if got := claimOf(t, db, rel); got != "" {
		t.Fatalf("after release claim = %q, want NULL", got)
	}

	can := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, can, "alice", claimT0, time.Hour))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: can, Cancel: true, Actor: "alice", Now: "n"}))
	if got := claimOf(t, db, can); got != "" {
		t.Fatalf("after cancel claim = %q, want NULL", got)
	}

	reo := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, reo, "alice", claimT0, time.Hour))
	if _, err := db.ExecContext(context.Background(), `UPDATE beads SET status = 'closed' WHERE id = ?`, reo); err != nil {
		t.Fatal(err)
	}
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: reo, Reopen: true, Actor: "alice", Now: "n"}))
	if got := claimOf(t, db, reo); got != "" {
		t.Fatalf("after reopen claim = %q, want NULL", got)
	}
}

func TestClaimExpiredListFilterAndRetryCount(t *testing.T) {
	db := testDB(t)
	expired := createForUpdate(t, db)
	live := createForUpdate(t, db)
	none := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, expired, "alice", claimT0, time.Minute))
	mustNoErr(t, claimWithClaim(t, db, live, "alice", claimT0, 10*time.Hour))
	mustNoErr(t, claimWithClaim(t, db, none, "alice", claimT0, 0))

	at := claimT0.Add(time.Hour).Format(filterTimeFormat)
	beads, _, err := ListBeads(context.Background(), db, ListFilter{Priority: -1, ClaimExpiredAt: at})
	mustNoErr(t, err)
	if len(beads) != 1 || beads[0].ID != expired {
		t.Fatalf("expired beads = %+v, want only %s", beads, expired)
	}

	mustNoErr(t, claimWithClaim(t, db, expired, "bob", claimT0.Add(time.Hour), 0))
	m, err := DerivedMetrics(context.Background(), db, []string{expired})
	mustNoErr(t, err)
	if m[expired].RetryCount != 1 {
		t.Fatalf("retry_count = %d, want 1 (claim_takeover)", m[expired].RetryCount)
	}
}

func TestClaimClockString(t *testing.T) {
	got, err := ClaimClockString(func(string) string { return "2026-09-28T09:00:00+09:00" })
	mustNoErr(t, err)
	if got != "2026-09-28T00:00:00.000Z" {
		t.Fatalf("clock = %q", got)
	}
	if _, err := ClaimClockString(func(string) string { return "bad" }); err == nil {
		t.Fatal("want an error for an invalid LM_NOW")
	}
}

func TestClaimTakeoverRejectsUnparsableExpiry(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0, time.Minute))
	if _, err := db.ExecContext(context.Background(), `UPDATE beads SET claim_expires_at = 'garbage' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if err := claimWithClaim(t, db, id, "bob", claimT0.Add(time.Hour), 0); err == nil || !strings.Contains(err.Error(), "parse claim_expires_at") {
		t.Fatalf("err = %v, want parse error", err)
	}
}

func TestClaimGateRejectClearsClaim(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{id}, Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "u", Now: "n1"})
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Force: true, Reason: "r", Actor: "alice", Now: "n2", Clock: claimT0, ClaimTTL: time.Hour}))
	if got := claimOf(t, db, id); got == "" {
		t.Fatal("precondition: claim should be set")
	}
	mustGateReject(t, db, GateRejectInput{ID: gateID, Reason: "no", Actor: "u", Now: "n3"})
	if got := claimOf(t, db, id); got != "" {
		t.Fatalf("after gate reject claim = %q, want NULL", got)
	}
}
