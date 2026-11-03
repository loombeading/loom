// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/estimate"
)

const flowWindow = 7 * 24 * time.Hour

func aheadFlowSection(ctx context.Context, q domain.Querier, now time.Time, namespace string) ([]string, error) {
	recs, err := domain.FlowRecords(ctx, q, now.Add(-flowWindow), now, namespace)
	if err != nil {
		return nil, err
	}
	samples := make([]estimate.FlowSample, len(recs))
	for i, r := range recs {
		samples[i] = estimate.FlowSample{ID: r.ID, CreatedAt: r.CreatedAt, ClosedAt: r.ClosedAt, ClaimedAt: r.ClaimedAt, ReadyAt: r.ReadyAt, Reopened: r.Reopened}
	}
	return flowSection(estimate.ComputeFlow(now.Add(-flowWindow), now, samples)), nil
}

func flowSection(f estimate.Flow) []string {
	reopen := "n/a(n=0)"
	if f.Closed > 0 {
		reopen = fmt.Sprintf("%.2f (%d/%d)", float64(f.Reopened)/float64(f.Closed), f.Reopened, f.Closed)
	}
	return []string{
		"## Flow",
		fmt.Sprintf("- Window: 7d closed=%d", f.Closed),
		"- lead: " + flowStatText(f.Lead),
		"- cycle: " + flowStatText(f.Cycle),
		"- ready: " + flowStatText(f.Ready),
		"- reopen: " + reopen,
	}
}

func flowStatText(s estimate.FlowStat) string {
	if s.N < estimate.FlowMinSamples {
		return fmt.Sprintf("n/a(n=%d)", s.N)
	}
	return fmt.Sprintf("p50=%.1fh p90=%.1fh n=%d", s.P50.Hours(), s.P90.Hours(), s.N)
}
