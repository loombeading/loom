// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestUsageErrorsNameLmBinary(t *testing.T) {
	initBeadsDir(t)

	cases := []struct {
		name       string
		args       []string
		wantErrMsg string
		wantSeeMsg string
	}{
		{
			name:       "close with no IDs",
			args:       []string{"close"},
			wantErrMsg: "Error: lm close requires at least one ID",
		},
		{
			name:       "completion with no shell",
			args:       []string{"completion"},
			wantErrMsg: "Error: lm completion requires exactly one shell (zsh, bash)",
		},
		{
			name:       "cost add with no ID",
			args:       []string{"cost", "add"},
			wantErrMsg: "Error: lm cost add requires exactly one ID",
			wantSeeMsg: "See: lm help cost add",
		},
		{
			name:       "dep add with wrong arg count",
			args:       []string{"dep", "add", "only-one-id"},
			wantErrMsg: "Error: lm dep add requires <ID> <depends-on-ID>",
			wantSeeMsg: "See: lm help dep add",
		},
		{
			name:       "gate resolve with no ID",
			args:       []string{"gate", "resolve"},
			wantErrMsg: "Error: lm gate resolve requires at least one ID",
			wantSeeMsg: "See: lm help gate resolve",
		},
		{
			name:       "import with no path",
			args:       []string{"import"},
			wantErrMsg: `Error: lm import requires exactly one <path> (or "-" for stdin)`,
		},
		{
			name:       "rework add with no ID",
			args:       []string{"rework", "add"},
			wantErrMsg: "Error: lm rework add requires exactly one ID",
			wantSeeMsg: "See: lm help rework add",
		},
		{
			name:       "show with no ID",
			args:       []string{"show"},
			wantErrMsg: "Error: lm show requires exactly one ID",
			wantSeeMsg: "See: lm help show",
		},
		{
			name:       "update with no IDs",
			args:       []string{"update"},
			wantErrMsg: "Error: lm update requires at least one ID",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errS, code := runCmd(t, c.args, "")
			if code != 1 {
				t.Fatalf("%v: exit code = %d, want 1 (stdout=%q stderr=%q)", c.args, code, out, errS)
			}
			if !strings.Contains(errS, c.wantErrMsg) {
				t.Errorf("%v: stderr = %q, want it to contain %q", c.args, errS, c.wantErrMsg)
			}
			if c.wantSeeMsg != "" && !strings.Contains(errS, c.wantSeeMsg) {
				t.Errorf("%v: stderr = %q, want it to contain %q", c.args, errS, c.wantSeeMsg)
			}
		})
	}
}
