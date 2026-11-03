// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestDepRemoveParentChildRequireMilestone(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic", "--label", "milestone:x")
	plain := mustCreateBead(t, "--title", "plain")
	orphanChild := mustCreateBead(t, "--title", "orphan child", "--parent", plain)
	child := mustCreateBead(t, "--title", "child", "--parent", epic)
	subEpic := mustCreateBead(t, "--title", "sub epic", "--parent", epic, "--label", "milestone:z")
	exempt := mustCreateBead(t, "--title", "exempt child", "--parent", epic, "--label", "exempt:milestone")
	gate := mustCreateBead(t, "--title", "gate child", "--parent", epic, "--type", "gate", "--label", "kind:adjudicate")
	unchecked := mustCreateBead(t, "--title", "unchecked child", "--parent", epic)

	writeBeadsConfig(t, `{"create":{"require_milestone":true}}`)
	for _, extra := range [][]string{{"--dry-run"}, nil} {
		args := append([]string{"dep", "remove", child, epic, "--type", "parent-child"}, extra...)
		out, errS, code := runCmd(t, args, "")
		if code != 1 || !strings.Contains(errS, "外すと子が無所属になる") || !strings.Contains(errS, "exempt:milestone") {
			t.Fatalf("dep remove %v = (%q, %d), want exit 1 and the 無所属 guidance", extra, errS, code)
		}
		if strings.Contains(out, "Unlinked") || strings.Contains(out, "Dry run") {
			t.Errorf("rejected dep remove %v wrote output:\n%s", extra, out)
		}
	}

	for name, id := range map[string]string{"own milestone label": subEpic, "own exempt label": exempt, "gate": gate} {
		if _, errS, code := runCmd(t, []string{"dep", "remove", id, epic, "--type", "parent-child"}, ""); code != 0 {
			t.Errorf("dep remove %s child = (%q, %d), want exit 0", name, errS, code)
		}
	}
	if _, errS, code := runCmd(t, []string{"dep", "remove", orphanChild, plain, "--type", "parent-child"}, ""); code != 0 {
		t.Errorf("dep remove of an already unaffiliated child = (%q, %d), want exit 0", errS, code)
	}

	if _, errS, code := runCmd(t, []string{"update", child, "--add-label", "exempt:milestone", "--reason", "one-off"}, ""); code != 0 {
		t.Fatalf("update --add-label = (%q, %d)", errS, code)
	}
	if _, errS, code := runCmd(t, []string{"dep", "remove", child, epic, "--type", "parent-child"}, ""); code != 0 {
		t.Errorf("dep remove after exempt:milestone = (%q, %d), want exit 0 (the rejected remove must have left the link)", errS, code)
	}

	writeBeadsConfig(t, `{"create":{"require_milestone":false}}`)
	if _, errS, code := runCmd(t, []string{"dep", "remove", unchecked, epic, "--type", "parent-child"}, ""); code != 0 {
		t.Errorf("dep remove with require_milestone=false = (%q, %d), want exit 0", errS, code)
	}
}
