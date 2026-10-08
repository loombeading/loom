// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type tutorialBlock struct {
	cmds []tutorialCmd
}

type tutorialCmd struct {
	line string
	want []string
}

var tutorialIDRE = regexp.MustCompile(`\b(?:id|payments)-[0-9a-z]+\b`)

func parseTutorialBlocks(t *testing.T, text string) []tutorialBlock {
	t.Helper()
	var blocks []tutorialBlock
	var cur *tutorialBlock
	inFence := false
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, "```") {
			if inFence && cur != nil && len(cur.cmds) > 0 {
				blocks = append(blocks, *cur)
			}
			inFence = !inFence
			cur = nil
			continue
		}
		if !inFence {
			continue
		}
		if strings.HasPrefix(line, "$ ") {
			if cur == nil {
				cur = &tutorialBlock{}
			}
			cur.cmds = append(cur.cmds, tutorialCmd{line: strings.TrimPrefix(line, "$ ")})
			continue
		}
		if cur == nil {
			continue
		}
		last := &cur.cmds[len(cur.cmds)-1]
		last.want = append(last.want, line)
	}
	for i := range blocks {
		for j := range blocks[i].cmds {
			c := &blocks[i].cmds[j]
			for len(c.want) > 0 && c.want[len(c.want)-1] == "" {
				c.want = c.want[:len(c.want)-1]
			}
		}
	}
	return blocks
}

func splitTutorialCommand(line string) []string {
	var args []string
	var b strings.Builder
	inQuote, have := false, false
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
			have = true
		case r == ' ' && !inQuote:
			if have {
				args = append(args, b.String())
				b.Reset()
				have = false
			}
		case r == '#' && !inQuote && !have:
			return args
		default:
			b.WriteRune(r)
			have = true
		}
	}
	if have {
		args = append(args, b.String())
	}
	return args
}

type tutorialIDs struct {
	toActual map[string]string
	toDoc    map[string]string
}

func (m *tutorialIDs) bind(t *testing.T, doc, actual string) {
	t.Helper()
	if got, ok := m.toActual[doc]; ok && got != actual {
		t.Fatalf("tutorial ID %s maps to both %s and %s", doc, got, actual)
	}
	if got, ok := m.toDoc[actual]; ok && got != doc {
		t.Fatalf("ID %s appears as both %s and %s in the tutorial", actual, got, doc)
	}
	m.toActual[doc] = actual
	m.toDoc[actual] = doc
}

func (m *tutorialIDs) matchLine(t *testing.T, want, actual string) bool {
	t.Helper()
	locs := tutorialIDRE.FindAllStringIndex(want, -1)
	docIDs := make([]string, 0, len(locs))
	var pat strings.Builder
	pat.WriteString("^")
	last := 0
	for _, loc := range locs {
		pat.WriteString(regexp.QuoteMeta(want[last:loc[0]]) + `((?:id|payments)-[0-9a-z]+)`)
		docIDs = append(docIDs, want[loc[0]:loc[1]])
		last = loc[1]
	}
	pat.WriteString(regexp.QuoteMeta(want[last:]) + "$")
	sub := regexp.MustCompile(pat.String()).FindStringSubmatch(actual)
	if sub == nil {
		return false
	}
	for i, doc := range docIDs {
		if a, ok := m.toActual[doc]; ok && a != sub[i+1] {
			return false
		}
		if d, ok := m.toDoc[sub[i+1]]; ok && d != doc {
			return false
		}
	}
	for i, doc := range docIDs {
		m.bind(t, doc, sub[i+1])
	}
	return true
}

func (m *tutorialIDs) matchOutput(t *testing.T, want, actual []string) bool {
	t.Helper()
	if len(want) == 0 {
		return len(actual) == 0
	}
	if want[0] == "..." {
		for skip := 0; skip <= len(actual); skip++ {
			if m.matchOutput(t, want[1:], actual[skip:]) {
				return true
			}
		}
		return false
	}
	return len(actual) > 0 && m.matchLine(t, want[0], actual[0]) && m.matchOutput(t, want[1:], actual[1:])
}

func (m *tutorialIDs) substitute(s string) string {
	return tutorialIDRE.ReplaceAllStringFunc(s, func(id string) string {
		if a, ok := m.toActual[id]; ok {
			return a
		}
		return id
	})
}

func tutorialMustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, errS, code := runCmd(t, args, "")
	if code != 0 {
		t.Fatalf("lm %v: exit code = %d, stderr = %q", args, code, errS)
	}
	return out
}

func tutorialFreshDB(t *testing.T) {
	t.Helper()
	t.Setenv("LM_DIR", filepath.Join(t.TempDir(), ".loom"))
	tutorialMustRun(t, "init")
}

func TestTutorialCommandsMatchOutput(t *testing.T) {
	text, err := os.ReadFile(filepath.Join("..", "..", "docs", "tutorial.md"))
	if err != nil {
		t.Fatalf("read tutorial: %v", err)
	}
	setIsolatedXDGHome(t)
	t.Chdir(t.TempDir())
	t.Setenv("LM_ACTOR", "tutorial")
	t.Setenv("LM_NAMESPACE", "")
	t.Setenv("LM_NOW", "2026-09-26T09:00:00+09:00")
	local := time.Local
	time.Local = time.FixedZone("JST", 9*60*60)
	t.Cleanup(func() { time.Local = local })

	ids := &tutorialIDs{toActual: map[string]string{}, toDoc: map[string]string{}}
	fresh := func() {
		tutorialFreshDB(t)
		ids = &tutorialIDs{toActual: map[string]string{}, toDoc: map[string]string{}}
	}
	gatesAhead := func() {
		fresh()
		rel := mustParseCreatedID(t, tutorialMustRun(t, "create", "--title", "2026-Q4 リリース", "--priority", "1", "--label", "milestone:2026-Q4"))
		tutorialMustRun(t, "create", "--title", "検索結果のハイライト表示を追加する", "--priority", "1", "--parent", rel)
		terms := mustParseCreatedID(t, tutorialMustRun(t, "create", "--title", "利用規約の改訂を公開する", "--priority", "2", "--parent", rel))
		prices := mustParseCreatedID(t, tutorialMustRun(t, "create", "--title", "価格表を更新する", "--priority", "2", "--parent", rel))
		adj := mustParseCreatedID(t, tutorialMustRun(t, "gate", "create", "--kind", "adjudicate", "--subject", "利用規約の改訂は人の判断が要るか", "--blocks", terms))
		tutorialMustRun(t, "gate", "resolve", adj, "--reason", "人が判断する")
		human := mustParseCreatedID(t, tutorialMustRun(t, "gate", "create", "--kind", "human", "--adjudicated-by", adj,
			"--subject", "利用規約の改訂を公開してよいか", "--check", "利用規約の改訂を公開する作業を始める", "--blocks", terms))
		ext := mustParseCreatedID(t, tutorialMustRun(t, "gate", "create", "--kind", "external", "--resolver", "agent", "--subject", "価格の改定日を待つ", "--blocks", prices))
		tutorialMustRun(t, "update", ext, "--add-label", "wait:date:2026-09-25")
		ids.bind(t, "id-z09", rel)
		ids.bind(t, "id-k4m", human)
		ids.bind(t, "id-w7r", ext)
	}
	steps := []struct {
		first string
		setup func()
	}{
		{"lm gate", gatesAhead},
		{"lm gate resolve", nil},
		{"lm ahead", gatesAhead},
		{"lm create --title \"検索結果", fresh},
		{"lm create --title \"2026-Q4", nil},
		{"lm create --title \"決済", nil},
		{"lm export", nil},
		{"lm import", nil},
	}

	blocks := parseTutorialBlocks(t, string(text))
	if len(blocks) != len(steps) {
		t.Fatalf("docs/tutorial.md has %d command blocks, steps has %d; update steps", len(blocks), len(steps))
	}
	for i, b := range blocks {
		if !strings.HasPrefix(b.cmds[0].line, steps[i].first) {
			t.Fatalf("block %d starts with %q, want %q; update steps", i, b.cmds[0].line, steps[i].first)
		}
		if steps[i].setup != nil {
			steps[i].setup()
		}
		for _, c := range b.cmds {
			args := splitTutorialCommand(ids.substitute(c.line))
			if len(args) == 0 || args[0] != "lm" {
				t.Fatalf("tutorial command %q does not start with lm", c.line)
			}
			args = args[1:]
			redirect := ""
			if n := len(args); n >= 2 && args[n-2] == ">" {
				redirect, args = args[n-1], args[:n-2]
			}
			if args[0] == "import" {
				tutorialFreshDB(t)
			}
			out := tutorialMustRun(t, args...)
			if redirect != "" {
				if err := os.WriteFile(redirect, []byte(out), 0o600); err != nil {
					t.Fatalf("write %s: %v", redirect, err)
				}
				out = ""
			}
			got := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if out == "" {
				got = nil
			}
			if !ids.matchOutput(t, c.want, got) {
				t.Errorf("docs/tutorial.md: $ %s\nwant:\n%s\ngot:\n%s", c.line, strings.Join(c.want, "\n"), out)
			}
		}
	}
}
