// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/estimate"
	"github.com/loombeading/loom/internal/storage"
)

func mustAddTokenCostAt(t *testing.T, id string, tokensIn, tokensOut int, recordedAt string) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, beadsDir(t), storage.OpenOptions{Env: func(string) string { return "" }, Actor: "test"})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := domain.AddTokenCost(ctx, tx, domain.AddTokenCostInput{
		BeadID: id, TokensIn: tokensIn, TokensOut: tokensOut, Actor: "test", Now: recordedAt,
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("AddTokenCost: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

const pinnedAheadNowLine = "Now: 2026-09-23 14:05:12 JST"

func pinAheadNow(t *testing.T) {
	t.Helper()
	t.Setenv("LM_NOW", "2026-09-23T05:05:12Z")
	orig := time.Local
	time.Local = time.FixedZone("JST", 9*60*60)
	t.Cleanup(func() { time.Local = orig })
}

func TestAheadNowLine(t *testing.T) {
	initBeadsDir(t)
	pinAheadNow(t)
	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, "# Ahead\n"+pinnedAheadNowLine+"\n") {
		t.Errorf("ahead output = %q, want %q right after # Ahead", out, pinnedAheadNowLine)
	}
}

func aheadFilterLine(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if line == "# Ahead" {
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "Now: ") {
				t.Fatalf("ahead output missing Now: line right after # Ahead:\n%s", out)
			}
			if i+2 < len(lines) {
				return lines[i+2]
			}
			return ""
		}
	}
	t.Fatalf("ahead output missing \"# Ahead\" heading:\n%s", out)
	return ""
}

func TestAheadNamespaceFlagFiltersReadyAndEmitsFilterLine(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "alpha-ready", "--namespace", "x")
	mustCreateBead(t, "--title", "beta-ready", "--namespace", "y")

	out, errS, code := runCmd(t, []string{"ahead", "--namespace", "x"}, "")
	if code != 0 {
		t.Fatalf("ahead --namespace x: exit code = %d, stderr = %q", code, errS)
	}
	if got := aheadFilterLine(t, out); got != `Filter: namespace="x"` {
		t.Errorf("line after # Ahead = %q, want Filter: namespace=\"x\"", got)
	}
	if !strings.Contains(out, "alpha-ready") {
		t.Errorf("ahead --namespace x output = %q, want alpha-ready listed", out)
	}
	if strings.Contains(out, "beta-ready") {
		t.Errorf("ahead --namespace x output = %q, must not list beta-ready (namespace y)", out)
	}
	if !strings.Contains(out, "- Ready: 0 tokens (1 beads)") {
		t.Errorf("ahead --namespace x output = %q, want Ready summary counting only the x-namespace bead", out)
	}
}

func TestAheadNamespaceEnvVarAndFlagPrecedence(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "alpha-ready", "--namespace", "x")
	mustCreateBead(t, "--title", "beta-ready", "--namespace", "y")

	t.Setenv("LM_NAMESPACE", "x")
	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead with LM_NAMESPACE=x: exit code = %d, stderr = %q", code, errS)
	}
	if got := aheadFilterLine(t, out); got != `Filter: namespace="x"` {
		t.Errorf("line after # Ahead = %q, want Filter: namespace=\"x\"", got)
	}
	if !strings.Contains(out, "alpha-ready") || strings.Contains(out, "beta-ready") {
		t.Errorf("ahead with LM_NAMESPACE=x output = %q, want only alpha-ready", out)
	}

	out, errS, code = runCmd(t, []string{"ahead", "--namespace", "y"}, "")
	if code != 0 {
		t.Fatalf("ahead --namespace y with LM_NAMESPACE=x: exit code = %d, stderr = %q", code, errS)
	}
	if got := aheadFilterLine(t, out); got != `Filter: namespace="y"` {
		t.Errorf("line after # Ahead = %q, want Filter: namespace=\"y\" (--namespace must win over LM_NAMESPACE)", got)
	}
	if !strings.Contains(out, "beta-ready") || strings.Contains(out, "alpha-ready") {
		t.Errorf("ahead --namespace y with LM_NAMESPACE=x output = %q, want only beta-ready", out)
	}
}

func TestAheadNamespaceCrossNamespaceBlockKeepsCourseNumber(t *testing.T) {
	initBeadsDir(t)
	blockerY := mustCreateBead(t, "--title", "blocker-in-y", "--namespace", "y")
	mustCreateBead(t, "--title", "blocked-in-x", "--namespace", "x", "--blocked-by", blockerY)

	out, errS, code := runCmd(t, []string{"ahead", "--namespace", "x"}, "")
	if code != 0 {
		t.Fatalf("ahead --namespace x: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "blocker-in-y") {
		t.Errorf("ahead --namespace x output = %q, must not show the y-namespace blocker in any section", out)
	}
	if strings.Contains(out, "## Course 1") {
		t.Errorf("ahead --namespace x output = %q, must not emit ## Course 1 (it becomes empty once the y blocker is filtered out)", out)
	}
	if !strings.Contains(out, "## Course 2") {
		t.Fatalf("ahead --namespace x output = %q, want ## Course 2 to hold blocked-in-x (course numbering preserved, not compacted to Course 1)", out)
	}
	courseIdx := strings.Index(out, "## Course 2")
	summaryIdx := strings.Index(out, "## Summary")
	if courseIdx == -1 || summaryIdx == -1 || !strings.Contains(out[courseIdx:summaryIdx], "blocked-in-x") {
		t.Errorf("ahead --namespace x output = %q, want blocked-in-x listed under ## Course 2", out)
	}
	if !strings.Contains(out, "- Courses: ") || !strings.Contains(out, "(1 beads in 1 courses)") {
		t.Errorf("ahead --namespace x output = %q, want the Courses summary to count 1 bead in 1 (kept) course", out)
	}
	if !strings.Contains(out, "## Ready\n(none)") {
		t.Errorf("ahead --namespace x output = %q, want ## Ready to be empty", out)
	}
}

func TestAheadNamespaceFiltersGateWaiting(t *testing.T) {
	initBeadsDir(t)
	gate := mustCreateBead(t, "--title", "the-gate", "--type", "gate", "--label", "kind:human")
	cX := mustCreateBead(t, "--title", "c-waits-in-x", "--namespace", "x", "--blocked-by", gate)
	mustCreateBead(t, "--title", "d-waits-in-y", "--namespace", "y", "--blocked-by", gate)
	gateAlias := mustDisplayAlias(t, gate)

	out, errS, code := runCmd(t, []string{"ahead", "--namespace", "x"}, "")
	if code != 0 {
		t.Fatalf("ahead --namespace x: exit code = %d, stderr = %q", code, errS)
	}
	if body := aheadSection(t, out, "### Needs you"); len(body) != 1 || !strings.HasPrefix(body[0], "- "+gateAlias+" ") || !strings.HasSuffix(body[0], "  waiting=1 est=n/a(no-data)") {
		t.Errorf("ahead --namespace x ### Needs you = %q, want %s counting only c-waits-in-x (waiting=1)", body, gateAlias)
	}
	if !strings.Contains(out, "- Gate waiting: 0 tokens (1 beads)") {
		t.Errorf("ahead --namespace x output = %q, want Gate waiting summary count of 1", out)
	}
	_ = cX
}

func TestAheadDefaultBudgetIsNamespaceScoped(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "x-task", "--namespace", "x")
	yID := mustCreateBead(t, "--title", "y-task", "--namespace", "y")
	yCanonical := mustCanonicalID(t, yID)

	mustAddTokenCostAt(t, yCanonical, 100, 0, "2026-01-01T00:00:00.000Z")
	mustAddTokenCostAt(t, yCanonical, 200, 0, "2026-01-02T00:00:00.000Z")
	mustAddTokenCostAt(t, yCanonical, 300, 0, "2026-01-03T00:00:00.000Z")

	t.Setenv("LM_NOW", "2026-01-10T00:00:00.000Z")

	outX, errS, code := runCmd(t, []string{"ahead", "--namespace", "x"}, "")
	if code != 0 {
		t.Fatalf("ahead --namespace x: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(outX, "- Budget: n/a (fewer than 3 recorded days; pass --budget)") {
		t.Errorf("ahead --namespace x output = %q, want n/a auto-budget (namespace x has no token_costs of its own)", outX)
	}

	outAll, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(outAll, "- Budget (auto, median of last 3 recorded days):") {
		t.Errorf("ahead (unfiltered) output = %q, want a computed auto-budget from y's 3 recorded days", outAll)
	}
}

func TestAheadWeekLine(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "x-task", "--namespace", "x")
	ids := make([]string, 0, 3)
	for _, title := range []string{"a", "b", "c"} {
		ids = append(ids, mustCanonicalID(t, mustCreateBead(t, "--title", title, "--namespace", "y")))
	}
	mustAddTokenCostAt(t, ids[0], 5000, 0, "2026-01-01T00:00:00.000Z")
	mustAddTokenCostAt(t, ids[0], 100, 0, "2026-01-06T00:00:00.000Z")
	mustAddTokenCostAt(t, ids[1], 150, 50, "2026-01-06T00:00:00.000Z")
	mustAddTokenCostAt(t, ids[2], 300, 0, "2026-01-07T00:00:00.000Z")
	t.Setenv("LM_NOW", "2026-01-10T00:00:00.000Z")
	reset := "2026-01-05T00:00:00Z"

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--week-used", "25", "--week-reset", reset}, "- 週制限の残り ≈ 9 件 (used 25%"},
		{[]string{"--week-used", "40", "--week-reset", reset}, "- 週制限の残り ≈ 4 件 (used 40%"},
		{[]string{"--week-used", "100", "--week-reset", reset}, "- 週制限の残り ≈ 0 件 (used 100%"},
		{[]string{"--week-used", "25"}, "- 週制限の残り: n/a (pass both --week-used and --week-reset)"},
		{[]string{"--week-reset", reset}, "- 週制限の残り: n/a (pass both --week-used and --week-reset)"},
		{[]string{"--week-used", "0", "--week-reset", reset}, "- 週制限の残り: n/a (--week-used must be in (0, 100])"},
		{[]string{"--week-used", "101", "--week-reset", reset}, "- 週制限の残り: n/a (--week-used must be in (0, 100])"},
		{[]string{"--week-used", "25", "--week-reset", reset, "--namespace", "x"}, "- 週制限の残り: n/a (fewer than 3 beads with token_costs since --week-reset)"},
		{[]string{"--week-used", "25", "--week-reset", "2026-01-07T00:00:00Z"}, "- 週制限の残り: n/a (fewer than 3 beads with token_costs since --week-reset)"},
	}
	for _, c := range cases {
		out, errS, code := runCmd(t, append([]string{"ahead"}, c.args...), "")
		if code != 0 {
			t.Fatalf("ahead %v: exit code = %d, stderr = %q", c.args, code, errS)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("ahead %v output = %q, want %q", c.args, out, c.want)
		}
	}

	out, _, _ := runCmd(t, []string{"ahead"}, "")
	if strings.Contains(out, "週制限の残り") {
		t.Errorf("ahead without week flags output = %q, want no weekly-limit line", out)
	}
	if _, errS, code := runCmd(t, []string{"ahead", "--week-used", "25", "--week-reset", "yesterday"}, ""); code != 1 || !strings.Contains(errS, "--week-reset") {
		t.Errorf("ahead with invalid --week-reset: exit code = %d, stderr = %q, want 1 naming --week-reset", code, errS)
	}
}

func TestAheadNoNamespaceFilterIsUnchanged(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "alpha-ready", "--namespace", "x")
	mustCreateBead(t, "--title", "beta-ready", "--namespace", "y")

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Filter:") {
		t.Errorf("ahead (no namespace filter) output = %q, must not emit a Filter: line", out)
	}
	if !strings.Contains(out, "alpha-ready") || !strings.Contains(out, "beta-ready") {
		t.Errorf("ahead (no namespace filter) output = %q, want Beads from every namespace listed", out)
	}
	if !strings.Contains(out, "- Ready: 0 tokens (2 beads)") {
		t.Errorf("ahead (no namespace filter) output = %q, want Ready summary counting both beads", out)
	}
}

func TestAheadInvalidNamespaceFails(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"ahead", "--namespace", "BAD"}, "")
	if code != 1 {
		t.Fatalf("ahead --namespace BAD: exit code = %d, stdout = %q, stderr = %q, want 1", code, out, errS)
	}
	if !strings.HasPrefix(errS, "Error:") {
		t.Errorf("ahead --namespace BAD: stderr = %q, want it to start with \"Error:\"", errS)
	}
}

func aheadSection(t *testing.T, out, heading string) []string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if line != heading {
			continue
		}
		var body []string
		for _, l := range lines[i+1:] {
			if l == "" {
				break
			}
			body = append(body, l)
		}
		return body
	}
	t.Fatalf("ahead output missing %q heading:\n%s", heading, out)
	return nil
}

func TestAheadReleasesCountsDescendants(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "q4-release", "--type", "task", "--label", "milestone:2026-Q4")
	gate := mustCreateBead(t, "--title", "q4-gate", "--type", "gate", "--parent", epic, "--label", "kind:human")
	c1 := mustCreateBead(t, "--title", "c1-ready", "--parent", epic)
	c2 := mustCreateBead(t, "--title", "c2-wip", "--parent", epic)
	c3 := mustCreateBead(t, "--title", "c3-closed", "--parent", epic)
	mustCreateBead(t, "--title", "c4-gated", "--parent", epic, "--blocked-by", gate)
	c5 := mustCreateBead(t, "--title", "c5-course", "--parent", epic, "--blocked-by", c1)
	c6 := mustCreateBead(t, "--title", "c6-cancelled", "--parent", epic)
	mustCreateBead(t, "--title", "gc-ready", "--parent", c5)

	if _, errS, code := runCmd(t, []string{"update", "--claim", c2}, ""); code != 0 {
		t.Fatalf("update c2: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", c3, "--summary", "s"}, ""); code != 0 {
		t.Fatalf("close c3: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"update", "--status", "cancelled", c6}, ""); code != 0 {
		t.Fatalf("cancel c6: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Milestones")
	if len(body) != 2 {
		t.Fatalf("## Milestones body = %q, want the Window line and exactly the q4-release line", body)
	}
	if !strings.HasPrefix(body[0], "- Window: 72h closed=") {
		t.Errorf("## Milestones body[0] = %q, want the Window line", body[0])
	}
	if !strings.Contains(body[1], "q4-release") {
		t.Errorf("## Milestones line = %q, want the q4-release Epic", body[1])
	}
	if want := "  milestone=2026-Q4  ready=2 wip=1 course=1 gate=1 remaining=5  close/日=0.0〜0.3  残り日数=15.0日〜見込み無し  eta=発散  Gate 待ち"; !strings.HasSuffix(body[1], want) {
		t.Errorf("## Milestones line = %q, want suffix %q", body[1], want)
	}
	if !strings.Contains(out, "- Gate waiting: 0 tokens (2 beads)\n- Milestones: 1") {
		t.Errorf("ahead output = %q, want \"- Milestones: 1\" right after the Gate waiting summary line", out)
	}
}

func TestAheadMilestonesMarksAbolishedEpic(t *testing.T) {
	initBeadsDir(t)
	old := mustCreateBead(t, "--title", "old-release", "--type", "task", "--label", "milestone:2026-Q3")
	mustCreateBead(t, "--title", "old-c1", "--parent", old)
	cur := mustCreateBead(t, "--title", "cur-release", "--type", "task", "--label", "milestone:2026-Q4")
	mustCreateBead(t, "--title", "cur-c1", "--parent", cur)
	if _, errS, code := runCmd(t, []string{"update", old, "--add-label", "abolished-by:lm-any"}, ""); code != 0 {
		t.Fatalf("update add-label: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Milestones")
	var oldLine, curLine string
	for _, l := range body {
		switch {
		case strings.Contains(l, "old-release"):
			oldLine = l
		case strings.Contains(l, "cur-release"):
			curLine = l
		}
	}
	if !strings.Contains(oldLine, "廃止・棚卸し中") || strings.Contains(oldLine, "eta=") || strings.Contains(oldLine, "残り日数=") {
		t.Errorf("abolished Epic line = %q, want 廃止・棚卸し中 with no ETA", oldLine)
	}
	if !strings.Contains(oldLine, "remaining=") {
		t.Errorf("abolished Epic line = %q, want the count columns kept", oldLine)
	}
	if strings.Contains(curLine, "廃止・棚卸し中") || !strings.Contains(curLine, "eta=") {
		t.Errorf("live Epic line = %q, want the usual ETA and no 廃止 mark", curLine)
	}
}

func TestAheadReleasesETANonDiverging(t *testing.T) {
	initBeadsDir(t)
	orig := time.Local
	time.Local = time.FixedZone("JST", 9*60*60)
	t.Cleanup(func() { time.Local = orig })

	for i := range 20 {
		id := mustCreateBead(t, "--title", fmt.Sprintf("filler-%d", i))
		if i < 15 {
			if _, errS, code := runCmd(t, []string{"update", id, "--external-ref", fmt.Sprintf("https://github.com/o/r/pull/%d", i)}, ""); code != 0 {
				t.Fatalf("update filler %d external-ref: exit code = %d, stderr = %q", i, code, errS)
			}
		}
		if _, errS, code := runCmd(t, []string{"close", id, "--summary", "s"}, ""); code != 0 {
			t.Fatalf("close filler %d: exit code = %d, stderr = %q", i, code, errS)
		}
	}

	epic := mustCreateBead(t, "--title", "q4-release", "--type", "task", "--label", "milestone:2026-Q4")
	mustCreateBead(t, "--title", "c1", "--parent", epic)
	mustCreateBead(t, "--title", "c2", "--parent", epic)

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Milestones")
	if len(body) != 2 {
		t.Fatalf("## Milestones body = %q, want the Window line and exactly the q4-release line", body)
	}
	if !strings.HasPrefix(body[0], "- Window: 72h closed=20(PR 15) ") {
		t.Errorf("## Milestones body[0] = %q, want closed=20(PR 15)", body[0])
	}
	if !strings.Contains(body[1], "  close/日=") || !strings.Contains(body[1], "残り日数=") {
		t.Errorf("## Milestones line = %q, want close/日 and 残り日数", body[1])
	}
	etaPattern := regexp.MustCompile(`  eta=\d{4}-\d{2}-\d{2} \d{2}:\d{2} JST〜\d{4}-\d{2}-\d{2} \d{2}:\d{2} JST$`)
	if !etaPattern.MatchString(body[1]) {
		t.Errorf("## Milestones line = %q, want a non-diverging eta= range (closed=20(PR 15) should outrun 2 remaining/created)", body[1])
	}
}

func TestAheadReleasesGateCountsHumanOnlyUnlessAll(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "q4-release", "--type", "task", "--label", "milestone:2026-Q4")
	human := mustCreateBead(t, "--title", "human-gated", "--parent", epic)
	externalPrefix := mustCreateBead(t, "--title", "external-gated-2", "--parent", epic)
	external := mustCreateBead(t, "--title", "external-gated", "--parent", epic)
	both := mustCreateBead(t, "--title", "human-and-external-gated", "--parent", epic)

	if _, errS, code := runCmd(t, withAdjudication(t, []string{"gate", "create", "--blocks", human, "--subject", "human sign-off", "--kind", "human"}), ""); code != 0 {
		t.Fatalf("gate create (human): exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"gate", "create", "--blocks", externalPrefix, "--subject", "notify releng", "--kind", "external", "--resolver", "watcher"}, ""); code != 0 {
		t.Fatalf("gate create (external-2): exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"gate", "create", "--blocks", external, "--subject", "vendor response", "--kind", "external", "--resolver", "watcher"}, ""); code != 0 {
		t.Fatalf("gate create (external): exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, withAdjudication(t, []string{"gate", "create", "--blocks", both, "--subject", "human sign-off", "--kind", "human"}), ""); code != 0 {
		t.Fatalf("gate create (both/human): exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"gate", "create", "--blocks", both, "--subject", "CI green", "--kind", "external", "--resolver", "watcher"}, ""); code != 0 {
		t.Fatalf("gate create (both/external): exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Milestones")
	if len(body) != 2 {
		t.Fatalf("## Milestones body = %q, want the Window line and exactly the q4-release line", body)
	}
	if want := "ready=0 wip=0 course=0 gate=2 remaining=4  close/日=0.0〜0.0  残り日数=見込み無し  eta=発散  Gate 待ち"; !strings.HasSuffix(body[1], want) {
		t.Errorf("## Milestones line = %q, want suffix %q", body[1], want)
	}

	outAll, errS, code := runCmd(t, []string{"ahead", "--all"}, "")
	if code != 0 {
		t.Fatalf("ahead --all: exit code = %d, stderr = %q", code, errS)
	}
	bodyAll := aheadSection(t, outAll, "## Milestones")
	if len(bodyAll) != 2 {
		t.Fatalf("## Milestones (--all) body = %q, want the Window line and exactly the q4-release line", bodyAll)
	}
	if want := "ready=0 wip=0 course=0 gate=4 remaining=4  close/日=0.0〜0.0  残り日数=見込み無し  eta=発散  Gate 待ち"; !strings.HasSuffix(bodyAll[1], want) {
		t.Errorf("## Milestones (--all) line = %q, want suffix %q", bodyAll[1], want)
	}
}

func TestAheadReleasesETAFloorIgnoresGateKindAndAllFlag(t *testing.T) {
	initBeadsDir(t)
	orig := time.Local
	time.Local = time.FixedZone("JST", 9*60*60)
	t.Cleanup(func() { time.Local = orig })

	for i := range 20 {
		id := mustCreateBead(t, "--title", fmt.Sprintf("filler-%d", i))
		if i < 15 {
			if _, errS, code := runCmd(t, []string{"update", id, "--external-ref", fmt.Sprintf("https://github.com/o/r/pull/%d", i)}, ""); code != 0 {
				t.Fatalf("update filler %d external-ref: exit code = %d, stderr = %q", i, code, errS)
			}
		}
		if _, errS, code := runCmd(t, []string{"close", id, "--summary", "s"}, ""); code != 0 {
			t.Fatalf("close filler %d: exit code = %d, stderr = %q", i, code, errS)
		}
	}

	epic := mustCreateBead(t, "--title", "q4-release", "--type", "task", "--label", "milestone:2026-Q4")
	held := mustCreateBead(t, "--title", "held", "--parent", epic)
	gate := mustGateCreateCmd(t, []string{held}, "waits on a date", "--kind", "adjudicate")
	if _, errS, code := runCmd(t, []string{"update", gate, "--add-label", "wait:date:2099-01-01"}, ""); code != 0 {
		t.Fatalf("update gate --add-label wait:date: exit code = %d, stderr = %q", code, errS)
	}

	wantFloor := time.Date(2099, 1, 1, 0, 0, 0, 0, time.Local)
	want := "  eta=" + wantFloor.Format("2006-01-02 15:04 JST") + "〜" + wantFloor.Format("2006-01-02 15:04 JST") + "  judge=1"
	for _, c := range []struct {
		args     []string
		wantGate string
	}{
		{[]string{"ahead"}, "gate=0 remaining=1"},
		{[]string{"ahead", "--all"}, "gate=1 remaining=1"},
	} {
		out, errS, code := runCmd(t, c.args, "")
		if code != 0 {
			t.Fatalf("%v: exit code = %d, stderr = %q", c.args, code, errS)
		}
		body := aheadSection(t, out, "## Milestones")
		if len(body) != 2 {
			t.Fatalf("%v: ## Milestones body = %q, want the Window line and exactly the q4-release line", c.args, body)
		}
		if !strings.Contains(body[1], c.wantGate) {
			t.Errorf("%v: ## Milestones line = %q, want %q", c.args, body[1], c.wantGate)
		}
		if !strings.HasSuffix(body[1], want) {
			t.Errorf("%v: ## Milestones line = %q, want suffix %q (both ends raised to the wait:date: floor)", c.args, body[1], want)
		}
	}
}

func TestAheadMilestoneHeldByOwnGate(t *testing.T) {
	initBeadsDir(t)
	orig := time.Local
	time.Local = time.FixedZone("JST", 9*60*60)
	t.Cleanup(func() { time.Local = orig })

	for i := range 20 {
		id := mustCreateBead(t, "--title", fmt.Sprintf("filler-%d", i))
		if _, errS, code := runCmd(t, []string{"update", id, "--external-ref", fmt.Sprintf("https://github.com/o/r/pull/%d", i)}, ""); code != 0 {
			t.Fatalf("update filler %d external-ref: exit code = %d, stderr = %q", i, code, errS)
		}
		if _, errS, code := runCmd(t, []string{"close", id, "--summary", "s"}, ""); code != 0 {
			t.Fatalf("close filler %d: exit code = %d, stderr = %q", i, code, errS)
		}
	}

	afterMS := mustCreateBead(t, "--title", "after-ms", "--type", "task", "--label", "milestone:after")
	mustCreateBead(t, "--title", "after-child", "--parent", afterMS)
	afterGate := mustGateCreateCmd(t, []string{afterMS}, "measure window", "--kind", "external", "--resolver", "watcher")
	if _, errS, code := runCmd(t, []string{"update", afterGate, "--add-label", "wait:after:2099-03-04T05:06:00Z"}, ""); code != 0 {
		t.Fatalf("update gate --add-label wait:after: exit code = %d, stderr = %q", code, errS)
	}

	judgeMS := mustCreateBead(t, "--title", "judge-ms", "--type", "task", "--label", "milestone:judge")
	mustCreateBead(t, "--title", "judge-child", "--parent", judgeMS)
	mustGateCreateCmd(t, []string{judgeMS}, "done condition is a bag", "--kind", "adjudicate")

	humanMS := mustCreateBead(t, "--title", "human-ms", "--type", "task", "--label", "milestone:human")
	mustCreateBead(t, "--title", "human-child", "--parent", humanMS)
	if _, errS, code := runCmd(t, withAdjudication(t, []string{"gate", "create", "--blocks", humanMS, "--subject", "sign-off", "--kind", "human"}), ""); code != 0 {
		t.Fatalf("gate create (human): exit code = %d, stderr = %q", code, errS)
	}

	lineOf := func(body []string, title string) string {
		for _, l := range body {
			if strings.Contains(l, title) {
				return l
			}
		}
		t.Fatalf("## Milestones = %q, want a line for %s", body, title)
		return ""
	}

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Milestones")

	wantFloor := time.Date(2099, 3, 4, 5, 6, 0, 0, time.UTC).In(time.Local).Format("2006-01-02 15:04 JST")
	after := lineOf(body, "after-ms")
	if !strings.Contains(after, "gate=0 remaining=1") {
		t.Errorf("after-ms line = %q, want an external Gate on the milestone itself left out of gate= by default", after)
	}
	if !strings.HasSuffix(after, "  eta="+wantFloor+"〜"+wantFloor) {
		t.Errorf("after-ms line = %q, want both ends raised to the wait:after: time %s of the Gate on the milestone itself", after, wantFloor)
	}

	judge := lineOf(body, "judge-ms")
	if !strings.Contains(judge, "gate=0 remaining=1") || !strings.HasSuffix(judge, "  judge=1") {
		t.Errorf("judge-ms line = %q, want gate=0 and judge=1 for an adjudicate Gate on the milestone itself", judge)
	}

	human := lineOf(body, "human-ms")
	if !strings.Contains(human, "gate=1 remaining=1") || !strings.HasSuffix(human, "  Gate 待ち") || strings.Contains(human, "judge=") {
		t.Errorf("human-ms line = %q, want gate=1, Gate 待ち and no judge= for a human Gate on the milestone itself", human)
	}

	outAll, errS, code := runCmd(t, []string{"ahead", "--all"}, "")
	if code != 0 {
		t.Fatalf("ahead --all: exit code = %d, stderr = %q", code, errS)
	}
	bodyAll := aheadSection(t, outAll, "## Milestones")
	for _, title := range []string{"after-ms", "judge-ms", "human-ms"} {
		if l := lineOf(bodyAll, title); !strings.Contains(l, "gate=1 remaining=1") {
			t.Errorf("--all %s line = %q, want gate=1 for the Gate on the milestone itself", title, l)
		}
	}
}

func TestAheadGateWaitingFoldsFourKindsAndListsNeedsYouPerGate(t *testing.T) {
	initBeadsDir(t)
	rel := mustCreateBead(t, "--title", "release", "--label", "milestone:r1")
	mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "ext-waiter")}, "ext-gate", "--kind", "external", "--resolver", "watcher")
	mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "adj-waiter", "--parent", rel)}, "adj-gate", "--kind", "adjudicate")
	unknownGate := mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "unk-waiter")}, "unk-gate", "--kind", "human")
	if _, errS, code := runCmd(t, []string{"update", unknownGate, "--remove-label", "kind:human"}, ""); code != 0 {
		t.Fatalf("update --remove-label kind:human: exit code = %d, stderr = %q", code, errS)
	}
	mixed := mustCreateBead(t, "--title", "mixed-waiter", "--parent", rel)
	mustGateCreateCmd(t, []string{mixed}, "mixed-ext-gate", "--kind", "external", "--resolver", "watcher")
	mustGateCreateCmd(t, []string{mixed}, "prereq-gate", "--kind", "human")
	humanGate := mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "human-waiter")}, "human-gate", "--kind", "human")
	humanGateAlias := mustDisplayAlias(t, humanGate)

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	_, gw, ok := strings.Cut(out, "## Gate waiting\n")
	if !ok {
		t.Fatalf("ahead output missing ## Gate waiting:\n%s", out)
	}
	folded := []string{
		"- Opens automatically: 2 gates, waiting=3 est=n/a(no-data)",
		"- Waiting on judge: 1 gates, waiting=2 est=n/a(no-data)",
		"- Unknown kind (fix labels): 1 gates, waiting=1 est=n/a(no-data)",
		"- Blocked by other work: 1 gates, waiting=2 est=n/a(no-data)",
	}
	prev := -1
	for _, want := range folded {
		idx := strings.Index(gw, want)
		if idx == -1 {
			t.Fatalf("## Gate waiting = %q, want folded line %q", gw, want)
		}
		if idx <= prev {
			t.Fatalf("## Gate waiting: %q at %d, want after %d (Opens automatically -> Waiting on judge -> Unknown kind -> Blocked by other work):\n%s", want, idx, prev, gw)
		}
		prev = idx
	}
	needsYouIdx := strings.Index(gw, "### Needs you")
	if needsYouIdx <= prev {
		t.Fatalf("## Gate waiting: ### Needs you at %d, want after the folded lines (%d):\n%s", needsYouIdx, prev, gw)
	}
	body := aheadSection(t, gw, "### Needs you")
	if len(body) != 1 || !strings.HasPrefix(body[0], "- "+humanGateAlias+" ") || !strings.HasSuffix(body[0], "  waiting=1 est=n/a(no-data)") {
		t.Errorf("### Needs you = %q, want only human-gate (waiting=1)", body)
	}
	for _, subject := range []string{"ext-gate", "adj-gate", "unk-gate", "prereq-gate", "mixed-ext-gate"} {
		if strings.Contains(gw, "] "+subject+"  ") {
			t.Errorf("## Gate waiting = %q, must not print a Gate row for the folded %s", gw, subject)
		}
	}
	if strings.Contains(gw, "] human-waiter") || strings.Contains(gw, "] mixed-waiter") {
		t.Errorf("## Gate waiting = %q, must not print a row per waiting Bead", gw)
	}
	if !strings.Contains(out, "gate=1 remaining=2") {
		t.Errorf("ahead output = %q, want the release to count mixed-waiter (human Gate) but not adj-waiter as gate", out)
	}
}

func TestAheadGateWaitingCountsWaitersInNamespace(t *testing.T) {
	initBeadsDir(t)
	for i := range 3 {
		mustCloseWithActual(t, fmt.Sprintf("closed-p2-%d", i), 2, 1000)
	}
	direct := mustCreateBead(t, "--title", "direct", "--namespace", "x")
	mustCreateBead(t, "--title", "through", "--namespace", "x", "--blocked-by", direct)
	other := mustCreateBead(t, "--title", "other", "--namespace", "y")
	gate := mustCanonicalID(t, mustGateCreateCmd(t, []string{direct, other}, "shared", "--kind", "human"))
	mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "y-only", "--namespace", "y")}, "y-gate", "--kind", "human")
	gate = mustDisplayAlias(t, gate)

	out, errS, code := runCmd(t, []string{"ahead", "--namespace", "x"}, "")
	if code != 0 {
		t.Fatalf("ahead --namespace x: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "### Needs you")
	if len(body) != 1 || !strings.HasPrefix(body[0], "- "+gate+" ") || !strings.HasSuffix(body[0], "  waiting=2 est=2.00k") {
		t.Errorf("### Needs you = %q, want only %s with waiting=2 est=2.00k", body, gate)
	}
	if !strings.Contains(out, "- Gate waiting: 2.00k tokens (2 beads)") {
		t.Errorf("ahead output = %q, want \"- Gate waiting: 2.00k tokens (2 beads)\"", out)
	}
}

func TestAheadSectionOrder(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a-ready")
	b := mustCreateBead(t, "--title", "b-course1", "--blocked-by", a)
	mustCreateBead(t, "--title", "c-course2", "--blocked-by", b)
	wip := mustCreateBead(t, "--title", "wip")
	if _, errS, code := runCmd(t, []string{"update", "--claim", wip}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}
	mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "waiter")}, "sign-off", "--kind", "human")

	pinAheadNow(t)
	out, errS, code := runCmd(t, []string{"ahead", "--calibration"}, "")
	if code != 0 {
		t.Fatalf("ahead --calibration: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, "# Ahead\n"+pinnedAheadNowLine+"\n\n## Calibration\n") {
		t.Errorf("ahead --calibration output = %q, want ## Calibration right after # Ahead", out)
	}
	order := []string{"## Calibration", "## Depth calibration", "## Course 2", "## Course 1", "## Ready", "## In progress", "## Summary", "## Milestones", "## Gate waiting"}
	prev := -1
	for _, h := range order {
		idx := strings.Index(out, "\n"+h+"\n")
		if idx <= prev {
			t.Fatalf("ahead --calibration: %q at %d, want after %d:\n%s", h, idx, prev, out)
		}
		prev = idx
	}
	if strings.Contains(out, "\n\n\n") || strings.HasSuffix(out, "\n\n") {
		t.Errorf("ahead output = %q, want exactly one blank line between sections and none trailing", out)
	}
}

func TestAheadBatchFoldsWhenSameAsReady(t *testing.T) {
	initBeadsDir(t)
	for i := range 3 {
		mustCloseWithActual(t, fmt.Sprintf("closed-p2-%d", i), 2, 1000)
	}
	mustCreateBead(t, "--title", "b-ready", "--priority", "2")
	mustCreateBead(t, "--title", "c-ready", "--priority", "2")

	out, errS, code := runCmd(t, []string{"ahead", "--budget", "4000"}, "")
	if code != 0 {
		t.Fatalf("ahead --budget 4000: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "## Batch") {
		t.Errorf("ahead output = %q, must fold ## Batch when it equals ## Ready", out)
	}
	if !strings.Contains(out, "- Batch: 2.00k tokens (2 beads, estimated, same as Ready)") {
		t.Errorf("ahead output = %q, want \"- Batch: 2.00k tokens (2 beads, estimated, same as Ready)\"", out)
	}

	out, errS, code = runCmd(t, []string{"ahead", "--budget", "1500"}, "")
	if code != 0 {
		t.Fatalf("ahead --budget 1500: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Batch: 1.00k tokens (1 beads, estimated)") {
		t.Errorf("ahead output = %q, want \"- Batch: 1.00k tokens (1 beads, estimated)\"", out)
	}
	if bi, ri := strings.Index(out, "\n## Batch\n"), strings.Index(out, "\n## Ready\n"); bi == -1 || bi > ri {
		t.Errorf("ahead output = %q, want ## Batch above ## Ready", out)
	}
}

func TestAheadGateWaitingOmitsEmptyFoldedLinesAndFallsBackToNone(t *testing.T) {
	initBeadsDir(t)
	markers := []string{"### Needs you", "- Blocked by other work:", "- Waiting on judge:", "- Opens automatically:", "- Unknown kind (fix labels):"}

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "## Gate waiting\n(none)") {
		t.Errorf("ahead output = %q, want ## Gate waiting followed by (none)", out)
	}
	for _, marker := range markers {
		if strings.Contains(out, marker) {
			t.Errorf("ahead output = %q, must not print %q when Gate waiting is empty", out, marker)
		}
	}

	mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "human-waiter")}, "human sign-off", "--kind", "human")

	out, errS, code = runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "### Needs you") {
		t.Errorf("ahead output = %q, want the ### Needs you sub-heading", out)
	}
	for _, marker := range markers[1:] {
		if strings.Contains(out, marker) {
			t.Errorf("ahead output = %q, must not print the empty %q folded line", out, marker)
		}
	}
}

func TestAheadGateWaitingStalledLine(t *testing.T) {
	initBeadsDir(t)
	t.Setenv("LM_NOW", "2026-09-23T00:00:00Z")
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })

	passed := mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "passed-waiter")}, "passed-gate", "--kind", "external", "--resolver", "watcher")
	if _, errS, code := runCmd(t, []string{"update", passed, "--add-label", "wait:date:2026-09-22"}, ""); code != 0 {
		t.Fatalf("update --add-label wait:date (passed): exit code = %d, stderr = %q", code, errS)
	}
	passedAlias := mustDisplayAlias(t, passed)

	future := mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "future-waiter")}, "future-gate", "--kind", "external", "--resolver", "watcher")
	if _, errS, code := runCmd(t, []string{"update", future, "--add-label", "wait:date:2026-09-24"}, ""); code != 0 {
		t.Fatalf("update --add-label wait:date (future): exit code = %d, stderr = %q", code, errS)
	}

	mixed := mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "mixed-waiter")}, "mixed-gate", "--kind", "external", "--resolver", "watcher")
	if _, errS, code := runCmd(t, []string{"update", mixed, "--add-label", "wait:date:2026-09-22"}, ""); code != 0 {
		t.Fatalf("update --add-label wait:date (mixed): exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"update", mixed, "--add-label", "wait:file:/tmp/x"}, ""); code != 0 {
		t.Fatalf("update --add-label wait:file (mixed): exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	want := fmt.Sprintf("- Stalled: %s (wait:date passed but still open)", passedAlias)
	if !strings.Contains(out, want) {
		t.Errorf("ahead output = %q, want %q", out, want)
	}
	if strings.Count(out, "- Stalled:") != 1 {
		t.Errorf("ahead output = %q, want exactly one Stalled line (future-gate not yet passed, mixed-gate mixes a non-date probe)", out)
	}
}

func TestAheadGateListErrorFails(t *testing.T) {
	initBeadsDir(t)
	dropBeadsColumn(t, "labels")

	_, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 1 {
		t.Fatalf("ahead: exit code = %d, stderr = %q, want 1", code, errS)
	}
	if !strings.HasPrefix(errS, "Error:") {
		t.Errorf("ahead stderr = %q, want it to start with \"Error:\"", errS)
	}
}

func TestAheadReleasesNoneAndNonTargets(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "bare-label", "--label", "release")
	closed := mustCreateBead(t, "--title", "closed-release", "--label", "milestone:old")
	mustCreateBead(t, "--title", "gate-release", "--type", "gate", "--label", "milestone:gate", "--label", "kind:human")
	if _, errS, code := runCmd(t, []string{"close", closed, "--summary", "s"}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	if body := aheadSection(t, out, "## Milestones"); len(body) != 1 || body[0] != "(none)" {
		t.Errorf("## Milestones body = %q, want [(none)]", body)
	}
	if !strings.Contains(out, "- Milestones: 0") {
		t.Errorf("ahead output = %q, want \"- Milestones: 0\"", out)
	}
	if !strings.Contains(out, "\n\n## Milestones\n(none)\n\n## Gate waiting\n") {
		t.Errorf("ahead output = %q, want ## Milestones right before ## Gate waiting", out)
	}
}

func TestAheadReleasesOrderNamesAndNamespace(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "low-release", "-p", "3", "--namespace", "x", "--label", "milestone:b", "--label", "milestone:a")
	mustCreateBead(t, "--title", "high-release", "-p", "1", "--namespace", "x", "--label", "milestone:c")
	mustCreateBead(t, "--title", "other-ns-release", "--namespace", "y", "--label", "milestone:y")

	pinAheadNow(t)
	out, errS, code := runCmd(t, []string{"ahead", "--namespace", "x"}, "")
	if code != 0 {
		t.Fatalf("ahead --namespace x: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, "# Ahead\n"+pinnedAheadNowLine+"\nFilter: namespace=\"x\"\n\n") {
		t.Errorf("ahead output = %q, want the filter line right after # Ahead", out)
	}
	body := aheadSection(t, out, "## Milestones")
	if len(body) != 3 {
		t.Fatalf("## Milestones body = %q, want the Window line and the two namespace-x releases only", body)
	}
	if !strings.HasPrefix(body[0], "- Window: 72h closed=") {
		t.Errorf("## Milestones body[0] = %q, want the Window line", body[0])
	}
	if !strings.Contains(body[1], "high-release") || !strings.HasSuffix(body[1], "  milestone=c  ready=0 wip=0 course=0 gate=0 remaining=0") {
		t.Errorf("## Milestones first line = %q, want high-release with no descendants", body[1])
	}
	if !strings.Contains(body[2], "low-release") || !strings.Contains(body[2], "  milestone=a,b  ") {
		t.Errorf("## Milestones second line = %q, want low-release with milestone=a,b", body[2])
	}
	if strings.Index(out, "## Milestones") < strings.Index(out, "## Ready") {
		t.Errorf("ahead output = %q, want ## Milestones below ## Ready", out)
	}
	if !strings.Contains(out, "- Milestones: 2") {
		t.Errorf("ahead output = %q, want \"- Milestones: 2\"", out)
	}
}

func TestAheadReleasesSortsBySamePriorityRemainingAscending(t *testing.T) {
	initBeadsDir(t)
	many := mustCreateBead(t, "--title", "many-release", "--type", "task", "--label", "milestone:many")
	few := mustCreateBead(t, "--title", "few-release", "--type", "task", "--label", "milestone:few")
	mustCreateBead(t, "--title", "many-c1", "--parent", many)
	mustCreateBead(t, "--title", "many-c2", "--parent", many)
	mustCreateBead(t, "--title", "few-c1", "--parent", few)

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Milestones")
	if len(body) != 3 {
		t.Fatalf("## Milestones body = %q, want the Window line and both releases", body)
	}
	if !strings.HasPrefix(body[0], "- Window: 72h closed=") {
		t.Errorf("## Milestones body[0] = %q, want the Window line", body[0])
	}
	if !strings.Contains(body[1], "few-release") || !strings.Contains(body[1], "remaining=1") {
		t.Errorf("## Milestones first line = %q, want few-release first (same priority, fewer remaining)", body[1])
	}
	if !strings.Contains(body[2], "many-release") || !strings.Contains(body[2], "remaining=2") {
		t.Errorf("## Milestones second line = %q, want many-release second", body[2])
	}
}

func TestAheadReadyMarksFrontReleaseDescendantsOnly(t *testing.T) {
	initBeadsDir(t)
	front := mustCreateBead(t, "--title", "front-release", "--type", "task", "-p", "1", "--label", "milestone:front")
	back := mustCreateBead(t, "--title", "back-release", "--type", "task", "-p", "2", "--label", "milestone:back")
	mustCreateBead(t, "--title", "front-ready", "--parent", front)
	mustCreateBead(t, "--title", "back-ready", "--parent", back)

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Ready")
	if len(body) != 2 {
		t.Fatalf("## Ready body = %q, want both descendants", body)
	}
	var frontLine, backLine string
	for _, l := range body {
		if strings.Contains(l, "front-ready") {
			frontLine = l
		}
		if strings.Contains(l, "back-ready") {
			backLine = l
		}
	}
	if !strings.HasSuffix(frontLine, "  milestone=front") {
		t.Errorf("front-ready line = %q, want it marked milestone=front (belongs to the frontmost release)", frontLine)
	}
	if strings.Contains(backLine, "milestone=") {
		t.Errorf("back-ready line = %q, must not be marked (its release isn't frontmost)", backLine)
	}
}

func TestAheadMilestoneCountsBlocksPrerequisitesTransitively(t *testing.T) {
	initBeadsDir(t)
	ms := mustCreateBead(t, "--title", "reached-milestone", "--type", "task", "--label", "milestone:m")
	done := mustCreateBead(t, "--title", "done-child", "--parent", ms)
	prereq := mustCreateBead(t, "--title", "prereq")
	waiter := mustCreateBead(t, "--title", "prereq-child", "--parent", prereq)
	upstream := mustCreateBead(t, "--title", "upstream")
	finished := mustCreateBead(t, "--title", "finished-prereq")
	for _, link := range [][2]string{{ms, prereq}, {ms, finished}, {waiter, upstream}} {
		if _, errS, code := runCmd(t, []string{"dep", "add", link[0], link[1], "--type", "blocks", "--allow-priority-change"}, ""); code != 0 {
			t.Fatalf("dep add %s %s: exit code = %d, stderr = %q", link[0], link[1], code, errS)
		}
	}
	for _, id := range []string{done, finished} {
		if _, errS, code := runCmd(t, []string{"close", id, "--reason", "done"}, ""); code != 0 {
			t.Fatalf("close %s: exit code = %d, stderr = %q", id, code, errS)
		}
	}
	mustGateCreateCmd(t, []string{upstream}, "push-by-hand", "--kind", "human")

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Milestones")
	if len(body) != 2 || !strings.Contains(body[1], "reached-milestone") {
		t.Fatalf("## Milestones = %q, want the window line and reached-milestone", body)
	}
	if !strings.Contains(body[1], "  milestone=m  ready=0 wip=0 course=0 gate=4 remaining=3  ") {
		t.Errorf("milestone line = %q, want prereq, prereq-child, upstream and the milestone itself counted in gate (gate=4 remaining=3)", body[1])
	}
	if !strings.HasSuffix(body[1], "  Gate 待ち") {
		t.Errorf("milestone line = %q, want the human Gate behind the prerequisites to mark Gate 待ち", body[1])
	}
}

func TestAheadInProgressSection(t *testing.T) {
	initBeadsDir(t)
	wip := mustCreateBead(t, "--title", "wip-x", "--namespace", "x")
	mustCreateBead(t, "--title", "blocked-by-wip", "--namespace", "x", "--blocked-by", wip)
	wipY := mustCreateBead(t, "--title", "wip-y", "--namespace", "y")
	for _, id := range []string{wip, wipY} {
		if _, errS, code := runCmd(t, []string{"update", "--claim", id}, ""); code != 0 {
			t.Fatalf("claim %s: exit code = %d, stderr = %q", id, code, errS)
		}
	}

	out, errS, code := runCmd(t, []string{"ahead", "--namespace", "x"}, "")
	if code != 0 {
		t.Fatalf("ahead --namespace x: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## In progress")
	if len(body) != 1 || !strings.Contains(body[0], "wip-x") || !strings.HasSuffix(body[0], "  est=n/a(no-data)") {
		t.Errorf("## In progress body = %q, want only wip-x with est=n/a(no-data)", body)
	}
	releasesIdx := strings.Index(out, "## Milestones")
	inProgressIdx := strings.Index(out, "## In progress")
	readyIdx := strings.Index(out, "## Ready")
	if readyIdx >= inProgressIdx || inProgressIdx >= releasesIdx {
		t.Errorf("ahead output = %q, want ## Ready, ## In progress, ## Milestones in that order", out)
	}
	if body := aheadSection(t, out, "## Ready"); len(body) != 1 || body[0] != "(none)" {
		t.Errorf("## Ready body = %q, want [(none)]", body)
	}
	if body := aheadSection(t, out, "## Course 1"); len(body) != 1 || !strings.Contains(body[0], "blocked-by-wip") {
		t.Errorf("## Course 1 body = %q, want blocked-by-wip", body)
	}
	if !strings.Contains(out, "- In progress: 0 tokens (1 beads)\n- Ready: 0 tokens (0 beads)") {
		t.Errorf("ahead output = %q, want the In progress summary line right before the Ready one", out)
	}
}

func TestAheadInProgressEstimate(t *testing.T) {
	initBeadsDir(t)
	for _, title := range []string{"done-a", "done-b", "done-c"} {
		id := mustCreateBead(t, "--title", title)
		mustAddTokenCostAt(t, mustCanonicalID(t, id), 2000, 0, "2026-01-01T00:00:00.000Z")
		if _, errS, code := runCmd(t, []string{"close", id, "--summary", "s"}, ""); code != 0 {
			t.Fatalf("close %s: exit code = %d, stderr = %q", id, code, errS)
		}
	}
	wip := mustCreateBead(t, "--title", "wip-estimated")
	if _, errS, code := runCmd(t, []string{"update", "--claim", wip}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## In progress")
	if len(body) != 1 || !strings.Contains(body[0], "wip-estimated") || !strings.Contains(body[0], "  est=2.00k(") {
		t.Errorf("## In progress body = %q, want wip-estimated with est=2.00k(...)", body)
	}
	if !strings.Contains(out, "- In progress: 2.00k tokens (1 beads)") {
		t.Errorf("ahead output = %q, want \"- In progress: 2.00k tokens (1 beads)\"", out)
	}
}

func TestAheadInProgressNone(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "just-open")

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	if body := aheadSection(t, out, "## In progress"); len(body) != 1 || body[0] != "(none)" {
		t.Errorf("## In progress body = %q, want [(none)]", body)
	}
	if !strings.Contains(out, "- In progress: 0 tokens (0 beads)") {
		t.Errorf("ahead output = %q, want \"- In progress: 0 tokens (0 beads)\"", out)
	}
}

func mustCloseWithActual(t *testing.T, title string, priority, tokens int) {
	t.Helper()
	id := mustCreateBead(t, "--title", title, "--priority", strconv.Itoa(priority))
	mustAddTokenCostAt(t, mustCanonicalID(t, id), tokens, 0, "2026-01-01T00:00:00.000Z")
	if _, errS, code := runCmd(t, []string{"close", id, "--summary", "s"}, ""); code != 0 {
		t.Fatalf("close %s: exit code = %d, stderr = %q", id, code, errS)
	}
}

func TestAheadBatchSkipsOversizedReady(t *testing.T) {
	initBeadsDir(t)
	for i := range 3 {
		mustCloseWithActual(t, fmt.Sprintf("closed-p1-%d", i), 1, 5000)
	}
	for i := range 3 {
		mustCloseWithActual(t, fmt.Sprintf("closed-p2-%d", i), 2, 1000)
	}
	mustCreateBead(t, "--title", "a-ready", "--priority", "1")
	b := mustCreateBead(t, "--title", "b-ready", "--priority", "2")
	c := mustCreateBead(t, "--title", "c-ready", "--priority", "2")
	aliases := displayAliases(t, b, c)
	bAlias, cAlias := aliases[0], aliases[1]

	out, errS, code := runCmd(t, []string{"ahead", "--budget", "4000"}, "")
	if code != 0 {
		t.Fatalf("ahead --budget 4000: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, out, "## Batch")
	if len(body) != 2 || !strings.Contains(body[0], bAlias) || !strings.Contains(body[1], cAlias) {
		t.Errorf("## Batch body = %q, want exactly b-ready then c-ready (a-ready skipped)", body)
	}
	if !strings.Contains(out, "- Reserved by In progress: 0 tokens, available: 4.00k") {
		t.Errorf("ahead output = %q, want \"- Reserved by In progress: 0 tokens, available: 4.00k\"", out)
	}
	if !strings.Contains(out, "- Batch: 2.00k tokens (2 beads, estimated)") {
		t.Errorf("ahead output = %q, want \"- Batch: 2.00k tokens (2 beads, estimated)\"", out)
	}
}

func TestAheadBatchReservesInProgress(t *testing.T) {
	initBeadsDir(t)
	for i := range 3 {
		mustCloseWithActual(t, fmt.Sprintf("closed-p1-%d", i), 1, 5000)
	}
	for i := range 3 {
		mustCloseWithActual(t, fmt.Sprintf("closed-p2-%d", i), 2, 1000)
	}
	d := mustCreateBead(t, "--title", "d-wip", "--priority", "2")
	if _, errS, code := runCmd(t, []string{"update", "--claim", d}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}
	b := mustCreateBead(t, "--title", "b-ready", "--priority", "2")
	c := mustCreateBead(t, "--title", "c-ready", "--priority", "2")
	aliases := displayAliases(t, b, c)
	bAlias, cAlias := aliases[0], aliases[1]

	const dEstimate, budget = 1000.0, 2900.0
	available := budget - dEstimate
	if available < 1000 || available >= 2000 {
		t.Fatalf("test setup: available = %v, want in [1000,2000) so exactly one of b/c (1000 each) fits", available)
	}

	out, errS, code := runCmd(t, []string{"ahead", "--budget", "2900"}, "")
	if code != 0 {
		t.Fatalf("ahead --budget 2900: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Reserved by In progress: 1.00k tokens, available: 1.90k") {
		t.Errorf("ahead output = %q, want \"- Reserved by In progress: 1.00k tokens, available: 1.90k\"", out)
	}
	body := aheadSection(t, out, "## Batch")
	if len(body) != 1 || !strings.Contains(body[0], bAlias) || strings.Contains(body[0], cAlias) {
		t.Errorf("## Batch body = %q, want exactly one line for b-ready, not c-ready", body)
	}
	if !strings.Contains(out, "- Batch: 1.00k tokens (1 beads, estimated)") {
		t.Errorf("ahead output = %q, want \"- Batch: 1.00k tokens (1 beads, estimated)\"", out)
	}
}

func TestAheadBatchNoneWhenNothingFits(t *testing.T) {
	initBeadsDir(t)
	for i := range 3 {
		mustCloseWithActual(t, fmt.Sprintf("closed-p1-%d", i), 1, 5000)
	}
	for i := range 3 {
		mustCloseWithActual(t, fmt.Sprintf("closed-p2-%d", i), 2, 1000)
	}
	d := mustCreateBead(t, "--title", "d-wip", "--priority", "2")
	if _, errS, code := runCmd(t, []string{"update", "--claim", d}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}
	mustCreateBead(t, "--title", "b-ready", "--priority", "2")

	out, errS, code := runCmd(t, []string{"ahead", "--budget", "500"}, "")
	if code != 0 {
		t.Fatalf("ahead --budget 500: exit code = %d, stderr = %q", code, errS)
	}
	if body := aheadSection(t, out, "## Batch"); len(body) != 1 || body[0] != "(none)" {
		t.Errorf("## Batch body = %q, want [(none)]", body)
	}
	if !strings.Contains(out, "- Batch: 0 tokens (0 beads, estimated)") {
		t.Errorf("ahead output = %q, want \"- Batch: 0 tokens (0 beads, estimated)\"", out)
	}
}

func TestAheadBatchOmittedWithoutBudget(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "only-ready")

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "## Batch") || strings.Contains(out, "- Batch:") {
		t.Errorf("ahead output = %q, must not contain ## Batch or a - Batch: line when budget is n/a", out)
	}

	out, errS, code = runCmd(t, []string{"ahead", "--budget", "1000"}, "")
	if code != 0 {
		t.Fatalf("ahead --budget 1000: exit code = %d, stderr = %q", code, errS)
	}
	if body := aheadSection(t, out, "## Batch"); len(body) != 1 || body[0] != "(none)" {
		t.Errorf("## Batch body = %q, want [(none)] (est=n/a Bead must not join)", body)
	}
}

type failAfterQuerier struct {
	db   *sql.DB
	left int
}

var errInjectedQuery = errors.New("injected query failure")

func (f *failAfterQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if f.left <= 0 {
		return nil, errInjectedQuery
	}
	f.left--
	return f.db.QueryContext(ctx, query, args...)
}

func (f *failAfterQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if f.left <= 0 {
		return f.db.QueryRowContext(ctx, "SELECT injected_query_failure")
	}
	f.left--
	return f.db.QueryRowContext(ctx, query, args...)
}

func TestAheadLinesFailsOnEveryQueryError(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a-ready")
	mustCreateBead(t, "--title", "b-course", "--blocked-by", a)
	wip := mustCreateBead(t, "--title", "wip")
	if _, errS, code := runCmd(t, []string{"update", "--claim", wip}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}
	mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "waiter")}, "sign-off", "--kind", "human")
	id := mustCanonicalID(t, a)
	mustAddTokenCostAt(t, id, 100, 0, "2026-01-06T00:00:00.000Z")

	ctx := context.Background()
	db, err := storage.Open(ctx, beadsDir(t), storage.OpenOptions{Env: func(string) string { return "" }, Actor: "test"})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	opts := aheadOpts{calibration: true, weekUsed: 25, weekUsedSet: true, weekResetAt: now.AddDate(0, 0, -7), weekResetSet: true}

	for k := 0; ; k++ {
		if k > 1000 {
			t.Fatal("aheadLines never succeeded")
		}
		out, err := aheadLines(ctx, &failAfterQuerier{db: db.SQL, left: k}, now, "", opts)
		if err == nil {
			if k == 0 || len(out) == 0 {
				t.Fatalf("aheadLines succeeded with %d queries allowed and %d lines", k, len(out))
			}
			return
		}
		if out != nil {
			t.Fatalf("aheadLines with %d queries allowed: err = %v but out = %q, want nil", k, err, out)
		}
	}
}

func TestFormatReleaseDays(t *testing.T) {
	r := estimate.ReleaseETAResult{ClosePerDayLo: 24, ClosePerDayHi: 48, EarlyDays: 2, LateDays: 4}
	if got, want := formatReleaseDays(r), "close/日=24.0〜48.0  残り日数=2.0日〜4.0日"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	r = estimate.ReleaseETAResult{EarlyDaysNone: true, LateDaysNone: true}
	if got, want := formatReleaseDays(r), "close/日=0.0〜0.0  残り日数=見込み無し"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
