// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestClaimReclaimBySelfRunsBlockerCheck(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	blocker := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0, time.Hour))
	mustNoErr(t, mustAddLinkAt(t, db, id, blocker, BlocksDepType, "n3"))

	err := claimWithClaim(t, db, id, "alice", claimT0.Add(time.Minute), time.Hour)
	want := "blocked by " + displayIDOf(t, db, blocker)
	if err == nil || err.Error() != want {
		t.Fatalf("self reclaim err = %v, want %q", err, want)
	}
	b, gerr := GetBead(context.Background(), db, id)
	mustNoErr(t, gerr)
	if b.Status != StatusInProgress || b.ClaimedBy.String != "alice" {
		t.Fatalf("bead = %+v, want in_progress by alice after a rejected reclaim", b)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	mustNoErr(t, err)
	mustNoErr(t, CloseBead(context.Background(), tx, CloseInput{ID: blocker, Actor: "u", Now: "n4"}))
	mustNoErr(t, tx.Commit())
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0.Add(2*time.Minute), time.Hour))
}

func TestClaimExpiryDoesNotAutoRelease(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, claimWithClaim(t, db, id, "alice", claimT0, time.Minute))
	wantExpiry := claimOf(t, db, id)
	if wantExpiry != "2026-09-28T00:01:00.000Z" {
		t.Fatalf("claim = %q, want 2026-09-28T00:01:00.000Z", wantExpiry)
	}

	at := claimT0.Add(time.Hour).Format(filterTimeFormat)
	beads, _, err := ListBeads(context.Background(), db, ListFilter{Priority: -1, ClaimExpiredAt: at})
	mustNoErr(t, err)
	if len(beads) != 1 || beads[0].ID != id {
		t.Fatalf("expired beads = %+v, want only %s", beads, id)
	}
	ready, _, err := ReadyBeads(context.Background(), db, ReadyFilter{Priority: -1, Now: at})
	mustNoErr(t, err)
	for _, r := range ready {
		if r.ID == id {
			t.Fatalf("expired claim %s appeared in ready", id)
		}
	}
	b, err := GetBead(context.Background(), db, id)
	mustNoErr(t, err)
	if b.Status != StatusInProgress || b.ClaimedBy.String != "alice" || b.ClaimExpiresAt.String != wantExpiry {
		t.Fatalf("bead = %+v, want in_progress by alice expiring at %s", b, wantExpiry)
	}

	err = mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Release: true, Actor: "bob", Now: "n", Clock: claimT0.Add(time.Hour)})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("release by non-owner after expiry err = %v, want a --force requirement", err)
	}
}
