// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func newlyBlockedSection(out string) string {
	_, after, ok := strings.Cut(out, "\n## Newly blocked\n")
	if !ok {
		return ""
	}
	return after
}

func TestDepAddBlocksReportsDownstreamNewlyBlocked(t *testing.T) {
	initBeadsDir(t)
	down := mustCreateBead(t, "--title", "downstream-x", "--priority", "2")
	up := mustCreateBead(t, "--title", "upstream-y", "--priority", "2")

	out := mustDepAdd(t, "--type", "blocks", down, up)
	sec := newlyBlockedSection(out)
	if !strings.Contains(sec, "downstream-x") || strings.Contains(sec, "upstream-y") {
		t.Fatalf("dep add blocks output = %q", out)
	}
	if strings.Index(out, "## Priority changes") > strings.Index(out, "## Newly blocked") {
		t.Fatalf("Newly blocked must follow Priority changes: %q", out)
	}
}

func TestDepAddParentChildReportsParentNewlyBlocked(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent-x", "--priority", "2")
	child := mustCreateBead(t, "--title", "child-y", "--priority", "2")

	out := mustDepAdd(t, "--type", "parent-child", child, parent)
	sec := newlyBlockedSection(out)
	if !strings.Contains(sec, "parent-x") || strings.Contains(sec, "child-y") {
		t.Fatalf("dep add parent-child output = %q", out)
	}
}

func TestDepAddNoDropOmitsNewlyBlocked(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")
	c := mustCreateBead(t, "--title", "c")
	mustDepAdd(t, "--type", "blocks", a, b)

	for _, args := range [][]string{
		{"--type", "blocks", a, c},
		{"--type", "discovered-from", b, c},
	} {
		if out := mustDepAdd(t, args...); strings.Contains(out, "Newly blocked") {
			t.Fatalf("dep add %v output = %q, want no Newly blocked", args, out)
		}
	}
}

func TestDepAddDryRunReportsNewlyBlockedWithoutWriting(t *testing.T) {
	initBeadsDir(t)
	down := mustCreateBead(t, "--title", "downstream-x")
	up := mustCreateBead(t, "--title", "upstream-y")

	out := mustDepAdd(t, "--dry-run", "--type", "blocks", down, up)
	if !strings.HasPrefix(out, "Dry run: nothing written\n") || !strings.Contains(newlyBlockedSection(out), "downstream-x") {
		t.Fatalf("dry-run output = %q", out)
	}
	ready, _, _ := runCmd(t, []string{"ready"}, "")
	if !strings.Contains(ready, "downstream-x") {
		t.Fatalf("dry-run wrote the link: ready = %q", ready)
	}
}

func TestDepAddRaiseRejectionOmitsNewlyBlocked(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent-x", "--priority", "1")
	child := mustCreateBead(t, "--title", "child-y", "--priority", "4")

	out, errS, code := runCmd(t, []string{"dep", "add", "--type", "parent-child", child, parent}, "")
	if code != 1 || strings.Contains(out+errS, "Newly blocked") {
		t.Fatalf("raise rejection: code = %d, out = %q, err = %q", code, out, errS)
	}
}

func TestCreateWithParentReportsParentNewlyBlocked(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent-x")

	out, errS, code := runCmd(t, []string{"create", "--title", "child-y", "--parent", parent}, "")
	sec := newlyBlockedSection(out)
	if code != 0 || !strings.Contains(sec, "parent-x") || strings.Contains(sec, "child-y") {
		t.Fatalf("create --parent: code = %d, out = %q, err = %q", code, out, errS)
	}

	out, _, _ = runCmd(t, []string{"create", "--title", "child-z", "--parent", parent}, "")
	if strings.Contains(out, "Newly blocked") {
		t.Fatalf("second child must not report the already blocked parent: %q", out)
	}
}

func TestCreateBlockedByOmitsNewlyBlocked(t *testing.T) {
	initBeadsDir(t)
	up := mustCreateBead(t, "--title", "upstream-y")

	out, errS, code := runCmd(t, []string{"create", "--title", "downstream-x", "--blocked-by", up}, "")
	if code != 0 || strings.Contains(out, "Newly blocked") {
		t.Fatalf("create --blocked-by: code = %d, out = %q, err = %q", code, out, errS)
	}
}
