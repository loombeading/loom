// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestMilestoneOrderInversionRejectedByCLI(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "m", "--label", "milestone:x", "--priority", "2")
	child := mustCreateBead(t, "--title", "c", "--parent", epic, "--priority", "2")
	loose := mustCreateBead(t, "--title", "l", "--priority", "1")

	for _, args := range [][]string{
		{"update", child, "--priority", "1"},
		{"create", "--title", "x", "--parent", epic, "--priority", "1"},
		{"dep", "add", loose, epic, "--type", "parent-child"},
	} {
		out, errS, code := runCmd(t, args, "")
		if code != 1 {
			t.Fatalf("%v: exit code = %d, want 1 (stdout %q)", args, code, out)
		}
		if msg := out + errS; !strings.Contains(msg, "P2 より高い") || !strings.Contains(msg, "--priority 2 以上") {
			t.Fatalf("%v: stdout %q stderr %q, want the milestone priority and the allowed range", args, out, errS)
		}
	}
}
