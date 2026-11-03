// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestCreateRejectedLinkAbortsBeadCreation(t *testing.T) {
	initBeadsDir(t)

	epic := mustCreateBead(t, "--title", "m", "--label", "milestone:x", "--priority", "2")
	blocker := mustCreateBead(t, "--title", "b")

	before := mustExport(t)
	_, errS, code := runCmd(t, []string{"create", "--title", "x", "--parent", epic, "--blocked-by", blocker, "--priority", "1"}, "")
	if code != 1 {
		t.Fatalf("create with a priority inversion: exit code = %d, want 1, stderr = %q", code, errS)
	}
	if got := mustExport(t); got != before {
		t.Errorf("rejected create left data behind:\nbefore:\n%s\nafter:\n%s", before, got)
	}

	parent := mustCreateBead(t, "--title", "p")
	if _, errS, code := runCmd(t, []string{"close", parent, "--reason", "d"}, ""); code != 0 {
		t.Fatalf("close parent: exit code = %d, stderr = %q", code, errS)
	}
	before = mustExport(t)
	_, errS, code = runCmd(t, []string{"create", "--title", "y", "--parent", parent, "--blocked-by", blocker}, "")
	if code != 1 {
		t.Fatalf("create under a closed parent: exit code = %d, want 1, stderr = %q", code, errS)
	}
	if got := mustExport(t); got != before {
		t.Errorf("rejected create under a closed parent left data behind:\nbefore:\n%s\nafter:\n%s", before, got)
	}
}

func TestExternalRefsKeepInsertionOrderAndAreIdempotent(t *testing.T) {
	initBeadsDir(t)

	id := mustCreateBead(t, "--title", "t",
		"--external-ref", "https://x/9",
		"--external-ref", "https://x/1",
		"--external-ref", "https://x/9")
	show, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show after create: exit code = %d", code)
	}
	if !strings.Contains(show, `external_refs: ["https://x/9", "https://x/1"]`) {
		t.Errorf("create external_refs not in insertion order without duplicates:\n%s", show)
	}

	if _, errS, code := runCmd(t, []string{"update", "--external-ref", "https://x/1", "--external-ref", "https://x/5", id}, ""); code != 0 {
		t.Fatalf("update --external-ref: exit code = %d, stderr = %q", code, errS)
	}
	show, _, code = runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show after update: exit code = %d", code)
	}
	if !strings.Contains(show, `external_refs: ["https://x/9", "https://x/1", "https://x/5"]`) {
		t.Errorf("update external_refs did not append only the new value in order:\n%s", show)
	}
}

func TestUpdatePartialRejectionWritesNothing(t *testing.T) {
	initBeadsDir(t)

	id := mustCreateBead(t, "--title", "t")
	before := mustExport(t)

	for _, args := range [][]string{
		{"update", id, "--partial", "leftover"},
		{"update", id, "--external-ref", "https://x/7", "--partial", ""},
	} {
		if _, _, code := runCmd(t, args, ""); code == 0 {
			t.Fatalf("%v: exit code = 0, want non-zero", args)
		}
		if got := mustExport(t); got != before {
			t.Errorf("%v wrote data despite the rejection:\nbefore:\n%s\nafter:\n%s", args, before, got)
		}
	}
}

func TestUpdatePartialCopiesTypeAndPriorityWithoutParent(t *testing.T) {
	initBeadsDir(t)

	src := mustCreateBead(t, "--title", "g", "--type", "gate", "--label", "kind:external", "--priority", "3")
	out, errS, code := runCmd(t, []string{"update", src, "--external-ref", "https://x/8", "--partial", "rest"}, "")
	if code != 0 {
		t.Fatalf("update --partial: exit code = %d, stderr = %q", code, errS)
	}
	var partialID string
	for line := range strings.SplitSeq(out, "\n") {
		if after, ok := strings.CutPrefix(line, "- Created: "); ok {
			partialID = mustCanonicalID(t, strings.SplitN(after, " ", 2)[0])
		}
	}
	if partialID == "" {
		t.Fatalf("update --partial output missing a Created line:\n%s", out)
	}

	show, _, code := runCmd(t, []string{"show", partialID}, "")
	if code != 0 {
		t.Fatalf("show partial: exit code = %d", code)
	}
	srcAlias := mustDisplayAlias(t, src)
	for _, want := range []string{
		`type: "gate"`,
		`priority: 3`,
		`title: "rest"`,
		"blocks ← " + srcAlias,
		"discovered-from ← " + srcAlias,
	} {
		if !strings.Contains(show, want) {
			t.Errorf("partial show missing %q:\n%s", want, show)
		}
	}
	if strings.Contains(show, "parent-child ←") {
		t.Errorf("partial of a parentless source got a parent:\n%s", show)
	}
}
