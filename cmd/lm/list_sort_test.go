// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
	"time"
)

func TestListSortUpdatedOrdersByUpdatedAtDescending(t *testing.T) {
	initBeadsDir(t)

	oldest := mustCreateBead(t, "--title", "oldest", "-p", "1")
	time.Sleep(20 * time.Millisecond)
	middle := mustCreateBead(t, "--title", "middle", "-p", "4")
	time.Sleep(20 * time.Millisecond)
	newest := mustCreateBead(t, "--title", "newest", "-p", "4")
	_ = oldest
	_ = middle
	_ = newest

	out, errS, code := runCmd(t, []string{"list", "--sort", "updated"}, "")
	if code != 0 {
		t.Fatalf("list --sort updated: exit code = %d, stderr = %q", code, errS)
	}
	out = strings.TrimPrefix(out, "Filter: status=\"unfinished\"\n")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("list --sort updated output = %q, want 3 rows", out)
	}
	if !strings.Contains(lines[0], "newest") || !strings.Contains(lines[1], "middle") || !strings.Contains(lines[2], "oldest") {
		t.Errorf("list --sort updated output = %q, want newest, middle, oldest in that order", out)
	}
}

func TestListSortUpdatedIsNestedLikeDefault(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "Epic", "--type", "task")
	mustCreateBead(t, "--title", "Child", "--parent", epic)

	out, errS, code := runCmd(t, []string{"list", "--sort", "updated"}, "")
	if code != 0 {
		t.Fatalf("list --sort updated: exit code = %d, stderr = %q", code, errS)
	}
	out = strings.TrimPrefix(out, "Filter: status=\"unfinished\"\n")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("list --sort updated output = %q, want 2 rows", out)
	}
	if strings.HasPrefix(lines[0], " ") {
		t.Errorf("epic row = %q, want no indentation", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  - ") {
		t.Errorf("child row = %q, want 2-space indentation (list is always a tree, even under --sort updated)", lines[1])
	}
}

func TestListSortDefaultIsUpdated(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "first", "-p", "1")
	time.Sleep(20 * time.Millisecond)
	mustCreateBead(t, "--title", "second", "-p", "4")

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	out = strings.TrimPrefix(out, "Filter: status=\"unfinished\"\n")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "second") {
		t.Errorf("list output = %q, want \"second\" first (updated_at desc, the default)", out)
	}
}

func TestListSortInvalidValueRejected(t *testing.T) {
	initBeadsDir(t)

	_, errS, code := runCmd(t, []string{"list", "--sort", "bogus"}, "")
	if code != 1 {
		t.Fatalf("list --sort bogus: exit code = %d, want 1", code)
	}
	if !strings.Contains(errS, "Error:") {
		t.Errorf("stderr = %q, want an Error: line", errS)
	}
}
