// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func setLabelsRaw(t *testing.T, id, labelsJSON string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), "UPDATE beads SET labels = ? WHERE id = ?", labelsJSON, mustCanonicalID(t, id)); err != nil {
		t.Fatal(err)
	}
}

func TestCheckGateCyclesCleanExitsZero(t *testing.T) {
	initBeadsDir(t)
	out, errS, code := runCmd(t, []string{"check", "gate-cycles"}, "")
	if code != 0 || errS != "" {
		t.Fatalf("exit = %d, stderr = %q", code, errS)
	}
	if want := "## Cycles\n(none)\n\n## No owner\n(none)\n"; out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestCheckGateCyclesReportsViolations(t *testing.T) {
	initBeadsDir(t)
	const pr = "https://github.com/o/r/pull/7"
	p := mustCreateBead(t, "--title", "owner", "--external-ref", pr)
	g := mustGateCreateCmd(t, []string{p}, "wait", "--kind", "adjudicate")
	setLabelsRaw(t, g, `["kind:adjudicate","material:`+pr+`"]`)
	lost := mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "b")}, "lost", "--kind", "adjudicate")
	setLabelsRaw(t, lost, `["wait:pr-merged:https://x/1"]`)

	out, errS, code := runCmd(t, []string{"check", "gate-cycles"}, "")
	if code != 1 || errS != "" {
		t.Fatalf("exit = %d, stderr = %q, want 1 and no stderr", code, errS)
	}
	dg, dp, dl := displayOf(t, g), displayOf(t, p), displayOf(t, lost)
	want := "## Cycles\n- " + dg + " -> " + dp + " -> " + dg + "\n\n## No owner\n- " + dl + ` "https://x/1"` + "\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestCheckGateCyclesUnreadableExitsTwo(t *testing.T) {
	initBeadsDir(t)
	dropTable(t, "beads")
	out, errS, code := runCmd(t, []string{"check", "gate-cycles"}, "")
	if code != 2 || out != "" || !strings.HasPrefix(errS, "Error: ") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q; want 2, empty stdout, Error: on stderr", code, out, errS)
	}

	t.Setenv("LM_DIR", t.TempDir())
	out, errS, code = runCmd(t, []string{"check", "gate-cycles"}, "")
	if code != 2 || out != "" || !strings.HasPrefix(errS, "Error: ") {
		t.Fatalf("missing db: exit = %d, stdout = %q, stderr = %q", code, out, errS)
	}
}

func TestCheckUsageErrors(t *testing.T) {
	initBeadsDir(t)
	for _, args := range [][]string{{"check"}, {"check", "nope"}, {"check", "gate-cycles", "extra"}} {
		if _, _, code := runCmd(t, args, ""); code != 1 {
			t.Errorf("%v: exit = %d, want 1", args, code)
		}
	}
}

func displayOf(t *testing.T, id string) string {
	t.Helper()
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show %s: %d %q", id, code, errS)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if h, ok := strings.CutPrefix(line, "# "); ok {
			return h
		}
	}
	t.Fatalf("show %s has no heading", id)
	return ""
}
