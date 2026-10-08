// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"slices"
	"strings"
	"testing"
)

func TestActorFlagHelpStatesResolutionOrder(t *testing.T) {
	initBeadsDir(t)
	const want = "(default: $LM_ACTOR, then $CLAUDE_CODE_SESSION_ID, then the OS user name)"

	var targets [][]string
	for _, c := range registry {
		if slices.Contains(c.flags, "actor") {
			targets = append(targets, []string{c.name})
		}
		for _, s := range c.sub {
			if slices.Contains(s.flags, "actor") {
				targets = append(targets, []string{c.name, s.name})
			}
		}
	}
	if len(targets) != 14 {
		t.Fatalf("commands with --actor = %d, want 14: %v", len(targets), targets)
	}
	for _, args := range targets {
		_, errS, code := runCmd(t, append(args, "-h"), "")
		if code != 0 {
			t.Errorf("lm %s -h: exit code = %d, stderr = %q", strings.Join(args, " "), code, errS)
			continue
		}
		if !strings.Contains(errS, "-actor string\n") || !strings.Contains(errS, want) {
			t.Errorf("lm %s -h: stderr = %q, want --actor help containing %q", strings.Join(args, " "), errS, want)
		}
	}
}
