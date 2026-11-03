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

func holdScene(t *testing.T, mode string) (string, string) {
	t.Helper()
	initBeadsDir(t)
	setTimeAdjustCoefficients(t, map[string]string{
		"promote_after": "24h", "decay_after": "24h", "hold_after": "24h",
		"severity_3_ceiling": "2", "severity_3_floor": "4", "time_adjust": mode,
	})
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	hold := mustCreateBeadAlias(t, "--title", "nr-hold", "--priority", "4", "--severity", "3")
	down := mustCreateBeadAlias(t, "--title", "nr-down", "--priority", "4")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET created_at = ?, status_set_at = NULL, claimed_by_set_at = NULL, priority_set_at = NULL,
		title_set_at = NULL, description_set_at = NULL, external_refs_set_at = NULL, due_at_set_at = NULL, expedite_until_set_at = NULL,
		severity_set_at = NULL, revived_at = NULL WHERE title = 'nr-hold'`, t0.Format("2006-01-02T15:04:05.000Z")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LM_NOW", t0.Add(60*time.Hour).Format(time.RFC3339))
	return hold, down
}

func newlyReadySection(out string) (string, bool) {
	_, section, ok := strings.Cut(out, "## Newly ready")
	if !ok {
		return "", false
	}
	section, _, _ = strings.Cut(section, "## Newly blocked")
	return strings.TrimSpace(section), true
}

func TestNewlyReadyFromHold(t *testing.T) {
	writes := map[string]func(t *testing.T, hold, down string) string{
		"dep add": func(t *testing.T, hold, down string) string {
			t.Helper()
			return mustRun(t, "dep", "add", down, hold, "--allow-priority-change")
		},
		"update severity": func(t *testing.T, hold, _ string) string {
			t.Helper()
			return mustRun(t, "update", hold, "--severity", "2", "--reason", "r")
		},
		"update expedite": func(t *testing.T, hold, _ string) string {
			t.Helper()
			return mustRun(t, "update", hold, "--expedite", "1h", "--reason", "r")
		},
		"update due": func(t *testing.T, hold, _ string) string {
			t.Helper()
			return mustRun(t, "update", hold, "--due", "2026-06-05T00:00:00Z", "--reason", "r")
		},
	}
	for name, write := range writes {
		t.Run("on: "+name, func(t *testing.T) {
			hold, down := holdScene(t, "on")
			out := write(t, hold, down)
			section, ok := newlyReadySection(out)
			if !ok || !strings.Contains(section, "nr-hold") {
				t.Errorf("want ## Newly ready listing nr-hold:\n%s", out)
			}
			if strings.Contains(section, "nr-down") {
				t.Errorf("the new downstream is not newly ready:\n%s", out)
			}
		})
		for _, mode := range []string{"off", "shadow"} {
			t.Run(mode+": "+name, func(t *testing.T) {
				hold, down := holdScene(t, mode)
				if out := write(t, hold, down); strings.Contains(out, "## Newly ready") {
					t.Errorf("time_adjust=%s must not print ## Newly ready:\n%s", mode, out)
				}
			})
		}
	}

	t.Run("on: nothing released prints an empty section", func(t *testing.T) {
		_, down := holdScene(t, "on")
		out := mustRun(t, "update", down, "--severity", "2", "--reason", "r")
		if section, ok := newlyReadySection(out); !ok || strings.Contains(section, "nr-hold") {
			t.Errorf("want ## Newly ready without nr-hold:\n%s", out)
		}
	})

	t.Run("on: create --parent prints the section", func(t *testing.T) {
		hold, _ := holdScene(t, "on")
		out := mustRun(t, "create", "--title", "nr-child", "--parent", hold)
		if section, ok := newlyReadySection(out); !ok || strings.Contains(section, "nr-hold") {
			t.Errorf("want ## Newly ready without nr-hold:\n%s", out)
		}
	})

	t.Run("off: create --parent prints no section", func(t *testing.T) {
		hold, _ := holdScene(t, "off")
		if out := mustRun(t, "create", "--title", "nr-child", "--parent", hold); strings.Contains(out, "## Newly ready") {
			t.Errorf("time_adjust=off must not print ## Newly ready:\n%s", out)
		}
	})

	t.Run("dep add --dry-run prints no section", func(t *testing.T) {
		hold, down := holdScene(t, "on")
		out := mustRun(t, "dep", "add", down, hold, "--dry-run")
		if !strings.Contains(out, "Dry run: nothing written") || strings.Contains(out, "## Newly ready") {
			t.Errorf("dry run output:\n%s", out)
		}
	})

	t.Run("dep add that is refused prints no section", func(t *testing.T) {
		hold, _ := holdScene(t, "on")
		urgent := mustCreateBeadAlias(t, "--title", "nr-urgent", "--priority", "1")
		out, errS, code := runCmd(t, []string{"dep", "add", urgent, hold}, "")
		if code == 0 || strings.Contains(out+errS, "## Newly ready") {
			t.Errorf("want a refusal without ## Newly ready: code=%d stdout=%q stderr=%q", code, out, errS)
		}
	})

	t.Run("on: same LM_NOW gives the same section", func(t *testing.T) {
		sections := make([]string, 2)
		for i := range sections {
			hold, down := holdScene(t, "on")
			section, ok := newlyReadySection(mustRun(t, "dep", "add", down, hold, "--allow-priority-change"))
			if !ok {
				t.Fatal("no ## Newly ready")
			}
			sections[i] = strings.NewReplacer(hold, "H", down, "D").Replace(section)
		}
		if sections[0] != sections[1] {
			t.Errorf("section differs between runs:\n%s\nvs\n%s", sections[0], sections[1])
		}
	})
}
