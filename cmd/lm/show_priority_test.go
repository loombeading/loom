// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var priorityT0 = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func priorityStamp(h int) string {
	return priorityT0.Add(time.Duration(h) * time.Hour).Format("2006-01-02T15:04:05.000Z")
}

func priorityFixture(t *testing.T) {
	t.Helper()
	initBeadsDir(t)
	setTimeAdjustCoefficients(t, map[string]string{
		"promote_after": "24h", "decay_after": "24h", "hold_after": "24h", "cancel_after": "720h",
		"redetect_window": "24h", "due_lead": "72h", "severity_3_ceiling": "2", "severity_3_floor": "4",
	})
}

func pinPriorityBead(t *testing.T, id string, sets string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	q := `UPDATE beads SET created_at = ?, updated_at = ?, status_set_at = NULL, claimed_by_set_at = NULL, priority_set_at = NULL,
		title_set_at = NULL, description_set_at = NULL, external_refs_set_at = NULL, due_at_set_at = NULL, expedite_until_set_at = NULL,
		severity_set_at = NULL, revived_at = NULL`
	if sets != "" {
		q += ", " + sets
	}
	q += " WHERE id = ?"
	all := append([]any{priorityStamp(0), priorityStamp(0)}, args...)
	if _, err := db.ExecContext(t.Context(), q, append(all, id)...); err != nil {
		t.Fatal(err)
	}
}

func prioritySection(t *testing.T, id string) string {
	t.Helper()
	out := mustRun(t, "show", id)
	_, rest, ok := strings.Cut(out, "## Priority\n")
	if !ok {
		t.Fatalf("no ## Priority section:\n%s", out)
	}
	section, _, _ := strings.Cut(rest, "\n\n")
	display := ""
	for line := range strings.SplitSeq(out, "\n") {
		if title, ok := strings.CutPrefix(line, "# "); ok {
			display = strings.Fields(title)[0]
			break
		}
	}
	return strings.ReplaceAll("## Priority\n"+section+"\n", display, "{id}")
}

func setPriorityNow(t *testing.T, h int) {
	t.Helper()
	t.Setenv("LM_NOW", priorityT0.Add(time.Duration(h)*time.Hour).Format(time.RFC3339))
}

const (
	baseLines = "- ready_since: 2026-06-01T00:00:00.000Z (status)\n- progress: 2026-06-01T00:00:00.000Z (created_at)\n"
	dueLever  = "- lever: lm update {id} --due <時刻> -> demand\n"
	depLever  = "- lever: lm dep add <下流> {id} --type blocks -> demand\n"
	expLever  = "- lever: lm update {id} --expedite <期間> --reason <理由> -> P0\n"
)

func TestShowPrioritySectionPerState(t *testing.T) {
	priorityFixture(t)
	decay := mustCreateBead(t, "--title", "decay", "--priority", "1", "--severity", "3")
	held := mustCreateBead(t, "--title", "held", "--priority", "2", "--severity", "3")
	promoted := mustCreateBead(t, "--title", "promoted", "--priority", "4", "--severity", "3")
	leading := mustCreateBead(t, "--title", "leading", "--priority", "1", "--severity", "1")
	expediting := mustCreateBead(t, "--title", "expediting", "--priority", "4", "--severity", "1")
	claimed := mustCreateBead(t, "--title", "claimed", "--priority", "3", "--severity", "1")
	pinPriorityBead(t, decay, "")
	pinPriorityBead(t, held, "")
	pinPriorityBead(t, promoted, "due_at = ?", priorityStamp(1000))
	pinPriorityBead(t, leading, "due_at = ?", priorityStamp(100))
	pinPriorityBead(t, expediting, "expedite_until = ?, expedite_reason = 'urgent'", priorityStamp(100))
	pinPriorityBead(t, claimed, "status = 'in_progress', claimed_by = 'someone'")
	setPriorityNow(t, 84)

	decayOffShadow := "## Priority\n- stored: P1\n- severity: 3 (ceiling=P2 floor=P4 hold=yes)\n- graph: P1 (self)\n- expedite: none\n- due: none\n- demand: none\n" + baseLines +
		"- state: decayed  [if time_adjust=on]\n- effective: P1 (stored)\n- effective_if_on: P4 (decayed)\n" +
		"- next: 2026-06-05T00:00:00.000Z (in 12h0m) hold  [if time_adjust=on]\n" +
		"- next: 2026-07-05T00:00:00.000Z (in 732h0m) hold-cancel eligible  [if time_adjust=on]\n" + dueLever + depLever + expLever
	decayOn := "## Priority\n- stored: P1\n- severity: 3 (ceiling=P2 floor=P4 hold=yes)\n- graph: P1 (self)\n- expedite: none\n- due: none\n- demand: none\n" + baseLines +
		"- state: decayed\n- effective: P4 (decayed)\n" +
		"- next: 2026-06-05T00:00:00.000Z (in 12h0m) hold\n- next: 2026-07-05T00:00:00.000Z (in 732h0m) hold-cancel eligible\n" + dueLever + depLever + expLever

	for _, mode := range []string{"off", "shadow"} {
		setTimeAdjustCoefficients(t, map[string]string{"time_adjust": mode})
		if got := prioritySection(t, decay); got != decayOffShadow {
			t.Errorf("decay, time_adjust=%s:\n%s\nwant:\n%s", mode, got, decayOffShadow)
		}
	}
	setTimeAdjustCoefficients(t, map[string]string{"time_adjust": "on"})
	if got := prioritySection(t, decay); got != decayOn {
		t.Errorf("decay, time_adjust=on:\n%s\nwant:\n%s", got, decayOn)
	}

	setTimeAdjustCoefficients(t, map[string]string{"time_adjust": "shadow"})
	for name, tc := range map[string]struct{ id, want string }{
		"held": {held, "## Priority\n- stored: P2\n- severity: 3 (ceiling=P2 floor=P4 hold=yes)\n- graph: P2 (self)\n- expedite: none\n- due: none\n- demand: none\n" + baseLines +
			"- state: held  [if time_adjust=on]\n- effective: P2 (stored)\n- effective_if_on: P4 (decayed)\n" +
			"- next: 2026-07-04T00:00:00.000Z (in 708h0m) hold-cancel eligible  [if time_adjust=on]\n" +
			"- lever: lm update {id} --revive --reason <理由> -> back to Ready\n" + dueLever + depLever + expLever},
		"promoted with the due lead pending": {promoted, "## Priority\n- stored: P4\n- severity: 3 (ceiling=P2 floor=P4 hold=yes)\n- graph: P4 (self)\n- expedite: none\n" +
			"- due: 2026-07-12T16:00:00.000Z (lead from 2026-07-09T16:00:00.000Z, pending)\n- demand: due\n" + baseLines +
			"- state: promoted  [if time_adjust=on]\n- effective: P4 (stored)\n- effective_if_on: P2 (aged)\n" +
			"- next: 2026-07-09T16:00:00.000Z (in 844h0m) due lead -> P1\n" +
			"- lever: lm update {id} --severity 2 -> ceiling P1\n" + expLever},
		"due lead reached": {leading, "## Priority\n- stored: P1\n- severity: 1 (ceiling=P1 floor=none hold=no)\n- graph: P1 (self)\n- expedite: none\n" +
			"- due: 2026-06-05T04:00:00.000Z (lead from 2026-06-02T04:00:00.000Z, active)\n- demand: due\n" + baseLines +
			"- state: normal  [if time_adjust=on]\n- effective: P1 (stored)\n- next: none\n" + expLever},
		"expedite active": {expediting, "## Priority\n- stored: P4\n- severity: 1 (ceiling=P1 floor=none hold=no)\n- graph: P0 via expedite\n" +
			"- expedite: active until 2026-06-05T04:00:00.000Z (urgent)\n- due: none\n- demand: none\n" + baseLines +
			"- state: normal  [if time_adjust=on]\n- effective: P0 (expedite)\n" +
			"- next: 2026-06-05T04:00:00.000Z (in 16h0m) expedite expires -> P4\n" + dueLever + depLever},
		"claimed": {claimed, "## Priority\n- stored: P3\n- severity: 1 (ceiling=P1 floor=none hold=no)\n- graph: P3 (self)\n- expedite: none\n- due: none\n- demand: none\n" +
			"- ready_since: not ready\n- progress: 2026-06-01T00:00:00.000Z (created_at)\n" +
			"- state: normal  [if time_adjust=on]\n- effective: P3 (stored)\n- next: none\n" + dueLever + depLever + expLever},
	} {
		if got := prioritySection(t, tc.id); got != tc.want {
			t.Errorf("%s:\n%s\nwant:\n%s", name, got, tc.want)
		}
	}

	setPriorityNow(t, 120)
	wantExpired := "## Priority\n- stored: P4\n- severity: 1 (ceiling=P1 floor=none hold=no)\n- graph: P4 (self)\n- expedite: expired at 2026-06-05T04:00:00.000Z\n- due: none\n- demand: none\n" + baseLines +
		"- state: normal  [if time_adjust=on]\n- effective: P4 (stored)\n- next: none\n" + dueLever + depLever + expLever
	if got := prioritySection(t, expediting); got != wantExpired {
		t.Errorf("expired expedite:\n%s\nwant:\n%s", got, wantExpired)
	}
	if again := prioritySection(t, expediting); again != wantExpired {
		t.Errorf("same LM_NOW, different section:\n%s", again)
	}
}

func TestShowPriorityDemandAndSourceReferences(t *testing.T) {
	priorityFixture(t)
	epic := mustCreateBead(t, "--title", "epic", "--priority", "1", "--label", "milestone:x")
	upstream := mustCreateBead(t, "--title", "upstream", "--priority", "3", "--severity", "3", "--parent", epic)
	mustCreateBead(t, "--title", "downstream", "--priority", "1", "--blocked-by", upstream)
	for _, id := range []string{epic, upstream} {
		pinPriorityBead(t, id, "")
	}
	setPriorityNow(t, 30)
	out := mustRun(t, "show", upstream)
	for _, want := range []string{"- graph: P1 via ", "- demand: milestone ", ", blocks ", "- ready_since: 2026-06-01T00:00:00.000Z (status)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestShowPriorityTerminalAndGate(t *testing.T) {
	priorityFixture(t)
	setPriorityNow(t, 84)
	done := mustCreateBead(t, "--title", "done")
	target := mustCreateBead(t, "--title", "target")
	mustRun(t, "close", done, "--reason", "r")
	if out := mustRun(t, "show", done); strings.Contains(out, "## Priority") {
		t.Errorf("a closed Bead must not show the section:\n%s", out)
	}
	out, errS, code := runCmd(t, []string{"gate", "create", "--kind", "external", "--resolver", "watcher", "--subject", "s", "--blocks", target}, "")
	if code != 0 {
		t.Fatalf("gate create: %d %q", code, errS)
	}
	gate := mustCanonicalID(t, mustParseCreatedID(t, out))
	if shown := mustRun(t, "show", gate); !strings.Contains(shown, "## Priority\n- stored: P") || strings.Index(shown, "## Priority") > strings.Index(shown, "## Dependencies") {
		t.Errorf("a Gate shows the section before Dependencies:\n%s", shown)
	}
}

func TestShowPriorityNextTransitionMatchesReady(t *testing.T) {
	priorityFixture(t)
	setTimeAdjustCoefficients(t, map[string]string{"time_adjust": "on"})
	id := mustCreateBead(t, "--title", "decaying", "--priority", "1", "--severity", "3")
	pinPriorityBead(t, id, "")
	setPriorityNow(t, 30)

	section := prioritySection(t, id)
	var next string
	for line := range strings.SplitSeq(section, "\n") {
		if rest, ok := strings.CutPrefix(line, "- next: "); ok && strings.Contains(line, "decay -> P3") {
			next, _, _ = strings.Cut(rest, " ")
		}
	}
	if next != priorityStamp(48) {
		t.Fatalf("decay time = %q, want %s in:\n%s", next, priorityStamp(48), section)
	}
	at, err := time.Parse(time.RFC3339Nano, next)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LM_NOW", at.Add(-time.Millisecond).Format(time.RFC3339Nano))
	if before := mustRun(t, "ready"); !strings.Contains(before, "eff=P2(decayed)") {
		t.Errorf("one millisecond before the transition ready must show P2:\n%s", before)
	}
	t.Setenv("LM_NOW", at.Format(time.RFC3339Nano))
	if after := mustRun(t, "ready"); !strings.Contains(after, "eff=P3(decayed)") {
		t.Errorf("at the transition ready must show P3:\n%s", after)
	}
}
