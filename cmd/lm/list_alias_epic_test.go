// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestListShowsAliasForChildBead(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task")
	mustCreateBead(t, "--title", "Child", "--parent", epic)

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	wantAlias := mustDisplayAlias(t, epic) + ".1"
	if !strings.Contains(out, "  - "+wantAlias+" [") {
		t.Fatalf("list output = %q, want the child nested one level under its parent, showing alias %q with no parenthesized short ID", out, wantAlias)
	}

	if strings.Contains(out, " (bd-") {
		t.Fatalf("list output = %q, must not show a parenthesized short ID form", out)
	}

	if !strings.HasPrefix(strings.TrimPrefix(out, "Filter: status=\"unfinished\"\n"), "- ") {
		t.Fatalf("list output = %q, want the parentless Epic's row first with no indentation", out)
	}
}

func TestCreateShowsAliasForChildBead(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task")

	out, errS, code := runCmd(t, []string{"create", "--title", "Child", "--parent", epic}, "")
	if code != 0 {
		t.Fatalf("create: exit code = %d, stderr = %q", code, errS)
	}
	wantAlias := mustDisplayAlias(t, epic) + ".1"
	if !strings.HasPrefix(out, "- Created: "+wantAlias+" [") {
		t.Fatalf("create output = %q, want the child's alias %q as lm list shows it", out, wantAlias)
	}
}

func TestListAliasNumberingSkipsRemovedChild(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task")
	first := mustCreateBead(t, "--title", "First", "--parent", epic)
	second := mustCreateBead(t, "--title", "Second", "--parent", epic)

	if _, errS, code := runCmd(t, []string{"dep", "remove", first, epic, "--type", "parent-child"}, ""); code != 0 {
		t.Fatalf("dep remove: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	wantAlias := mustDisplayAlias(t, epic) + ".2"
	if !strings.Contains(out, wantAlias+" [") {
		t.Fatalf("list output = %q, want the surviving second child (%s) to keep alias %q after the first child's link was removed", out, second, wantAlias)
	}
}

func TestReadyExcludesEpicWithOpenChild(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task")
	mustCreateBead(t, "--title", "Child", "--parent", epic)

	out, errS, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "- "+epic+" ") {
		t.Errorf("ready output = %q, must not list an Epic with an unfinished child", out)
	}
}

func TestReadyListsEpicWithAllTerminalChildren(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task")
	child := mustCreateBead(t, "--title", "Child", "--parent", epic)

	if _, errS, code := runCmd(t, []string{"close", child}, ""); code != 0 {
		t.Fatalf("close child: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- "+mustDisplayAlias(t, epic)+" ") {
		t.Errorf("ready output = %q, want the Epic listed once every child is terminal", out)
	}
}

func TestBlockedExcludesEpicWithOpenChild(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task")
	mustCreateBead(t, "--title", "Child", "--parent", epic)

	out, errS, code := runCmd(t, []string{"blocked"}, "")
	if code != 0 {
		t.Fatalf("blocked: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "- "+epic+" ") {
		t.Errorf("blocked output = %q, must not list an Epic with an unfinished child", out)
	}
}
