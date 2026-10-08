// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestReworkAddAndShowHistory(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, errS, code := runCmd(t, []string{"rework", "add", id, "--cause", "ci", "--reason", "PR #1"}, "")
	if code != 0 {
		t.Fatalf("rework add: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Rework recorded: ") {
		t.Errorf("rework add stdout = %q, want a - Rework recorded: line", out)
	}

	out, errS, code = runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	historyIdx := strings.Index(out, "## History")
	if historyIdx < 0 {
		t.Fatalf("show output missing ## History:\n%s", out)
	}
	if !strings.Contains(out[historyIdx:], "rework") {
		t.Errorf("show ## History missing a rework row:\n%s", out[historyIdx:])
	}
}

func TestReworkAddRejectsUnknownCauseBeforeOpeningDB(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"rework", "add", "no-such-id", "--cause", "typo"}, "")
	if code != 1 {
		t.Fatalf("rework add with unknown --cause: exit code = %d, want 1 (stdout=%q stderr=%q)", code, out, errS)
	}
	if !strings.Contains(errS, "invalid --cause") {
		t.Errorf("stderr = %q, want it to contain %q", errS, "invalid --cause")
	}
	if strings.Contains(out, "Rejected:") {
		t.Errorf("stdout = %q, want no Rejected: line (must reject before opening the DB)", out)
	}
	if strings.Contains(errS, "not found") {
		t.Errorf("stderr = %q, want no not found (must reject before resolving the ID)", errS)
	}
}

func TestReworkAddRejectsNonexistentIDAfterOpeningDB(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"rework", "add", "01hxxxxxxxxxxxxxxxxxxxxxxx", "--cause", "ci"}, "")
	if code != 1 {
		t.Fatalf("rework add with a valid --cause but no such ID: exit code = %d, want 1 (stdout=%q stderr=%q)", code, out, errS)
	}
	if !strings.Contains(out, "Rejected:") {
		t.Errorf("stdout = %q, want a Rejected: line (a well-formed but nonexistent ID reaches AddRework's own lookup)", out)
	}
}

func TestReworkAddRequiresCause(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, errS, code := runCmd(t, []string{"rework", "add", id}, "")
	if code != 1 {
		t.Fatalf("rework add with no --cause: exit code = %d, want 1 (stdout=%q stderr=%q)", code, out, errS)
	}
	if !strings.Contains(errS, "--cause is required") {
		t.Errorf("stderr = %q, want it to contain %q", errS, "--cause is required")
	}
}
