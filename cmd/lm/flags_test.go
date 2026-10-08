// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"reflect"
	"strings"
	"testing"
)

func newTestFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool("claim", false, "")
	fs.Bool("force", false, "")
	fs.String("reason", "", "")
	fs.String("description", "", "")
	fs.Int("priority", -1, "")
	fs.IntVar(new(int), "p", -1, "")
	return fs
}

func TestReorderArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "bool flag after positional",
			args: []string{"bd-1", "--claim"},
			want: []string{"--claim", "bd-1"},
		},
		{
			name: "value flag after multiple positionals",
			args: []string{"bd-1", "bd-2", "--reason", "x"},
			want: []string{"--reason", "x", "bd-1", "bd-2"},
		},
		{
			name: "equals form value flag after positional",
			args: []string{"bd-1", "--reason=x"},
			want: []string{"--reason=x", "bd-1"},
		},
		{
			name: "mixed flags before and after positionals",
			args: []string{"--force", "bd-1", "--claim"},
			want: []string{"--force", "--claim", "bd-1"},
		},
		{
			name: "shorthand value flag after positional",
			args: []string{"bd-1", "-p", "1"},
			want: []string{"-p", "1", "bd-1"},
		},
		{
			name: "double-dash stops reordering",
			args: []string{"--claim", "--", "-not-a-flag"},
			want: []string{"--claim", "--", "-not-a-flag"},
		},
		{
			name: "description dash value is positional-looking but consumed as value",
			args: []string{"bd-1", "--description", "-"},
			want: []string{"--description", "-", "bd-1"},
		},
		{
			name: "unknown flag left alone, no value consumed",
			args: []string{"bd-1", "--bogus", "bd-2"},
			want: []string{"--bogus", "bd-1", "bd-2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newTestFlagSet()
			got := reorderArgs(fs, tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("reorderArgs(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestValidatePriority(t *testing.T) {
	if err := validatePriority(-1); err != nil {
		t.Fatalf("validatePriority(-1) = %v, want nil (no filter)", err)
	}
	for _, p := range []int{1, 4} {
		if err := validatePriority(p); err != nil {
			t.Fatalf("validatePriority(%d) = %v, want nil", p, err)
		}
	}
	for _, p := range []int{-2, 0, 5} {
		err := validatePriority(p)
		if err == nil {
			t.Fatalf("validatePriority(%d) = nil, want error", p)
		}
		if err.Error() != "--priority must be between 1 and 4" {
			t.Fatalf("validatePriority(%d) error = %q, want %q", p, err.Error(), "--priority must be between 1 and 4")
		}
	}
}

func TestListPriorityOutOfRange(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"list", "--priority", "5"}, "")
	if code != 1 {
		t.Fatalf("list --priority 5: exit code = %d, want 1", code)
	}
	if !strings.Contains(errS, "--priority must be between 1 and 4") {
		t.Fatalf("list --priority 5 stderr = %q, want it to mention the valid range", errS)
	}
}

func TestReadyPriorityOutOfRange(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"ready", "--priority", "5"}, "")
	if code != 1 {
		t.Fatalf("ready --priority 5: exit code = %d, want 1", code)
	}
	if !strings.Contains(errS, "--priority must be between 1 and 4") {
		t.Fatalf("ready --priority 5 stderr = %q, want it to mention the valid range", errS)
	}
}

func TestUpdateFlagAfterID(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, errS, code := runCmd(t, []string{"update", id, "--claim"}, "")
	if code != 0 {
		t.Fatalf("update <id> --claim: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Updated:") {
		t.Fatalf("update <id> --claim output = %q, want an Updated line", out)
	}

	showOut, _, _ := runCmd(t, []string{"show", id}, "")
	if !strings.Contains(showOut, `status: "in_progress"`) {
		t.Fatalf("show after update <id> --claim = %q, want in_progress", showOut)
	}
}

func TestCloseFlagAfterMultipleIDs(t *testing.T) {
	initBeadsDir(t)
	id1 := mustCreateBead(t, "--title", "t1")
	id2 := mustCreateBead(t, "--title", "t2")

	out, errS, code := runCmd(t, []string{"close", id1, id2, "--reason", "done"}, "")
	if code != 0 {
		t.Fatalf("close <id1> <id2> --reason x: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Count(out, "- Closed:") != 2 {
		t.Fatalf("close output = %q, want two Closed lines", out)
	}
}

func TestDepAddFlagAfterIDs(t *testing.T) {
	initBeadsDir(t)
	child := mustCreateBead(t, "--title", "child")
	parent := mustCreateBead(t, "--title", "parent")

	out, errS, code := runCmd(t, []string{"dep", "add", child, parent, "--type", "parent-child"}, "")
	if code != 0 {
		t.Fatalf("dep add <a> <b> --type x: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "is child of") {
		t.Fatalf("dep add output = %q, want it to say the child relation", out)
	}
}

func TestShowFlagAfterID(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, errS, code := runCmd(t, []string{"show", id, "--all-deps"}, "")
	if code != 0 {
		t.Fatalf("show <id> --all-deps: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "t") {
		t.Fatalf("show <id> --all-deps output = %q, want the title", out)
	}
}

func TestUpdateStopsReorderingAfterDoubleDash(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"update", id, "--", "--claim"}, "")
	if code == 0 {
		t.Fatalf("update <id> -- --claim unexpectedly succeeded: %q", out)
	}
}
