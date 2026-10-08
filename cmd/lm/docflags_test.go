// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	docInlineCodeRE = regexp.MustCompile("`([^`]+)`")
	docLmCallRE     = regexp.MustCompile(`(?:^|[\s(])lm ([a-z]+)\b(?: ([a-z]+)\b)?`)
	docLeadingRE    = regexp.MustCompile(`^\s*(?:\$ )?(?:[A-Z_]+=\S* )*$`)
	docQuotedRE     = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'[^']*'`)
	docFlagRE       = regexp.MustCompile(`(?:^|\s)--([a-z][a-z0-9-]*)`)
	helpFlagLineRE  = regexp.MustCompile(`(?m)^\s+--?([a-z][a-z0-9-]*)`)
)

func docCodeFragments(text string) []string {
	var frags []string
	inFence := false
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			frags = append(frags, line)
			continue
		}
		for _, m := range docInlineCodeRE.FindAllStringSubmatch(line, -1) {
			frags = append(frags, m[1])
		}
	}
	return frags
}

type docLmUse struct {
	cmd     string
	leading bool
	flags   []string
}

func docLmUses(fragment string) []docLmUse {
	fragment = docQuotedRE.ReplaceAllStringFunc(fragment, func(s string) string { return strings.Repeat(" ", len(s)) })
	var uses []docLmUse
	for _, seg := range strings.FieldsFunc(fragment, func(r rune) bool { return r == '|' || r == ';' || r == '&' }) {
		locs := docLmCallRE.FindAllStringSubmatchIndex(seg, -1)
		for i, loc := range locs {
			u := docLmUse{cmd: seg[loc[2]:loc[3]]}
			if loc[4] >= 0 && isDocSubcommand(u.cmd, seg[loc[4]:loc[5]]) {
				u.cmd += " " + seg[loc[4]:loc[5]]
			}
			start := strings.Index(seg[loc[0]:], "lm ") + loc[0]
			u.leading = docLeadingRE.MatchString(seg[:start])
			end := len(seg)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			for _, f := range docFlagRE.FindAllStringSubmatch(seg[loc[1]:end], -1) {
				u.flags = append(u.flags, f[1])
			}
			uses = append(uses, u)
		}
	}
	return uses
}

func isDocSubcommand(cmd, sub string) bool {
	if cmd == "help" || cmd == "completion" {
		return false
	}
	for _, c := range registry {
		if c.name != cmd {
			continue
		}
		for _, s := range c.sub {
			if s.name == sub {
				return true
			}
		}
	}
	return false
}

func docFlagViolations(t *testing.T, name, text string) []string {
	t.Helper()
	var out []string
	for _, frag := range docCodeFragments(text) {
		for _, u := range docLmUses(frag) {
			help, known := docHelpFlags(t, u.cmd)
			if !known {
				if u.leading {
					out = append(out, fmt.Sprintf("%s: `lm %s` is not an lm command (in %q)", name, u.cmd, frag))
				}
				continue
			}
			for _, f := range u.flags {
				if !help[f] {
					out = append(out, fmt.Sprintf("%s: `lm %s --%s` is not in `lm help %s` (in %q)", name, u.cmd, f, u.cmd, frag))
				}
			}
		}
	}
	return out
}

var docHelpCache = map[string]map[string]bool{}

func docHelpFlags(t *testing.T, cmd string) (map[string]bool, bool) {
	t.Helper()
	if flags, ok := docHelpCache[cmd]; ok {
		return flags, flags != nil
	}
	var flags map[string]bool
	words := strings.Fields(cmd)
	switch words[0] {
	case "help", "completion":
		flags = map[string]bool{}
	default:
		if _, ok := handlers()[words[0]]; ok {
			out, errS, _ := runCmd(t, append([]string{"help"}, words...), "")
			flags = map[string]bool{"help": true}
			for _, m := range helpFlagLineRE.FindAllStringSubmatch(out+errS, -1) {
				flags[m[1]] = true
			}
		}
	}
	docHelpCache[cmd] = flags
	return flags, flags != nil
}

func helpCommandsSection(lines []string) []string {
	var sec []string
	for _, line := range lines {
		if len(sec) == 0 {
			if strings.TrimSpace(line) == "## Commands" {
				sec = append(sec, "## Commands")
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		sec = append(sec, line)
	}
	for len(sec) > 0 && strings.TrimSpace(sec[len(sec)-1]) == "" {
		sec = sec[:len(sec)-1]
	}
	return sec
}

func docHelpCopyViolations(name, text string, want []string) []string {
	var out []string
	var block []string
	inFence := false
	for i, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inFence {
				block = append(block, line)
			}
			continue
		}
		if inFence {
			if got := helpCommandsSection(block); len(got) > 0 && strings.Join(got, "\n") != strings.Join(want, "\n") {
				out = append(out, fmt.Sprintf("%s:%d: copy of `lm help` Commands differs from the real output:\ngot:\n%s\nwant:\n%s", name, i+1, strings.Join(got, "\n"), strings.Join(want, "\n")))
			}
			block = nil
		}
		inFence = !inFence
	}
	return out
}

func realHelpCommands(t *testing.T) []string {
	t.Helper()
	out, _, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("lm help: exit %d", code)
	}
	sec := helpCommandsSection(strings.Split(out, "\n"))
	if len(sec) < 2 {
		t.Fatalf("lm help has no Commands section:\n%s", out)
	}
	return sec
}

func shippedDocPaths(t *testing.T) (string, []string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	paths := []string{filepath.Join(root, "README.md"), filepath.Join(root, "llms.txt")}
	skills := filepath.Join(root, "skills", "loom")
	err = walkRepo(skills, nil, func(path string, d fs.DirEntry) error {
		if filepath.Ext(path) == ".md" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", skills, err)
	}
	sort.Strings(paths)
	return root, paths
}

func TestDocFlagsShippedHelpCopiesMatch(t *testing.T) {
	root, paths := shippedDocPaths(t)
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatalf("glob docs: %v", err)
	}
	want := realHelpCommands(t)
	for _, p := range append(paths, docs...) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		rel, _ := filepath.Rel(root, p)
		for _, v := range docHelpCopyViolations(filepath.ToSlash(rel), string(b), want) {
			t.Error(v)
		}
	}
}

func TestDocFlagsHelpCopyDetectsDrift(t *testing.T) {
	want := realHelpCommands(t)
	exact := "intro `## Commands` inline\n```\n" + strings.Join(want, "\n") + "\n```\n"
	if got := docHelpCopyViolations("doc.md", exact, want); len(got) != 0 {
		t.Errorf("exact copy reported: %v", got)
	}
	shifted := append([]string{want[0]}, want[2:]...)
	shifted = append(shifted, want[1])
	drift := "```\n" + strings.Join(shifted, "\n") + "\n```\n"
	if got := docHelpCopyViolations("doc.md", drift, want); len(got) != 1 || !strings.HasPrefix(got[0], "doc.md:") {
		t.Errorf("drifted copy: got %v, want 1 violation", got)
	}
}

func TestShippedDocsUseExistingLmFlags(t *testing.T) {
	root, paths := shippedDocPaths(t)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		rel, _ := filepath.Rel(root, p)
		for _, v := range docFlagViolations(t, filepath.ToSlash(rel), string(b)) {
			t.Error(v)
		}
	}
}

func TestDocFlagViolationsDetectsUnknownFlagAndCommand(t *testing.T) {
	doc := "Run `lm search foo --all`.\n```\nlm gate create --blocks x --bogus-flag\nlm export | jq --raw\n```\n`lm nosuchcmd`\n"
	got := docFlagViolations(t, "doc.md", doc)
	sort.Strings(got)
	want := []string{
		"doc.md: `lm gate create --bogus-flag` is not in `lm help gate create` (in \"lm gate create --blocks x --bogus-flag\")",
		"doc.md: `lm nosuchcmd` is not an lm command (in \"lm nosuchcmd\")",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("violations:\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
