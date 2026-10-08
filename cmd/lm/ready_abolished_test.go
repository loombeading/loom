// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestReadyExcludesAbolishedMilestoneDescendants(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "AbolishedEpic", "--type", "task", "--label", "milestone:x")
	child := mustCreateBead(t, "--title", "AbolishedChild", "--parent", epic)
	mustCreateBead(t, "--title", "AbolishedGrandchild", "--parent", child)
	mustCreateBead(t, "--title", "UnrelatedBead")

	if _, errS, code := runCmd(t, []string{"update", epic, "--add-label", "abolished-by:lm-any"}, ""); code != 0 {
		t.Fatalf("update add-label: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
	}
	for _, name := range []string{"AbolishedChild", "AbolishedGrandchild"} {
		if strings.Contains(out, name) {
			t.Errorf("ready output = %q, must not list %s under an abolished milestone", out, name)
		}
	}
	if !strings.Contains(out, "UnrelatedBead") {
		t.Errorf("ready output = %q, want the unrelated Bead listed", out)
	}

	for range 3 {
		claimOut, errS, code := runCmd(t, []string{"ready", "--claim"}, "")
		if code != 0 {
			t.Fatalf("ready --claim: exit code = %d, stderr = %q", code, errS)
		}
		if strings.Contains(claimOut, "AbolishedChild") || strings.Contains(claimOut, "AbolishedGrandchild") {
			t.Fatalf("ready --claim output = %q, must not claim a descendant of an abolished milestone", claimOut)
		}
	}

	if _, errS, code := runCmd(t, []string{"update", epic, "--remove-label", "abolished-by:lm-any"}, ""); code != 0 {
		t.Fatalf("update remove-label: exit code = %d, stderr = %q", code, errS)
	}
	out, errS, code = runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "AbolishedGrandchild") {
		t.Errorf("ready output = %q, want the grandchild back once the label is removed", out)
	}
}
