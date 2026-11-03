// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package estimate

import (
	"testing"
	"time"
)

func TestComputeFlowWindowBoundaries(t *testing.T) {
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(7 * 24 * time.Hour)
	s := []FlowSample{
		{ID: "at-from", CreatedAt: from.Add(-time.Hour), ClosedAt: from},
		{ID: "at-to", CreatedAt: from, ClosedAt: to, Reopened: true},
		{ID: "after", CreatedAt: from, ClosedAt: to.Add(time.Nanosecond)},
	}
	got := ComputeFlow(from, to, s)
	if got.Closed != 1 || got.Reopened != 1 || got.Lead.N != 1 {
		t.Fatalf("got %+v, want only the sample closed exactly at to", got)
	}
}

func TestComputeFlowSingleSampleHasNoPercentile(t *testing.T) {
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	s := []FlowSample{{CreatedAt: from, ClosedAt: from.Add(time.Hour), ClaimedAt: from.Add(time.Minute), ReadyAt: from}}
	got := ComputeFlow(from, to, s)
	if got.Lead.N != 1 || got.Lead.P50 != 0 || got.Lead.P90 != 0 {
		t.Errorf("Lead = %+v, want N=1 with zero percentiles", got.Lead)
	}
	if got.Cycle.N != 1 || got.Ready.N != 1 {
		t.Errorf("Cycle=%+v Ready=%+v, want N=1 each", got.Cycle, got.Ready)
	}
}

func TestComputeFlowExactlyMinSamples(t *testing.T) {
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	var s []FlowSample
	for i := 1; i <= FlowMinSamples; i++ {
		s = append(s, FlowSample{CreatedAt: from, ClosedAt: from.Add(time.Duration(i) * time.Hour)})
	}
	got := ComputeFlow(from, to, s)
	if got.Lead.N != 10 || got.Lead.P50 != 5*time.Hour || got.Lead.P90 != 9*time.Hour {
		t.Errorf("Lead = %+v, want N=10 P50=5h P90=9h", got.Lead)
	}
}
