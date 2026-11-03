// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package estimate

import (
	"testing"
	"time"
)

func TestAcceptReleaseETAPickupLowerBound(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return now.Add(time.Duration(n) * time.Hour) }
	pick := func(early, late int) ReleaseETAResult { return ReleaseETAResult{Early: h(early), Late: h(late)} }

	got := ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 2}})
	if got[0].Early != h(1) || got[0].Late != h(2) {
		t.Fatalf("baseline: early=%v late=%v, want %v %v", got[0].Early, got[0].Late, h(1), h(2))
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 2, Pickups: []ReleaseETAResult{pick(10, 30), pick(48, 20)}}})
	if got[0].EarlyDiverges || got[0].LateDiverges || got[0].Early != h(48) || got[0].Late != h(30) {
		t.Fatalf("max over pickups: %+v, want early=%v late=%v", got[0], h(48), h(30))
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 100, Pickups: []ReleaseETAResult{pick(5, 6)}}})
	if got[0].Early != h(50) || got[0].Late != h(100) {
		t.Fatalf("cumulative later than pickup: %+v, want early=%v late=%v", got[0], h(50), h(100))
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 2, Pickups: []ReleaseETAResult{{Skip: true}}}})
	if got[0].Early != h(1) || got[0].Late != h(2) {
		t.Fatalf("skip pickup counts as now: %+v", got[0])
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 2, Pickups: []ReleaseETAResult{pick(3, 4), {EarlyDiverges: true, Late: h(40)}}}})
	if !got[0].EarlyDiverges || got[0].LateDiverges || got[0].Late != h(40) {
		t.Fatalf("diverging early pickup: %+v, want early diverges, late=%v", got[0], h(40))
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 2, Pickups: []ReleaseETAResult{{EarlyDiverges: true, LateDiverges: true}}}})
	if !got[0].EarlyDiverges || !got[0].LateDiverges {
		t.Fatalf("diverging pickup: %+v, want both diverge", got[0])
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 2, Floor: h(24), Pickups: []ReleaseETAResult{pick(10, 30)}}})
	if got[0].Early != h(24) || got[0].Late != h(30) {
		t.Fatalf("floor after pickup: %+v, want early=%v late=%v", got[0], h(24), h(30))
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{
		{Remaining: 2, Pickups: []ReleaseETAResult{pick(60, 80)}},
		{Remaining: 2},
	})
	if got[0].Early != h(60) || got[0].Late != h(80) {
		t.Fatalf("first milestone: %+v", got[0])
	}
	if got[1].Early != h(2) || got[1].Late != h(4) {
		t.Fatalf("pickup must not propagate: %+v, want early=%v late=%v", got[1], h(2), h(4))
	}
}
