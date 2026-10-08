// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func mustExport(t *testing.T) string {
	t.Helper()
	out, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export: exit code = %d, stderr = %q", code, errS)
	}
	return out
}

func terminalPrereqs(t *testing.T) (string, string) {
	t.Helper()
	closed := mustCreateBeadAlias(t, "--title", "closed prereq")
	if _, errS, code := runCmd(t, []string{"close", closed, "--reason", "done"}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	cancelled := mustCreateBeadAlias(t, "--title", "cancelled prereq")
	if _, errS, code := runCmd(t, []string{"update", cancelled, "--status", "cancelled", "--reason", "drop"}, ""); code != 0 {
		t.Fatalf("cancel: exit code = %d, stderr = %q", code, errS)
	}
	return closed, cancelled
}

func TestDepAddBlocksRejectsTerminalPrereq(t *testing.T) {
	initBeadsDir(t)
	closed, cancelled := terminalPrereqs(t)
	bead := mustCreateBeadAlias(t, "--title", "downstream")

	for _, prereq := range []string{closed, cancelled} {
		before := mustExport(t)
		_, errS, code := runCmd(t, []string{"dep", "add", bead, prereq, "--type", "blocks", "--allow-priority-change"}, "")
		if code != 1 {
			t.Fatalf("dep add %s: exit code = %d, want 1; stderr = %q", prereq, code, errS)
		}
		if !strings.Contains(errS, prereq) || !strings.Contains(errS, "前提は既に終端") {
			t.Fatalf("dep add %s: stderr = %q, want terminal prereq ID and guidance", prereq, errS)
		}
		if after := mustExport(t); after != before {
			t.Fatalf("dep add %s wrote data:\nbefore=%s\nafter=%s", prereq, before, after)
		}
	}

	if _, errS, code := runCmd(t, []string{"dep", "add", bead, closed, "--type", "discovered-from"}, ""); code != 0 {
		t.Fatalf("discovered-from to closed: exit code = %d, stderr = %q", code, errS)
	}

	open := mustCreateBeadAlias(t, "--title", "open prereq")
	if _, errS, code := runCmd(t, []string{"dep", "add", bead, open, "--type", "blocks", "--allow-priority-change"}, ""); code != 0 {
		t.Fatalf("blocks to open prereq: exit code = %d, stderr = %q", code, errS)
	}
}

func TestCreateBlockedByRejectsTerminalPrereqs(t *testing.T) {
	initBeadsDir(t)
	closed, cancelled := terminalPrereqs(t)
	open := mustCreateBeadAlias(t, "--title", "open prereq")

	before := mustExport(t)
	_, errS, code := runCmd(t, withAdjudication(t, []string{"create", "--title", "downstream",
		"--blocked-by", open, "--blocked-by", closed, "--blocked-by", cancelled}), "")
	if code != 1 {
		t.Fatalf("create: exit code = %d, want 1; stderr = %q", code, errS)
	}
	if !strings.Contains(errS, closed) || !strings.Contains(errS, cancelled) || strings.Contains(errS, open) {
		t.Fatalf("create: stderr = %q, want both terminal prereqs and not the open one", errS)
	}
	if after := mustExport(t); after != before {
		t.Fatalf("create wrote data:\nbefore=%s\nafter=%s", before, after)
	}

	mustCreateBead(t, "--title", "downstream", "--blocked-by", open)
}
