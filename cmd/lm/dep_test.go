// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func mustDepAdd(t *testing.T, extra ...string) string {
	t.Helper()
	args := append([]string{"dep", "add"}, extra...)
	out, errS, code := runCmd(t, args, "")
	if code != 0 {
		t.Fatalf("dep add %v: exit code = %d, stderr = %q", extra, code, errS)
	}
	return out
}

func TestDepAddDefaultTypeIsBlocks(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	blocked := mustCreateBead(t, "--title", "blocked")

	out := mustDepAdd(t, blocked, blocker)
	if !strings.Contains(out, "- Linked:") || !strings.Contains(out, "blocks") {
		t.Fatalf("dep add output = %q, want a Linked line mentioning blocks", out)
	}

	readyOut, errS, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(readyOut, "blocked") {
		t.Fatalf("ready output = %q, must not list the blocked Bead", readyOut)
	}
}

func TestDepAddRejectsCycle(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")
	mustDepAdd(t, a, b)

	_, errS, code := runCmd(t, []string{"dep", "add", b, a}, "")
	if code == 0 {
		t.Fatal("dep add creating a cycle succeeded, want a rejection")
	}
	if !strings.Contains(errS, "cycle") {
		t.Fatalf("dep add cycle stderr = %q, want it to mention a cycle", errS)
	}
}

func TestDepAddRejectsSecondParent(t *testing.T) {
	initBeadsDir(t)
	child := mustCreateBead(t, "--title", "child")
	parent1 := mustCreateBead(t, "--title", "parent1")
	parent2 := mustCreateBead(t, "--title", "parent2")
	mustDepAdd(t, "--type", "parent-child", child, parent1)

	_, errS, code := runCmd(t, []string{"dep", "add", "--type", "parent-child", child, parent2}, "")
	if code == 0 {
		t.Fatal("dep add giving a second active parent succeeded, want a rejection")
	}
	if !strings.Contains(errS, "active parent") {
		t.Fatalf("dep add second-parent stderr = %q, want it to mention the active parent", errS)
	}
}

func TestDepRemoveThenReady(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	blocked := mustCreateBead(t, "--title", "blocked")
	mustDepAdd(t, blocked, blocker)

	readyOut, _, _ := runCmd(t, []string{"ready"}, "")
	if strings.Contains(readyOut, "blocked") {
		t.Fatalf("ready before dep remove = %q, must not list the blocked Bead", readyOut)
	}

	out, errS, code := runCmd(t, []string{"dep", "remove", blocked, blocker}, "")
	if code != 0 {
		t.Fatalf("dep remove: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Unlinked:") {
		t.Fatalf("dep remove output = %q, want an Unlinked line", out)
	}

	readyOut, errS, code = runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(readyOut, "blocked") {
		t.Fatalf("ready after dep remove = %q, want it to list the unblocked Bead", readyOut)
	}
}

func TestDepRemoveNotFound(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")

	_, errS, code := runCmd(t, []string{"dep", "remove", a, b}, "")
	if code == 0 {
		t.Fatal("dep remove of a nonexistent link succeeded, want a rejection")
	}
	if errS == "" {
		t.Fatal("dep remove of a nonexistent link: empty stderr")
	}
}

func TestDepAddWarnsOnDiscoveredFromFanoutThresholdCrossing(t *testing.T) {
	initBeadsDir(t)
	src := mustCreateBead(t, "--title", "source")

	for i := range 5 {
		child := mustCreateBead(t, "--title", "child")
		_, errS, code := runCmd(t, []string{"dep", "add", "--type", "discovered-from", child, src}, "")
		if code != 0 {
			t.Fatalf("dep add #%d: exit code = %d, stderr = %q", i+1, code, errS)
		}
		if strings.Contains(errS, "閾値") {
			t.Fatalf("dep add #%d (at or under threshold) stderr = %q, want no warning", i+1, errS)
		}
	}

	sixth := mustCreateBead(t, "--title", "child6")
	_, errS, code := runCmd(t, []string{"dep", "add", "--type", "discovered-from", sixth, src}, "")
	if code != 0 {
		t.Fatalf("dep add #6: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(errS, "閾値(5)を超えた") || !strings.Contains(errS, "現在6件") {
		t.Fatalf("dep add #6 (crossing) stderr = %q, want a fanout warning mentioning 閾値(5) and 現在6件", errS)
	}

	seventh := mustCreateBead(t, "--title", "child7")
	_, errS, code = runCmd(t, []string{"dep", "add", "--type", "discovered-from", seventh, src}, "")
	if code != 0 {
		t.Fatalf("dep add #7: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(errS, "閾値") {
		t.Fatalf("dep add #7 (past the crossing) stderr = %q, want no repeated warning", errS)
	}
}

func TestDepAddRejectsInvalidType(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")

	_, errS, code := runCmd(t, []string{"dep", "add", "--type", "bogus", a, b}, "")
	if code == 0 {
		t.Fatal("dep add with an invalid --type succeeded, want a rejection")
	}
	if !strings.Contains(errS, "invalid --type") {
		t.Fatalf("dep add invalid-type stderr = %q, want it to mention --type", errS)
	}
}

func TestDepAddRejectsRelatedWithGuidance(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")

	_, errS, code := runCmd(t, []string{"dep", "add", "--type", "related", a, b}, "")
	if code == 0 {
		t.Fatal("dep add --type related succeeded, want a rejection")
	}
	if !strings.Contains(errS, "related") || !strings.Contains(errS, "discovered-from") {
		t.Fatalf("dep add --type related stderr = %q, want guidance naming discovered-from", errS)
	}
}

func TestDepAddRejectsRaiseWithoutFlag(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent", "--priority", "1")
	child := mustCreateBead(t, "--title", "child", "--priority", "4")

	out, errS, code := runCmd(t, []string{"dep", "add", "--type", "parent-child", child, parent}, "")
	if code != 1 || out != "" {
		t.Fatalf("dep add raise: code = %d, stdout = %q, want 1 and empty", code, out)
	}
	if !strings.Contains(errS, "--allow-priority-change") || !strings.Contains(errS, "## Priority changes") || !strings.Contains(errS, "P4→P1 (via ") {
		t.Fatalf("dep add raise stderr = %q", errS)
	}
	showOut, _, _ := runCmd(t, []string{"show", child}, "")
	if strings.Contains(showOut, "parent-child") {
		t.Fatalf("rejected link was written: %q", showOut)
	}

	out = mustDepAdd(t, "--type", "parent-child", "--allow-priority-change", child, parent)
	if !strings.Contains(out, "## Priority changes") || !strings.Contains(out, "P4→P1 (via ") {
		t.Fatalf("dep add allowed output = %q", out)
	}
}

func TestDepDryRunWritesNothing(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent", "--priority", "1")
	child := mustCreateBead(t, "--title", "child", "--priority", "4")

	out := mustDepAdd(t, "--dry-run", "--type", "parent-child", child, parent)
	if !strings.HasPrefix(out, "Dry run: nothing written\n") || strings.Contains(out, "Linked") || !strings.Contains(out, "P4→P1") {
		t.Fatalf("dry-run output = %q", out)
	}
	out = mustDepAdd(t, "--type", "discovered-from", child, parent)
	if !strings.Contains(out, "## Priority changes\n(none)") {
		t.Fatalf("no-change output = %q", out)
	}
	out = mustDepAdd(t, "--allow-priority-change", "--type", "parent-child", child, parent)
	if !strings.Contains(out, "P4→P1") {
		t.Fatalf("link after dry-run must still raise, got %q", out)
	}
	out, errS, code := runCmd(t, []string{"dep", "remove", "--dry-run", "--type", "parent-child", child, parent}, "")
	if code != 0 || !strings.Contains(out, "P1→P4") {
		t.Fatalf("remove dry-run: code = %d, out = %q, err = %q", code, out, errS)
	}
}
