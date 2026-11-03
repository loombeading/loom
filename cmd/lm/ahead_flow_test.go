// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/loombeading/loom/internal/estimate"
)

func TestAheadFlowSectionBetweenSummaryAndOutlook(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "open")

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	var headings []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "## ") {
			headings = append(headings, line)
		}
	}
	summary := slices.Index(headings, "## Summary")
	if summary < 0 || summary+2 >= len(headings) || headings[summary+1] != "## Flow" || headings[summary+2] != "## Outlook" {
		t.Fatalf("headings = %q, want ## Summary, ## Flow, ## Outlook in a row", headings)
	}
	if !strings.Contains(out, "- Window: 7d closed=0\n- lead: n/a(n=0)") || !strings.Contains(out, "- reopen: n/a(n=0)") {
		t.Errorf("flow section with no closed Bead missing:\n%s", out)
	}
}

func TestFlowSectionSampleBoundary(t *testing.T) {
	f := estimate.Flow{Closed: 10, Lead: estimate.FlowStat{N: 10, P50: 90 * time.Minute, P90: 3 * time.Hour}, Cycle: estimate.FlowStat{N: 9}}
	got := flowSection(f)
	if got[2] != "- lead: p50=1.5h p90=3.0h n=10" || got[3] != "- cycle: n/a(n=9)" || got[5] != "- reopen: 0.00 (0/10)" {
		t.Errorf("flowSection = %q", got)
	}
}
