// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestListNestsGrandchildUnderChildUnderEpic(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task")
	child := mustCreateBead(t, "--title", "Child", "--parent", epic)
	mustCreateBead(t, "--title", "Grandchild", "--parent", child)

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}

	out = strings.TrimPrefix(out, "Filter: status=\"unfinished\"\n")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("list output = %q, want 3 rows (epic, child, grandchild)", out)
	}
	if strings.HasPrefix(lines[0], " ") {
		t.Errorf("epic row = %q, want no indentation", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  - ") {
		t.Errorf("child row = %q, want 2-space indentation", lines[1])
	}
	if !strings.HasPrefix(lines[2], "    - ") {
		t.Errorf("grandchild row = %q, want 4-space indentation", lines[2])
	}
}

func TestListParentNotInResultSetSurfacesChildAtTopLevel(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task")
	child := mustCreateBead(t, "--title", "Child", "--parent", epic)

	if _, errS, code := runCmd(t, []string{"update", "--claim", epic}, ""); code != 0 {
		t.Fatalf("claim epic: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"list", "--status", "open"}, "")
	if code != 0 {
		t.Fatalf("list --status open: exit code = %d, stderr = %q", code, errS)
	}
	wantAlias := mustDisplayAlias(t, epic) + ".1"
	if !strings.Contains(out, "- "+wantAlias+" [") {
		t.Fatalf("list output = %q, want the child at top level (unindented) showing alias %q since its parent is excluded from the result set", out, wantAlias)
	}
	if strings.Contains(out, "  - "+wantAlias) {
		t.Fatalf("list output = %q, must not indent the child when its parent is excluded from the result set", out)
	}
	_ = child
}

func TestListLimitCountsBeadsBeforeBuildingTree(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task", "-p", "4")
	mustCreateBead(t, "--title", "Child1", "--parent", epic, "-p", "1")
	mustCreateBead(t, "--title", "Child2", "--parent", epic, "-p", "1")

	out, errS, code := runCmd(t, []string{"list", "--sort", "priority", "--limit", "2"}, "")
	if code != 0 {
		t.Fatalf("list --sort priority --limit 2: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Epic") {
		t.Fatalf("list --sort priority --limit 2 output = %q, must not include the P4 Epic (it sorts after the two P0 children)", out)
	}
	out = strings.TrimPrefix(out, "Filter: status=\"unfinished\"\n")
	if !strings.HasPrefix(out, "Truncated: showing 2 of 3\n") {
		t.Fatalf("list --sort priority --limit 2 output = %q, want it to start with a Truncated line counting Beads (2 of 3)", out)
	}
	rows := strings.Split(strings.TrimRight(strings.TrimPrefix(out, "Truncated: showing 2 of 3\n"), "\n"), "\n")
	if len(rows) != 2 {
		t.Fatalf("list --sort priority --limit 2 output = %q, want exactly 2 Bead rows", out)
	}
	for i, line := range rows {
		if strings.HasPrefix(line, " ") {
			t.Errorf("row %d = %q, want no indentation (its parent, the Epic, was truncated out)", i, line)
		}
	}
}
