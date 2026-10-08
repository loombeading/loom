// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOutlookSectionStopAndContinueColumns(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	in := outlookInput{
		closed:    6,
		created:   [outlookStages]int{0, 3, 3, 0, 0},
		remaining: [outlookStages]int{0, 2, 4, 0, 1},
		gates:     [outlookStages]outlookGates{2: {date: 2, lastDate: "2026-10-05", human: 1, judge: 1}},
	}
	want := []string{
		"## Outlook",
		"- Window: 3d closed=6 created=6 → 2.00/day in 2.00/day, 1.00 created per close",
		"- P1: n=2 cum=2 stop=2026-09-29(Tue) 09:00 JST cont=2026-09-30(Wed) 09:00 JST",
		"- P2: n=4 cum=6 stop=2026-10-01(Thu) 09:00 JST cont=発散  gates: date=2(2026-10-05) human=1 judge=1",
		"- P4: n=1 cum=7 stop=2026-10-01(Thu) 21:00 JST cont=発散",
	}
	if got := outlookSection(now, in); !slices.Equal(got, want) {
		t.Errorf("outlookSection =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOutlookSectionNoCloseAndNoRemaining(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	got := outlookSection(now, outlookInput{created: [outlookStages]int{1}, remaining: [outlookStages]int{0, 0, 1}})
	want := []string{
		"## Outlook",
		"- Window: 3d closed=0 created=1 → 0.00/day in 0.33/day, n/a created per close",
		"- P2: n=1 cum=1 stop=見込み無し cont=見込み無し",
	}
	if !slices.Equal(got, want) {
		t.Errorf("outlookSection = %q, want %q", got, want)
	}
	if got := outlookSection(now, outlookInput{closed: 3}); !slices.Equal(got[2:], []string{"(none)"}) {
		t.Errorf("outlookSection with no remaining = %q, want (none) after the Window line", got)
	}
}

func TestAheadOutlookCountsFromHistory(t *testing.T) {
	initBeadsDir(t)
	for i := range 3 {
		id := mustCreateBead(t, "--title", fmt.Sprintf("done-%d", i), "--priority", "1")
		if _, errS, code := runCmd(t, []string{"close", id, "--summary", "s"}, ""); code != 0 {
			t.Fatalf("close done-%d: exit code = %d, stderr = %q", i, code, errS)
		}
	}
	dropped := mustCreateBead(t, "--title", "dropped", "--expedite", "1h", "--reason", "interrupt")
	if _, errS, code := runCmd(t, []string{"update", dropped, "--status", "cancelled", "--reason", "r"}, ""); code != 0 {
		t.Fatalf("cancel: exit code = %d, stderr = %q", code, errS)
	}
	mustCreateBead(t, "--title", "ready-p2")
	wip := mustCreateBead(t, "--title", "wip-p3", "--priority", "3")
	if _, errS, code := runCmd(t, []string{"update", wip, "--claim"}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}
	held := mustCreateBead(t, "--title", "held-p2")
	gate := mustGateCreateCmd(t, []string{held}, "judge it", "--kind", "adjudicate")
	if _, errS, code := runCmd(t, []string{"update", gate, "--add-label", "wait:date:2099-01-02"}, ""); code != 0 {
		t.Fatalf("label gate: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Outlook")
	if len(body) != 3 {
		t.Fatalf("## Outlook body = %q, want the Window line and P2・P3 lines", body)
	}
	if want := "- Window: 3d closed=3 created=6 → 1.00/day in 2.00/day, 2.00 created per close"; body[0] != want {
		t.Errorf("## Outlook body[0] = %q, want %q", body[0], want)
	}
	if !strings.HasPrefix(body[1], "- P2: n=1 cum=1 stop=") || !strings.HasSuffix(body[1], "  gates: date=1(2099-01-02) judge=1") {
		t.Errorf("## Outlook P2 = %q, want n=1 cum=1 and the date/judge gates", body[1])
	}
	if !strings.HasPrefix(body[2], "- P3: n=1 cum=2 stop=") || !strings.Contains(body[2], " cont=発散") {
		t.Errorf("## Outlook P3 = %q, want n=1 cum=2 and a diverging continue column", body[2])
	}
	if strings.Index(out, "## Summary") > strings.Index(out, "## Outlook") || strings.Index(out, "## Outlook") > strings.Index(out, "## Milestones") {
		t.Errorf("ahead output = %q, want ## Summary, ## Outlook, ## Milestones in that order", out)
	}
}
