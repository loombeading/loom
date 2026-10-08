// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/loombeading/loom/internal/output"
)

type cmdHandler func(args []string, stdin io.Reader, stdout, stderr io.Writer) int

type subEntry struct {
	name  string
	desc  string
	flags []string
}

type cmdEntry struct {
	name  string
	desc  string
	flags []string
	sub   []subEntry
	bare  string
	run   cmdHandler
}

func groupBare(name string) string {
	for _, c := range registry {
		if c.name == name {
			return c.bare
		}
	}
	return ""
}

func withStdin(f func(args []string, stdout, stderr io.Writer) int) cmdHandler {
	return func(args []string, _ io.Reader, stdout, stderr io.Writer) int {
		return f(args, stdout, stderr)
	}
}

type groupSub struct {
	name string
	run  cmdHandler
}

func runGroupDispatch(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, subs []groupSub, bareRun func(args []string, stdout, stderr io.Writer) int, requireMsg string) int {
	if len(args) > 0 && (args[0] == helpFlag || args[0] == "-h") {
		writeGroupHelp(stderr, name)
		return 0
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		if groupBare(name) != "" {
			return bareRun(args, stdout, stderr)
		}
		output.WriteError(stderr, requireMsg)
		return 1
	}
	for _, s := range subs {
		if s.name == args[0] {
			return s.run(args[1:], stdin, stdout, stderr)
		}
	}
	output.WriteError(stderr, fmt.Sprintf("unknown lm %s subcommand %q", name, args[0]))
	fmt.Fprintln(stderr, "See: lm help")
	return 1
}

var registry []cmdEntry

func init() { registry = buildRegistry() }

func buildRegistry() []cmdEntry {
	return []cmdEntry{
		{name: "version", desc: "Print the lm binary version", run: withStdin(runVersion)},
		{name: "init", desc: "Initialize the Loom database in LM_DIR or $XDG_DATA_HOME/loom",
			run: withStdin(runInit)},
		{name: "where", desc: "Show the resolved database location, config, and schema version",
			run: withStdin(runWhere)},
		{name: "create", desc: "Create a new Bead",
			flags: []string{"title", flagType, flagPriority, "p", flagLabel, "description", flagSummary, flagParent,
				"blocked-by", flagNamespace, "external-ref", "reasoning-depth",
				flagReason, flagActor},
			run: runCreate},
		{name: "show", desc: "Show a single Bead's attributes, links, and audit history",
			flags: []string{"all-deps", "full"}, run: withStdin(runShow)},
		{name: "list", desc: "Enumerate Beads, 1 Bead 1 line",
			flags: []string{flagAll, flagStatus, flagType, flagPriority, "claimed-by", flagLabel, flagNamespace, flagLimit, "sort",
				flagParent, flagUpdatedBefore, flagUpdatedAfter, "claim-expired"},
			run: withStdin(runList)},
		{name: "search", desc: "Search title/description/summary/external_refs/audit-log reasons for a substring",
			flags: []string{flagAll, flagNamespace, flagLimit, flagParent, flagUpdatedBefore, flagUpdatedAfter},
			run:   withStdin(runSearch)},
		{name: "update", desc: "Update a Bead's status, attributes, and labels",
			flags: []string{"claim", "release", "force", "reopen", flagStatus, "title", "description",
				flagSummary, flagPriority, "p", flagType, flagNamespace, "external-ref", "remove-external-ref", "reasoning-depth",
				flagReason, flagActor, "add-label", "remove-label", "clear", "claim-ttl"},
			run: runUpdate},
		{name: "close", desc: "Close one or more Beads",
			flags: []string{flagReason, flagActor, flagSummary}, run: runClose},
		{name: "dep", desc: "Manage dependency links between Beads",
			sub: []subEntry{
				{name: cmdAdd, desc: "Add a dependency link: <ID> depends on <depends-on-ID> (blocks: ID is blocked by it; parent-child: ID is the child)",
					flags: []string{flagType, flagReason, flagActor, "allow-priority-change", "dry-run"}},
				{name: "remove", desc: "Remove (mark removed) a dependency link between two Beads",
					flags: []string{flagType, flagReason, flagActor, "allow-priority-change", "dry-run"}},
			}, run: runDep},
		{name: cmdGate, desc: "Manage Gates, the asynchronous wait primitive",
			sub: []subEntry{
				{name: "create", desc: "Create a Gate Bead that blocks other Beads until resolved",
					flags: []string{"subject", "current", "proposal", "check", "resolver", "kind", flagReason, flagNamespace, flagActor, "blocks"}},
				{name: "resolve", desc: "Resolve one or more Gates, unblocking the Beads they block",
					flags: []string{flagReason, flagActor}},
				{name: "reject", desc: "Reject one or more Gates: cancel the open/in_progress Beads each blocks, then cancel the Gate itself",
					flags: []string{flagReason, flagActor}},
				{name: "sweep", desc: "Auto-resolve open Gates whose wait is already met (the Beads they block are all terminal)",
					flags: []string{flagActor}},
			}, flags: []string{flagAll, flagNamespace}, bare: "List open Gates and the Beads they block", run: runGate},
		{name: "ahead", desc: "Estimate token cost of Ready Beads and the courses that follow, against an optional budget",
			flags: []string{"budget", "calibration", flagNamespace, flagAll}, run: withStdin(runAhead)},
		{name: "cost", desc: "Record token cost consumed working a Bead",
			sub: []subEntry{
				{name: cmdAdd, desc: "Add an input/output token cost record to a Bead",
					flags: []string{"in", "out", flagReason, flagActor}},
			}, run: runCost},
		{name: "rework", desc: "Record that work on a Bead was bounced back (CI failure, review, follow-up commit)",
			sub: []subEntry{
				{name: cmdAdd, desc: "Add a rework record to a Bead",
					flags: []string{"cause", flagReason, flagActor}},
			}, run: runRework},
		{name: cmdReady, desc: "Enumerate Beads that are ready to start",
			flags: []string{flagType, flagPriority, "claimed-by", flagLabel, flagNamespace, flagLimit, "claim", flagActor,
				flagParent, flagUpdatedBefore, flagUpdatedAfter},
			run: withStdin(runReady)},
		{name: "blocked", desc: "Enumerate open Beads that are not ready, with their blockers",
			flags: []string{flagType, flagPriority, "claimed-by", flagLabel, flagNamespace, flagLimit, flagParent,
				flagUpdatedBefore, flagUpdatedAfter},
			run: withStdin(runBlocked)},
		{name: "check", desc: "Read-only checks over the stored data (exit 1 on violations, 2 when the database cannot be read)",
			sub: []subEntry{
				{name: "gate-cycles", desc: "List cycles closed by open Gates' wait edges and wait targets no open Bead owns"},
			}, run: runCheck},
		{name: "export", desc: "Dump every Bead/dependency/token-cost row as JSONL",
			run: withStdin(runExport)},
		{name: "import", desc: "Merge a JSONL export into the database",
			flags: []string{"dry-run", flagActor}, run: runImport},
		{name: "migrate", desc: "Advance the database's schema to the version this build expects",
			flags: []string{flagActor}, run: withStdin(runMigrate)},
	}
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == helpFlag) {
		fmt.Fprintln(stderr, "Usage: lm version [flags] <args>")
		fmt.Fprintln(stderr, commandDesc("version"))
		return 0
	}
	commit, date := resolveVCS()
	fmt.Fprintf(stdout, "lm %s (%s %s)\n", resolveVersion(), commit, date)
	return 0
}

func helpExitCode(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 1
}

func helpUsage(fs *flag.FlagSet) func() {
	return func() {
		w := fs.Output()
		fmt.Fprintf(w, "Usage: lm %s [flags] <args>\n", fs.Name())
		if desc := commandDesc(fs.Name()); desc != "" {
			fmt.Fprintln(w, desc)
		}
		fmt.Fprintln(w)
		fs.PrintDefaults()
	}
}

func writeGroupHelp(w io.Writer, name string) {
	for _, c := range registry {
		if c.name != name {
			continue
		}
		if c.bare != "" {
			fmt.Fprintf(w, "Usage: lm %s [<subcommand>] [flags] <args>\n", name)
		} else {
			fmt.Fprintf(w, "Usage: lm %s <subcommand> [flags] <args>\n", name)
		}
		fmt.Fprintln(w, c.desc)
		fmt.Fprintln(w)
		if c.bare != "" {
			fmt.Fprintf(w, "- %s — %s\n", name, c.bare)
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "Subcommands:")
		for _, s := range c.sub {
			fmt.Fprintf(w, "- %s — %s\n", s.name, s.desc)
		}
		if len(c.flags) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Flags:")
			for _, f := range c.flags {
				fmt.Fprintf(w, "  --%s\n", f)
			}
		}
		return
	}
}

func commandDesc(name string) string {
	for _, c := range registry {
		if c.name == name {
			return c.desc
		}
		for _, s := range c.sub {
			if c.name+" "+s.name == name {
				return s.desc
			}
		}
	}
	return ""
}

func handlers() map[string]cmdEntry {
	reg := registry
	m := make(map[string]cmdEntry, len(reg))
	for _, c := range reg {
		m[c.name] = c
	}
	return m
}

var environmentVars = []struct{ name, desc string }{
	{"LM_DIR", "absolute path to the loom directory; no parent-directory search"},
	{"LM_NAMESPACE", "default namespace filter for enumeration commands"},
	{"LM_ACTOR", "default acting user for the audit log"},
	{"LM_READONLY", "any value other than \"\"/0/false/no opens the database read-only"},
	{"LM_NOW", "override the current instant for acceptance tests and diagnostics"},
	{"NO_COLOR", "any value suppresses ANSI decoration"},
	{"LM_COLOR", "\"always\" forces ANSI decoration unless NO_COLOR is also set"},
}

const (
	helpCommandLine       = "- help — Show this help or a command's usage"
	completionCommandLine = "- completion zsh|bash — Print a shell completion script"
)

func writeHelp(w io.Writer) {
	fmt.Fprintln(w, "# lm")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "lm is the Loom CLI: a task and memory tracker for AI coding agents. Output is Markdown only; every command is non-interactive.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: lm <command> [<subcommand>] [flags] [<args>]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Commands")
	for _, c := range registry {
		if len(c.sub) == 0 {
			fmt.Fprintf(w, "- %s — %s\n", c.name, c.desc)
			continue
		}
		if c.bare != "" {
			fmt.Fprintf(w, "- %s — %s\n", c.name, c.bare)
		}
		for _, s := range c.sub {
			fmt.Fprintf(w, "- %s %s — %s\n", c.name, s.name, s.desc)
		}
	}
	fmt.Fprintln(w, helpCommandLine)
	fmt.Fprintln(w, completionCommandLine)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Environment")
	for _, e := range environmentVars {
		fmt.Fprintf(w, "- %s — %s\n", e.name, e.desc)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "See: lm help <command>")
}

func runHelp(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeHelp(stdout)
		return 0
	}
	return dispatch(append(append([]string{}, args...), helpFlag), stdin, stdout, stderr)
}
