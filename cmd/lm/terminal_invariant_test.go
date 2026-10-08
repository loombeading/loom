// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestCloseRefusesUnfinishedChildrenAndAcceptsChildrenListedFirst(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent")
	child := mustCreateBead(t, "--title", "child", "--parent", parent)

	out, errS, code := runCmd(t, []string{"close", parent, "--reason", "done"}, "")
	if code != 1 || !strings.Contains(out, "Rejected: ") || !strings.Contains(out, "unfinished children: "+mustDisplayAlias(t, child)) ||
		!strings.Contains(errS, "--type discovered-from") {
		t.Fatalf("close = (%q, %q, %d), want Rejected naming the child, the re-link hint on stderr, exit 1", out, errS, code)
	}
	mustShowField(t, parent, "status: \"open\"\n")

	mustRun(t, "close", child, parent, "--reason", "done")
	mustShowField(t, parent, "status: \"closed\"\n")
}

func TestDepAddAndReopenRefuseTerminalParent(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent")
	child := mustCreateBead(t, "--title", "child")
	mustRun(t, "close", parent, "--reason", "done")

	_, errS, code := runCmd(t, []string{"dep", "add", child, parent, "--type", "parent-child"}, "")
	if code != 1 || !strings.Contains(errS, "terminal parent: ") || !strings.Contains(errS, "--reopen") {
		t.Fatalf("dep add = (%q, %d), want terminal parent with the reopen hint on stderr, exit 1", errS, code)
	}

	mustRun(t, "close", child, "--reason", "done")
	mustRun(t, "dep", "add", child, parent, "--type", "parent-child")
	out, errS, code := runCmd(t, []string{"update", child, "--reopen"}, "")
	if code != 1 || !strings.Contains(out, "Rejected: ") || !strings.Contains(errS, "terminal parent: ") {
		t.Fatalf("reopen = (%q, %q, %d), want Rejected naming the parent, exit 1", out, errS, code)
	}
	mustShowField(t, child, "status: \"closed\"\n")
}

func TestCreateDiscoveredFromLinksAndSatisfiesMilestone(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic", "--label", "milestone:x")
	src := mustCreateBead(t, "--title", "src", "--parent", epic)
	loose := mustCreateBead(t, "--title", "loose")
	writeBeadsConfig(t, `{"create":{"require_milestone":true}}`)

	for range 6 {
		mustCreateBead(t, "--title", "derived", "--discovered-from", src)
	}
	out, errS, code := runCmd(t, []string{"create", "--title", "derived", "--discovered-from", src}, "")
	if code != 0 || strings.Contains(errS, "警告") {
		t.Fatalf("create = (%q, %q, %d), want success without a fan-out warning past the threshold crossing", out, errS, code)
	}
	if show := mustRun(t, "show", src); strings.Count(show, "discovered-from") < 7 {
		t.Errorf("show src = %q, want 7 discovered-from links", show)
	}

	_, errS, code = runCmd(t, []string{"create", "--title", "orphan", "--discovered-from", loose}, "")
	if code != 1 || !strings.Contains(errS, "派生元が無所属") {
		t.Fatalf("create from an unaffiliated source = (%q, %d), want 派生元が無所属 and exit 1", errS, code)
	}
	if _, _, code := runCmd(t, []string{"create", "--title", "orphan", "--discovered-from", "lm-zzzzzz"}, ""); code != 1 {
		t.Fatalf("create from a missing source: exit code = %d, want 1", code)
	}
}

func TestCreateDiscoveredFromWarnsAtFanoutThreshold(t *testing.T) {
	initBeadsDir(t)
	src := mustCreateBead(t, "--title", "src")
	for range 5 {
		mustCreateBead(t, "--title", "derived", "--discovered-from", src)
	}
	_, errS, code := runCmd(t, []string{"create", "--title", "derived", "--discovered-from", src}, "")
	if code != 0 || !strings.Contains(errS, "警告") {
		t.Fatalf("create = (%q, %d), want the fan-out warning on stderr", errS, code)
	}
}
