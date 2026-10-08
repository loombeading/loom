// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func TestDecorate_StripRoundTrips(t *testing.T) {
	cases := []string{
		"",
		"(none)\n",
		"## Show bd-a3f8\n\n- Status: open\n",
		"- bd-a3f8 [open/P2] a title\n- bd-b9d4 [bug/in_progress/P1] another  @alice\n- bd-c001 [feature/closed/P3] done  ← blocked by: bd-x\n- bd-c002 [cancelled/P0] nope\n- bd-c003 [#119/P1] with pr\n- bd-c004 [bug/#119/P0] with pr and type\n- bd-c005 [closed/#871/P0] closed with pr\n- bd-c006 [epic/cancelled/#12/P1] cancelled with pr and type\n",
		"Filter: namespace=\"web\"\nTruncated: showing 2 of 3\nRejected: bd-a3f8 (not found)\nError: something broke\n",
		"- bd-zqfg.16 [open/P1] t\n",
		"- my-ns-a3f8 [open/P2] t\n",
		"  - bd-a3f8 [in_progress/P1] t\n",
		"- lm-zqhv [gate/open/P2] x  → blocks: lm-5t2n\n",
		"- Created: lm-4kpw [open/P2] t\n",
		"- blocks ← bd-4mv [closed/P1] t\n",
		"- bd-a3f8 [open/P1] t  ← blocked by: bd-x\n",
		"- bd-a3f8 [open/P1] t  gate=bd-zhpq\n",
		"- abc [open/P1] t\n",
		"- abc- [open/P1] t\n",
	}
	for _, s := range cases {
		got := stripANSI(Decorate(s))
		if got != s {
			t.Errorf("stripANSI(Decorate(%q)) = %q, want %q", s, got, s)
		}
	}
}

func TestDecorate_AddsANSIWhenApplicable(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"in_progress status", "- bd-a3f8 [in_progress/P1] t\n"},
		{"closed status", "- bd-a3f8 [closed/P1] t\n"},
		{"cancelled status", "- bd-a3f8 [cancelled/P1] t\n"},
		{"in_progress status with type prefix", "- bd-a3f8 [bug/in_progress/P1] t\n"},
		{"closed status with type prefix", "- bd-a3f8 [epic/closed/P1] t\n"},
		{"P0 priority", "- bd-a3f8 [open/P0] t\n"},
		{"P0 priority with PR number", "- bd-a3f8 [#119/P0] t\n"},
		{"PR number colored like in_progress", "- bd-a3f8 [#119/P1] t\n"},
		{"closed status with PR number", "- bd-a3f8 [closed/#871/P0] t\n"},
		{"cancelled status with PR number and type prefix", "- bd-a3f8 [epic/cancelled/#12/P1] t\n"},
		{"filter line", "Filter: namespace=\"web\"\n"},
		{"truncated line", "Truncated: showing 1 of 2\n"},
		{"truncated (more matches exist) line", "Truncated: showing 1 (more matches exist)\n"},
		{"rejected line", "Rejected: bd-a3f8 (not found)\n"},
		{"error line", "Error: boom\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decorate(c.in)
			if got == c.in {
				t.Errorf("Decorate(%q) = %q, want it to contain ANSI escapes", c.in, got)
			}
			if stripANSI(got) != c.in {
				t.Errorf("stripANSI(Decorate(%q)) = %q, want %q", c.in, stripANSI(got), c.in)
			}
		})
	}
}

func TestDecorate_HeadingUndecorated(t *testing.T) {
	cases := []string{
		"## Show bd-a3f8\n",
		"## Gate waiting\n",
		"## Description\n",
		"## Dependencies\n",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if got := Decorate(in); got != in {
				t.Errorf("Decorate(%q) = %q, want unchanged (headings are undecorated)", in, got)
			}
		})
	}

	in := "## Gate waiting\n- bd-zqfg.16 [open/P1] g  gate=bd-zhpq\n"
	want := "## Gate waiting\n- bd-" + ansiBold + "zqfg.16" + ansiReset + " [open/P1] g  gate=bd-zhpq\n"
	if got := Decorate(in); got != want {
		t.Errorf("Decorate(%q) = %q, want %q", in, got, want)
	}
}

func TestDecorate_SubHeadingUndecoratedRowStillDecorates(t *testing.T) {
	in := "## Gate waiting\n\n### Needs you\n- bd-zqfg.16 [open/P1] g  gate=bd-zhpq\n"
	want := "## Gate waiting\n\n### Needs you\n- bd-" + ansiBold + "zqfg.16" + ansiReset + " [open/P1] g  gate=bd-zhpq\n"
	if got := Decorate(in); got != want {
		t.Errorf("Decorate(%q) = %q, want %q", in, got, want)
	}
}

func TestDecorate_OpenStatusUndecorated(t *testing.T) {
	in := "- bd-a3f8 [open/P1] t\n"
	want := "- bd-" + ansiBold + "a3f8" + ansiReset + " [open/P1] t\n"
	if got := Decorate(in); got != want {
		t.Errorf("Decorate(%q) = %q, want %q (open is undecorated aside from ID bold)", in, got, want)
	}
}

func TestDecorate_OpenStatusWithTypePrefixUndecorated(t *testing.T) {
	in := "- bd-a3f8 [gate/open/P1] t\n"
	want := "- bd-" + ansiBold + "a3f8" + ansiReset + " [gate/open/P1] t\n"
	if got := Decorate(in); got != want {
		t.Errorf("Decorate(%q) = %q, want %q (open is undecorated aside from ID bold)", in, got, want)
	}
}

func TestDecorate_PRNumberSameColorAsInProgress(t *testing.T) {
	in := "- bd-a3f8 [#119/P1] t\n"
	got := Decorate(in)
	if want := ansiYellow + "#119" + ansiReset; !strings.Contains(got, want) {
		t.Errorf("Decorate(%q) = %q, want it to contain %q", in, got, want)
	}
	if stripANSI(got) != in {
		t.Errorf("stripANSI(Decorate(%q)) = %q, want %q", in, stripANSI(got), in)
	}
	withType := "- bd-a3f8 [bug/#119/P0] t\n"
	if got := Decorate(withType); !strings.Contains(got, ansiYellow+"#119"+ansiReset) || !strings.Contains(got, ansiRed+ansiBold+"P0") {
		t.Errorf("Decorate(%q) = %q, want #119 yellow and P0 red", withType, got)
	}
}

func TestDecorate_ClosedStatusWithPRNumberColorsBoth(t *testing.T) {
	in := "- bd-a3f8 [closed/#871/P0] t\n"
	got := Decorate(in)
	if want := ansiDim + "closed" + ansiReset; !strings.Contains(got, want) {
		t.Errorf("Decorate(%q) = %q, want it to contain %q", in, got, want)
	}
	if want := ansiYellow + "#871" + ansiReset; !strings.Contains(got, want) {
		t.Errorf("Decorate(%q) = %q, want it to contain %q", in, got, want)
	}
	if !strings.Contains(got, ansiRed+ansiBold+"P0") {
		t.Errorf("Decorate(%q) = %q, want P0 red", in, got)
	}
	if stripANSI(got) != in {
		t.Errorf("stripANSI(Decorate(%q)) = %q, want %q", in, stripANSI(got), in)
	}

	withType := "- bd-a3f8 [epic/cancelled/#12/P1] t\n"
	got = Decorate(withType)
	if want := ansiDim + "cancelled" + ansiReset; !strings.Contains(got, want) {
		t.Errorf("Decorate(%q) = %q, want it to contain %q", withType, got, want)
	}
	if want := ansiYellow + "#12" + ansiReset; !strings.Contains(got, want) {
		t.Errorf("Decorate(%q) = %q, want it to contain %q", withType, got, want)
	}
	if stripANSI(got) != withType {
		t.Errorf("stripANSI(Decorate(%q)) = %q, want %q", withType, stripANSI(got), withType)
	}
}

func TestDecorate_NonZeroPriorityUndecorated(t *testing.T) {
	in := "- bd-a3f8 [open/P1] t\n"
	want := "- bd-" + ansiBold + "a3f8" + ansiReset + " [open/P1] t\n"
	if got := Decorate(in); got != want {
		t.Errorf("Decorate(%q) = %q, want %q (only P0 gets priority color; ID bold still applies)", in, got, want)
	}
}

func TestDecorate_PlainRowUndecorated(t *testing.T) {
	in := "(none)\n"
	if got := Decorate(in); got != in {
		t.Errorf("Decorate(%q) = %q, want unchanged", in, got)
	}
}

func TestDecorate_BoldsLeadingID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"exact match: namespace + short id",
			"- bd-zqfg.16 [open/P1] t\n",
			"- bd-" + ansiBold + "zqfg.16" + ansiReset + " [open/P1] t\n",
		},
		{
			"namespace containing a dash",
			"- my-ns-a3f8 [open/P2] t\n",
			"- my-ns-" + ansiBold + "a3f8" + ansiReset + " [open/P2] t\n",
		},
		{
			"nested/indented line coexists with status color",
			"  - bd-a3f8 [in_progress/P1] t\n",
			"  - bd-" + ansiBold + "a3f8" + ansiReset + " [" + ansiYellow + "in_progress" + ansiReset + "/P1] t\n",
		},
		{
			"gate line: only the leading id is bolded, not the trailing blocks id",
			"- lm-zqhv [gate/open/P2] x  → blocks: lm-5t2n\n",
			"- lm-" + ansiBold + "zqhv" + ansiReset + " [gate/open/P2] x  → blocks: lm-5t2n\n",
		},
		{
			"Created line",
			"- Created: lm-4kpw [open/P2] t\n",
			"- Created: lm-" + ansiBold + "4kpw" + ansiReset + " [open/P2] t\n",
		},
		{
			"Links line: bold and dim coexist",
			"- blocks ← bd-4mv [closed/P1] t\n",
			"- blocks ← bd-" + ansiBold + "4mv" + ansiReset + " [" + ansiDim + "closed" + ansiReset + "/P1] t\n",
		},
		{
			"blocked-by suffix stays undecorated",
			"- bd-a3f8 [open/P1] t  ← blocked by: bd-x\n",
			"- bd-" + ansiBold + "a3f8" + ansiReset + " [open/P1] t  ← blocked by: bd-x\n",
		},
		{
			"gate= suffix stays undecorated",
			"- bd-a3f8 [open/P1] t  gate=bd-zhpq\n",
			"- bd-" + ansiBold + "a3f8" + ansiReset + " [open/P1] t  gate=bd-zhpq\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Decorate(c.in); got != c.want {
				t.Errorf("Decorate(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestDecorate_DoesNotBoldWithoutDash(t *testing.T) {
	in := "- abc [open/P1] t\n"
	if got := Decorate(in); got != in {
		t.Errorf("Decorate(%q) = %q, want unchanged (id has no '-')", in, got)
	}
}

func TestDecorate_DoesNotBoldWhenDashIsTrailing(t *testing.T) {
	in := "- abc- [open/P1] t\n"
	if got := Decorate(in); got != in {
		t.Errorf("Decorate(%q) = %q, want unchanged (id's '-' is trailing)", in, got)
	}
}
