// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunDoesNotDecorateNonTTYStdout(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--description", "d")

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("show stdout = %q, want no ANSI escapes on a non-TTY writer", out)
	}
}

func TestRunBDColorAlwaysDecoratesNonTTY(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "t", "--description", "d")

	t.Setenv("LM_COLOR", "always")
	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("list stdout = %q, want ANSI escapes with LM_COLOR=always", out)
	}
	if got := stripANSI(out); strings.Contains(got, "\x1b[") {
		t.Errorf("stripANSI left escapes behind: %q", got)
	}
}

func TestRunNoColorSuppressesEvenWithBDColorAlways(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--description", "d")

	t.Setenv("LM_COLOR", "always")
	t.Setenv("NO_COLOR", "1")
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("show stdout = %q, want no ANSI escapes with NO_COLOR set", out)
	}
}

func TestRunDecorationPreservesPlainContent(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t1", "--description", "d")
	mustCreateBead(t, "--title", "t2", "--description", "d")
	if _, errS, code := runCmd(t, []string{"update", id, "--claim"}, ""); code != 0 {
		t.Fatalf("update --claim: exit code = %d, stderr = %q", code, errS)
	}

	plain, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_COLOR", "always")
	decorated, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	if stripANSI(decorated) != plain {
		t.Errorf("stripANSI(decorated) = %q, want %q (plain)", stripANSI(decorated), plain)
	}
	if decorated == plain {
		t.Errorf("decorated output equals plain output, want ANSI escapes to be present")
	}
}

func TestRunErrorOutputDecoratedSameRule(t *testing.T) {
	initBeadsDir(t)

	t.Setenv("LM_COLOR", "always")
	var stdout, stderr bytes.Buffer
	code := run([]string{"show", "bd-doesnotexist"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("show nonexistent: exit code = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "\x1b[") {
		t.Errorf("stderr = %q, want ANSI escapes with LM_COLOR=always", stderr.String())
	}
}

func TestRunLMColorAlwaysGolden(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "alpha")
	blocked := mustCreateBead(t, "--title", "beta", "--blocked-by", blocker)
	gate := mustGateCreateCmd(t, []string{blocked}, "sign-off", "--kind", "human")
	if _, errS, code := runCmd(t, []string{"update", "--claim", blocker}, ""); code != 0 {
		t.Fatalf("update --claim: exit code = %d, stderr = %q", code, errS)
	}
	blockerAlias := mustDisplayAlias(t, blocker)
	blockedAlias := mustDisplayAlias(t, blocked)
	gateAlias := mustDisplayAlias(t, gate)
	boldID := func(alias string) string {
		i := strings.LastIndex(alias, "-")
		return alias[:i+1] + "\x1b[1m" + alias[i+1:] + "\x1b[0m"
	}

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			"list",
			[]string{"list", "--sort", "priority"},
			"\x1b[36mFilter: status=\"unfinished\"\x1b[0m\n" +
				"- " + boldID(blockerAlias) + " [\x1b[33min_progress\x1b[0m/P2] alpha  @test-actor\n" +
				"- " + boldID(blockedAlias) + " [open/P2] beta\n" +
				"- " + boldID(gateAlias) + " [gate/open/P2] gate: sign-off\n",
		},
		{
			"blocked",
			[]string{"blocked"},
			"- " + boldID(blockedAlias) + " [open/P2] beta  ← blocked by: " + blockerAlias + ", gate:" + gateAlias + "\n",
		},
		{
			"gate",
			[]string{"gate"},
			"## Blocked by other work\n" +
				"- " + boldID(gateAlias) + " [gate/open/P2] sign-off  → blocks: " + blockedAlias + "\n" +
				"  - When resolved: (--check に記載なし)\n" +
				"  - Blocked by: " + blockerAlias + "\n",
		},
	}
	t.Setenv("LM_COLOR", "always")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errS, code := runCmd(t, c.args, "")
			if code != 0 {
				t.Fatalf("%v: exit code = %d, stderr = %q", c.args, code, errS)
			}
			if out != c.want {
				t.Errorf("%v stdout = %q, want %q", c.args, out, c.want)
			}
		})
		t.Run(c.name+" with NO_COLOR", func(t *testing.T) {
			t.Setenv("NO_COLOR", "1")
			out, errS, code := runCmd(t, c.args, "")
			if code != 0 {
				t.Fatalf("%v: exit code = %d, stderr = %q", c.args, code, errS)
			}
			if strings.Contains(out, "\x1b[") {
				t.Errorf("%v stdout = %q, want no ANSI escapes with NO_COLOR set", c.args, out)
			}
		})
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
