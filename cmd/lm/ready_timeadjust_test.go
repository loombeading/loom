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

func setTimeAdjustCoefficients(t *testing.T, kv map[string]string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for k, v := range kv {
		if _, err := db.ExecContext(t.Context(), `UPDATE coefficients SET value = ? WHERE key = ?`, v, k); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBlockedHeldRows(t *testing.T) {
	initBeadsDir(t)
	setTimeAdjustCoefficients(t, map[string]string{
		"promote_after": "24h", "decay_after": "24h", "hold_after": "24h",
		"severity_3_ceiling": "2", "severity_3_floor": "4",
	})
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	holdID := mustCreateBead(t, "--title", "ta-hold", "--priority", "4", "--severity", "3")
	blocker := mustCreateBead(t, "--title", "ta-blocker", "--priority", "2")
	mustCreateBead(t, "--title", "ta-blocked", "--priority", "2", "--blocked-by", blocker)
	pinned := t0.Format("2006-01-02T15:04:05.000Z")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET created_at = ?, status_set_at = NULL, claimed_by_set_at = NULL, priority_set_at = NULL,
		title_set_at = NULL, description_set_at = NULL, external_refs_set_at = NULL, due_at_set_at = NULL, expedite_until_set_at = NULL,
		severity_set_at = NULL, revived_at = NULL WHERE id = ?`, pinned, holdID); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	t.Setenv("LM_NOW", t0.Add(60*time.Hour).Format(time.RFC3339))

	for _, mode := range []string{"off", "shadow"} {
		setTimeAdjustCoefficients(t, map[string]string{"time_adjust": mode})
		if out := mustRun(t, "blocked"); strings.Contains(out, "← held:") || strings.Contains(out, "ta-hold") {
			t.Errorf("time_adjust=%s blocked must not list held rows:\n%s", mode, out)
		}
	}

	setTimeAdjustCoefficients(t, map[string]string{"time_adjust": "on"})
	on := mustRun(t, "blocked")
	var held, blockedRow string
	for line := range strings.SplitSeq(on, "\n") {
		switch {
		case strings.Contains(line, "ta-hold"):
			held = line
		case strings.Contains(line, "ta-blocked"):
			blockedRow = line
		}
	}
	if !strings.Contains(held, "[open/P4] ta-hold  ← held: no demand since 2026-06-01T00:00:00.000Z") || strings.Contains(held, "blocked by") {
		t.Errorf("held row = %q", held)
	}
	if !strings.Contains(blockedRow, "← blocked by:") || strings.Contains(blockedRow, "← held:") {
		t.Errorf("blocked row = %q", blockedRow)
	}
	if again := mustRun(t, "blocked"); again != on {
		t.Errorf("same LM_NOW, different output:\n%s\nvs\n%s", on, again)
	}
}

func TestReadyTimeAdjustedFlagAndSwitch(t *testing.T) {
	initBeadsDir(t)
	setTimeAdjustCoefficients(t, map[string]string{
		"promote_after": "24h", "decay_after": "24h", "hold_after": "24h",
		"severity_3_ceiling": "2", "severity_3_floor": "4",
	})
	t0 := time.Now().UTC()
	mustCreateBead(t, "--title", "ta-hold", "--priority", "4", "--severity", "3")
	mustCreateBead(t, "--title", "ta-decay", "--priority", "1", "--severity", "3")
	t.Setenv("LM_NOW", t0.Add(60*time.Hour).Format(time.RFC3339))

	plain := mustRun(t, "ready")
	if strings.Contains(plain, "- Held:") || strings.Contains(plain, "(decayed)") || !strings.Contains(plain, "ta-hold") {
		t.Errorf("shadow ready must keep the pre-adjustment output:\n%s", plain)
	}
	adjusted := mustRun(t, "ready", "--time-adjusted")
	if !strings.Contains(adjusted, "- Held: 1") || strings.Contains(adjusted, "ta-hold") || !strings.Contains(adjusted, "eff=P3(decayed)") {
		t.Errorf("--time-adjusted output:\n%s", adjusted)
	}
	if _, errS, code := runCmd(t, []string{"ready", "--time-adjusted", "--claim"}, ""); code == 0 || !strings.Contains(errS, "--time-adjusted") {
		t.Errorf("--time-adjusted --claim: exit = %d, stderr = %q", code, errS)
	}

	setTimeAdjustCoefficients(t, map[string]string{"time_adjust": "on"})
	if on := mustRun(t, "ready"); on != adjusted {
		t.Errorf("on ready differs from --time-adjusted:\n%s\nvs\n%s", on, adjusted)
	}
	claimed := mustRun(t, "ready", "--claim")
	if !strings.Contains(claimed, "ta-decay") || strings.Contains(claimed, "ta-hold") {
		t.Errorf("on --claim must take the first adjusted Bead and skip the held one:\n%s", claimed)
	}
	id := strings.Fields(strings.SplitN(claimed, "Claimed: ", 2)[1])[0]
	if shown := mustRun(t, "show", id, "--full"); !strings.Contains(shown, "ready --claim: now="+t0.Add(60*time.Hour).Format("2006-01-02T15:04:05")) {
		t.Errorf("claim reason lacks the now used:\n%s", shown)
	}
}
