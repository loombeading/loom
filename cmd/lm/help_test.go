// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestHelpGoldenOutput(t *testing.T) {
	out, errS, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("help: exit code = %d, stderr = %q", code, errS)
	}
	if errS != "" {
		t.Fatalf("help: stderr = %q, want empty", errS)
	}

	//nolint:dupword
	want := `# lm

lm is the Loom CLI: a task and memory tracker for AI coding agents. Output is Markdown only; every command is non-interactive.

Usage: lm <command> [<subcommand>] [flags] [<args>]

## Commands
- version — Print the lm binary version
- init — Initialize the Loom database in LM_DIR or $XDG_DATA_HOME/loom
- where — Show the resolved database location, config, and schema version
- create — Create a new Bead
- show — Show a single Bead's attributes, links, and audit history
- list — Enumerate Beads, 1 Bead 1 line
- search — Search title/description/summary/external_refs/audit-log reasons for a substring
- update — Update a Bead's status, attributes, and labels
- close — Close one or more Beads
- dep add — Add a dependency link: <ID> depends on <depends-on-ID> (blocks: ID is blocked by it; parent-child: ID is the child)
- dep remove — Remove (mark removed) a dependency link between two Beads
- gate — List open Gates and the Beads they block
- gate create — Create a Gate Bead that blocks other Beads until resolved
- gate resolve — Resolve one or more Gates, unblocking the Beads they block
- gate reject — Reject one or more Gates: cancel the open/in_progress Beads each blocks, then cancel the Gate itself
- gate sweep — Auto-resolve open Gates whose wait is already met (the Beads they block are all terminal)
- ahead — Estimate token cost of Ready Beads and the courses that follow, against an optional budget
- cost add — Add an input/output token cost record to a Bead
- rework add — Add a rework record to a Bead
- ready — Enumerate Beads that are ready to start
- blocked — Enumerate open Beads that are not ready, with their blockers
- check gate-cycles — List cycles closed by open Gates' wait edges and wait targets no open Bead owns
- export — Dump every Bead/dependency/token-cost row as JSONL
- import — Merge a JSONL export into the database
- migrate — Advance the database's schema to the version this build expects
- help — Show this help or a command's usage
- completion zsh|bash — Print a shell completion script

## Environment
- LM_DIR — absolute path to the loom directory; no parent-directory search
- LM_NAMESPACE — default namespace filter for enumeration commands
- LM_ACTOR — default acting user for the audit log
- LM_READONLY — any value other than ""/0/false/no opens the database read-only
- LM_NOW — override the current instant for acceptance tests and diagnostics
- NO_COLOR — any value suppresses ANSI decoration
- LM_COLOR — "always" forces ANSI decoration unless NO_COLOR is also set

See: lm help <command>
`
	if out != want {
		t.Errorf("help output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestHelpFlagsMatchHelpCommand(t *testing.T) {
	want, _, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("help: exit code = %d", code)
	}

	for _, args := range [][]string{{"--help"}, {"-h"}} {
		out, errS, code := runCmd(t, args, "")
		if code != 0 {
			t.Errorf("%v: exit code = %d, stderr = %q", args, code, errS)
		}
		if out != want {
			t.Errorf("%v output = %q, want %q (same as `lm help`)", args, out, want)
		}
	}
}

func TestNoArgsWritesHelpToStderrExit1(t *testing.T) {
	wantOut, _, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("help: exit code = %d", code)
	}

	out, errS, code := runCmd(t, []string{}, "")
	if code != 1 {
		t.Errorf("lm (no args): exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("lm (no args): stdout = %q, want empty", out)
	}
	if errS != wantOut {
		t.Errorf("lm (no args): stderr = %q, want %q (same content as `lm help`)", errS, wantOut)
	}
}

func TestHelpNoDBAccessWithoutBDDir(t *testing.T) {
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)

	out, errS, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("help: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, "# lm\n") {
		t.Errorf("help output = %q, want it to start with \"# lm\\n\"", out)
	}
}

func TestUnknownSubcommandSeesHelp(t *testing.T) {
	out, errS, code := runCmd(t, []string{"bogus"}, "")
	if code != 1 {
		t.Errorf("lm bogus: exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("lm bogus: stdout = %q, want empty", out)
	}
	if !strings.Contains(errS, "unknown subcommand") {
		t.Errorf("lm bogus: stderr = %q, want it to mention \"unknown subcommand\"", errS)
	}
	if !strings.Contains(errS, "See: lm help") {
		t.Errorf("lm bogus: stderr = %q, want it to contain \"See: lm help\"", errS)
	}
}

func TestUnknownSubSubcommandSeesHelp(t *testing.T) {
	_, errS, code := runCmd(t, []string{"gate", "bogus"}, "")
	if code != 1 {
		t.Errorf("lm gate bogus: exit code = %d, want 1", code)
	}
	if !strings.Contains(errS, "See: lm help") {
		t.Errorf("lm gate bogus: stderr = %q, want it to contain \"See: lm help\"", errS)
	}
}

func TestHelpCommandExitZero(t *testing.T) {
	cases := [][]string{
		{"create"},
		{"export"},
		{"gate", "create"},
	}
	for _, args := range cases {
		helpArgs := append([]string{"help"}, args...)
		out, errS, code := runCmd(t, helpArgs, "")
		if code != 0 {
			t.Errorf("lm help %v: exit code = %d, out=%q, stderr = %q", args, code, out, errS)
		}
		wantPrefix := "Usage: lm " + strings.Join(args, " ") + " [flags] <args>\n"
		if !strings.HasPrefix(errS, wantPrefix) {
			t.Errorf("lm help %v: stderr = %q, want it to start with %q", args, errS, wantPrefix)
		}
	}
}

func TestCompletionZshBashSyntax(t *testing.T) {
	cases := []struct {
		shell   string
		checker string
	}{
		{"zsh", "zsh"},
		{"bash", "bash"},
	}
	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			out, errS, code := runCmd(t, []string{"completion", c.shell}, "")
			if code != 0 {
				t.Fatalf("completion %s: exit code = %d, stderr = %q", c.shell, code, errS)
			}
			if out == "" {
				t.Fatalf("completion %s: empty output", c.shell)
			}

			checkerPath, err := exec.LookPath(c.checker)
			if err != nil {
				t.Skipf("%s not found in PATH, skipping syntax check", c.checker)
			}
			cmd := exec.CommandContext(t.Context(), checkerPath, "-n", "/dev/stdin")
			cmd.Stdin = bytes.NewBufferString(out)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("%s -n reported a syntax error: %v\n%s", c.checker, err, stderr.String())
			}
		})
	}
}

func TestMilestoneAbsentFromHelpAndCompletion(t *testing.T) {
	out, _, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("help: exit code = %d", code)
	}
	if strings.Contains(strings.ToLower(out), "milestone") {
		t.Errorf("lm help output contains \"milestone\":\n%s", out)
	}

	for _, shell := range []string{"zsh", "bash"} {
		out, _, code := runCmd(t, []string{"completion", shell}, "")
		if code != 0 {
			t.Fatalf("completion %s: exit code = %d", shell, code)
		}
		if strings.Contains(strings.ToLower(out), "milestone") {
			t.Errorf("lm completion %s output contains \"milestone\"", shell)
		}
	}
}

func TestCompletionIncludesPriorityShorthand(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		out, _, code := runCmd(t, []string{"completion", shell}, "")
		if code != 0 {
			t.Fatalf("completion %s: exit code = %d", shell, code)
		}
		if !strings.Contains(out, "-p") {
			t.Errorf("lm completion %s output missing \"-p\" (create/update shorthand for --priority):\n%s", shell, out)
		}
	}
}

func TestCompletionUnknownShell(t *testing.T) {
	out, errS, code := runCmd(t, []string{"completion", "fish"}, "")
	if code != 1 {
		t.Errorf("completion fish: exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("completion fish: stdout = %q, want empty", out)
	}
	if errS == "" {
		t.Errorf("completion fish: stderr empty, want an error message")
	}
}

func TestHelpUndecorated(t *testing.T) {
	t.Setenv("LM_COLOR", "always")
	out, _, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("help: exit code = %d", code)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("help output = %q, want no ANSI escapes even with LM_COLOR=always", out)
	}
}
