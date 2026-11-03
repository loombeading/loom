// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"testing"
	"time"
)

func TestPickupPositionsCountsAheadAndWindowInterrupts(t *testing.T) {
	db := testDB(t)
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	old := mustCreate(t, db, CreateInput{Title: "old", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-10-01T00:00:00Z"})
	recent := mustCreate(t, db, CreateInput{Title: "recent", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-10-16T00:00:00Z"})
	urgent := mustCreate(t, db, CreateInput{Title: "urgent", Priority: 1, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-10-16T12:00:00Z"})

	got, err := PickupPositions(t.Context(), db, now, 72)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]PickupPosition{
		urgent: {Ahead: 0, Interrupts: 0},
		old:    {Ahead: 1, Interrupts: 1},
		recent: {Ahead: 2, Interrupts: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("PickupPositions = %v, want %v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("PickupPositions[%s] = %+v, want %+v", id, got[id], w)
		}
	}

	ready, ahead, interrupts, err := PickupAhead(t.Context(), db, recent, now, 72)
	if err != nil || !ready || ahead != 2 || interrupts != 1 {
		t.Errorf("PickupAhead(recent) = %v %d %d %v, want true 2 1 nil", ready, ahead, interrupts, err)
	}
	ready, _, _, err = PickupAhead(t.Context(), db, "missing", now, 72)
	if err != nil || ready {
		t.Errorf("PickupAhead(missing) = %v %v, want false nil", ready, err)
	}
}

func TestPickupPositionsRejectsMalformedCreatedAt(t *testing.T) {
	db := testDB(t)
	newBead(t, db, "n0")
	if _, err := PickupPositions(t.Context(), db, time.Now(), 72); err == nil {
		t.Fatal("PickupPositions with non-RFC3339 created_at = nil error, want an error")
	}
}
