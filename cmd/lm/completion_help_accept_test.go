// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestCompletionHelpAccept(t *testing.T) {
	cases := [][]string{
		{"help", "completion"},
		{"help", "completion", "zsh"},
		{"completion", "--help"},
		{"completion", "-h"},
		{"completion", "zsh", "--help"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, errS, code := runCmd(t, args, "")
			if code != 0 {
				t.Fatalf("lm %s: exit code = %d, want 0; stderr = %q", strings.Join(args, " "), code, errS)
			}
			if !strings.Contains(errS, "Usage: lm completion") {
				t.Errorf("lm %s: stderr = %q, want a line starting with \"Usage: lm completion\"", strings.Join(args, " "), errS)
			}
			if strings.Contains(out, "#compdef") || strings.Contains(out, "complete -F") {
				t.Errorf("lm %s: stdout has a completion script, want help only:\n%s", strings.Join(args, " "), out)
			}
		})
	}
}
