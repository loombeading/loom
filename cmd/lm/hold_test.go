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

func holdFixtureT0() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }

func pinBeadsToT0(t *testing.T, ids ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	pinned := holdFixtureT0().Format("2006-01-02T15:04:05.000Z")
	for _, id := range ids {
		if _, err := db.ExecContext(t.Context(), `UPDATE beads SET created_at = ?, status_set_at = NULL, claimed_by_set_at = NULL, priority_set_at = NULL,
			title_set_at = NULL, description_set_at = NULL, external_refs_set_at = NULL, due_at_set_at = NULL, expedite_until_set_at = NULL,
			severity_set_at = NULL, revived_at = NULL WHERE id = ?`, pinned, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), `UPDATE dependencies SET created_at = ? WHERE bead_id = ? OR depends_on_id = ?`, pinned, id, id); err != nil {
			t.Fatal(err)
		}
	}
}

func holdFixture(t *testing.T) {
	t.Helper()
	initBeadsDir(t)
	setTimeAdjustCoefficients(t, map[string]string{
		"promote_after": "24h", "decay_after": "24h", "hold_after": "24h", "cancel_after": "24h",
		"severity_3_ceiling": "2", "severity_3_floor": "4", "time_adjust": "on",
	})
	t.Setenv("LM_NOW", holdFixtureT0().Add(60*time.Hour).Format(time.RFC3339))
}

func TestHoldCancelCancelsOnlyHeldBeadsWithoutDemand(t *testing.T) {
	holdFixture(t)
	held := mustCreateBead(t, "--title", "hc-held", "--priority", "4", "--severity", "3")
	demanded := mustCreateBead(t, "--title", "hc-demanded", "--priority", "4", "--severity", "3")
	mustCreateBead(t, "--title", "hc-downstream", "--priority", "2", "--blocked-by", demanded)
	fresh := mustCreateBead(t, "--title", "hc-fresh", "--priority", "4", "--severity", "3")
	pinBeadsToT0(t, held, demanded)

	out := mustRun(t, "hold", "cancel")
	if !strings.Contains(out, "## Cancelled\n- Cancelled: ") || !strings.Contains(out, "[cancelled/P4] hc-held  held since 2026-06-02T00:00:00.000Z") {
		t.Errorf("held Bead missing from ## Cancelled:\n%s", out)
	}
	if strings.Contains(out, "hc-demanded") || strings.Contains(out, "hc-fresh") {
		t.Errorf("a Bead with demand or with recent progress must not be cancelled:\n%s", out)
	}
	if !strings.Contains(out, "## Newly ready") {
		t.Errorf("missing ## Newly ready:\n%s", out)
	}
	mustShowField(t, held, "status: \"cancelled\"\n")
	mustShowField(t, demanded, "status: \"open\"\n")
	mustShowField(t, fresh, "status: \"open\"\n")

	shown := mustRun(t, "show", held, "--full")
	if !strings.Contains(shown, "hold-cancel: held since 2026-06-02T00:00:00.000Z, no progress since 2026-06-01T00:00:00.000Z (60h0m0s); undo: lm update ") ||
		!strings.Contains(shown, "--reopen") {
		t.Errorf("audit reason lacks the hold origin, the idle length or the undo hint:\n%s", shown)
	}

	if again := mustRun(t, "hold", "cancel"); !strings.Contains(again, "## Cancelled\n(none)") {
		t.Errorf("second run must find nothing to cancel:\n%s", again)
	}
}

func TestHoldCancelDryRunWritesNothing(t *testing.T) {
	holdFixture(t)
	held := mustCreateBead(t, "--title", "hc-held", "--priority", "4", "--severity", "3")
	pinBeadsToT0(t, held)

	out := mustRun(t, "hold", "cancel", "--dry-run")
	if !strings.HasPrefix(out, "Dry run: nothing written\n") || !strings.Contains(out, "- Cancelled: ") {
		t.Errorf("dry-run output:\n%s", out)
	}
	mustShowField(t, held, "status: \"open\"\n")
	if shown := mustRun(t, "show", held, "--full"); strings.Contains(shown, "hold-cancel") {
		t.Errorf("dry-run left an audit record:\n%s", shown)
	}
}

func TestHoldCancelReopenBringsTheBeadBack(t *testing.T) {
	holdFixture(t)
	held := mustCreateBead(t, "--title", "hc-held", "--priority", "4", "--severity", "3")
	pinBeadsToT0(t, held)
	mustRun(t, "hold", "cancel")
	mustRun(t, "update", held, "--reopen", "--reason", "needed after all")
	mustShowField(t, held, "status: \"open\"\n")
	if out := mustRun(t, "hold", "cancel"); strings.Contains(out, "- Cancelled: ") {
		t.Errorf("a reopened Bead must not be cancelled again at once:\n%s", out)
	}
	mustShowField(t, held, "status: \"open\"\n")
}

func TestHoldCancelListsParentsThatBecomeReady(t *testing.T) {
	holdFixture(t)
	parent := mustCreateBead(t, "--title", "hc-parent", "--priority", "2")
	child := mustCreateBead(t, "--title", "hc-child", "--priority", "4", "--severity", "3", "--parent", parent)
	pinBeadsToT0(t, child)

	out := mustRun(t, "hold", "cancel")
	_, newly, _ := strings.Cut(out, "## Newly ready")
	if !strings.Contains(newly, "hc-parent") {
		t.Errorf("the parent that lost its last child must be listed under ## Newly ready:\n%s", out)
	}
}

func TestHoldCancelSkipsUnlessTimeAdjustIsOn(t *testing.T) {
	for _, mode := range []string{"off", "shadow"} {
		holdFixture(t)
		held := mustCreateBead(t, "--title", "hc-held", "--priority", "4", "--severity", "3")
		pinBeadsToT0(t, held)
		setTimeAdjustCoefficients(t, map[string]string{"time_adjust": mode})

		out := mustRun(t, "hold", "cancel")
		if !strings.HasPrefix(out, "- Skipped: time_adjust="+mode+"\n") || !strings.Contains(out, "## Cancelled\n(none)") || !strings.Contains(out, "## Newly ready\n(none)") {
			t.Errorf("time_adjust=%s output:\n%s", mode, out)
		}
		mustShowField(t, held, "status: \"open\"\n")
	}
}

func TestHoldCancelRejectsArgumentsAndUnknownFlags(t *testing.T) {
	holdFixture(t)
	held := mustCreateBead(t, "--title", "hc-held", "--priority", "4", "--severity", "3")
	pinBeadsToT0(t, held)
	for _, args := range [][]string{{"hold", "cancel", held}, {"hold", "cancel", "--nope"}} {
		if out, _, code := runCmd(t, args, ""); code != 1 || out != "" {
			t.Errorf("%v: exit = %d, stdout = %q, want 1 and nothing on stdout", args, code, out)
		}
	}
	mustShowField(t, held, "status: \"open\"\n")
}

func noDatabaseFixture(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)
}

func badNowFixture(t *testing.T) {
	t.Helper()
	holdFixture(t)
	t.Setenv("LM_NOW", "not-a-time")
}

func badCoefficientFixture(t *testing.T) {
	t.Helper()
	holdFixture(t)
	setTimeAdjustCoefficients(t, map[string]string{"cancel_after": "nope"})
}

func TestHoldCancelFailsBeforeWritingOnBadEnvironment(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T)
		args  []string
	}{
		{"no database", noDatabaseFixture, nil},
		{"bad namespace flag", holdFixture, []string{"--namespace", "Bad NS!"}},
		{"bad LM_NOW", badNowFixture, nil},
		{"bad coefficient", badCoefficientFixture, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			out, errS, code := runCmd(t, append([]string{"hold", "cancel"}, tc.args...), "")
			if code != 1 || out != "" || errS == "" {
				t.Errorf("exit = %d, stdout = %q, stderr = %q, want 1, nothing on stdout and a reason on stderr", code, out, errS)
			}
		})
	}
}

func TestHoldCancelRejectsReadOnlyDatabase(t *testing.T) {
	holdFixture(t)
	held := mustCreateBead(t, "--title", "hc-held", "--priority", "4", "--severity", "3")
	pinBeadsToT0(t, held)
	makeDBReadOnly(t)
	if out, errS, code := runCmd(t, []string{"hold", "cancel"}, ""); code != 1 || out != "" || errS == "" {
		t.Errorf("exit = %d, stdout = %q, stderr = %q, want 1 with the reason on stderr", code, out, errS)
	}
}

func TestHoldRequiresSubcommand(t *testing.T) {
	initBeadsDir(t)
	if _, errS, code := runCmd(t, []string{"hold"}, ""); code != 1 || !strings.Contains(errS, "lm hold requires a subcommand") {
		t.Errorf("exit = %d, stderr = %q", code, errS)
	}
}
