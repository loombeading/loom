// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/waitlabel"
)

var (
	gateLineRE     = regexp.MustCompile(`^- (\S+) \[gate/`)
	aheadWaitingRE = regexp.MustCompile(`^(.*)  waiting=(\d+) est=\S+$`)
)

func gateRowsBySection(out, from string) map[string][]string {
	rows := map[string][]string{}
	started := from == ""
	heading := ""
	for line := range strings.SplitSeq(out, "\n") {
		if !started {
			started = line == from
			continue
		}
		if strings.HasPrefix(line, "#") {
			heading = strings.TrimLeft(line, "# ")
			continue
		}
		if !gateLineRE.MatchString(line) {
			continue
		}
		if m := aheadWaitingRE.FindStringSubmatch(line); m != nil {
			line = m[1]
		}
		rows[heading] = append(rows[heading], line)
	}
	return rows
}

func aheadWaiting(t *testing.T, out, gate string) int {
	t.Helper()
	_, section, _ := strings.Cut(out, "## Gate waiting\n")
	for line := range strings.SplitSeq(section, "\n") {
		if !strings.HasPrefix(line, "- "+gate+" [gate/") {
			continue
		}
		m := aheadWaitingRE.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("ahead Gate waiting row %q has no waiting= suffix", line)
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	return -1
}

func TestGateWaitAheadAndGateAgree(t *testing.T) {
	initBeadsDir(t)
	b := mustCreateBead(t, "--title", "B")
	g2 := mustCanonicalID(t, mustGateCreateCmd(t, []string{b}, "g2", "--kind", "human"))
	a := mustCreateBead(t, "--title", "A", "--blocked-by", b)
	g1 := mustCanonicalID(t, mustGateCreateCmd(t, []string{a}, "g1", "--kind", "human"))

	e := mustCreateBead(t, "--title", "E")
	mustGateCreateCmd(t, []string{e}, "g3", "--kind", "external", "--resolver", "watcher")
	d := mustCreateBead(t, "--title", "D", "--blocked-by", e)
	mustCreateBead(t, "--title", "C", "--blocked-by", d)

	ready := mustCreateBead(t, "--title", "ready task")
	mustCreateBead(t, "--title", "R", "--blocked-by", ready, "--blocked-by", b)

	epic := mustCreateBead(t, "--title", "Epic")
	mustCreateBead(t, "--title", "Q", "--parent", epic, "--blocked-by", a)

	aheadOut, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	gateOut, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("gate: exit code = %d, stderr = %q", code, errS)
	}
	bAlias := mustDisplayAlias(t, b)
	aAlias := mustDisplayAlias(t, a)
	epicAlias := mustDisplayAlias(t, epic)
	g2Alias := mustDisplayAlias(t, g2)
	g1Alias := mustDisplayAlias(t, g1)

	if !strings.Contains(gateOut, "  via "+bAlias+": "+aAlias) {
		t.Errorf("lm gate output = %q, want %s's line to name A via B (%q)", gateOut, g2, "  via "+bAlias+": "+aAlias)
	}
	if !strings.Contains(gateOut, "  via "+aAlias+": "+epicAlias) {
		t.Errorf("lm gate output = %q, want %s's line to list the Epic via A (its child Q waits on A)", gateOut, g1)
	}

	aheadRows := gateRowsBySection(aheadOut, "## Gate waiting")
	gateRows := gateRowsBySection(gateOut, "")
	if !slices.Equal(aheadRows["Needs you"], gateRows["Needs you"]) {
		t.Errorf("### Needs you rows differ from ## Needs you:\nahead: %q\ngate:  %q\n--- ahead\n%s\n--- gate\n%s", aheadRows["Needs you"], gateRows["Needs you"], aheadOut, gateOut)
	}

	if got := aheadWaiting(t, aheadOut, g2Alias); got != 5 {
		t.Errorf("ahead Gate waiting %s waiting = %d, want 5\n%s", g2Alias, got, aheadOut)
	}
	for _, folded := range []string{g1Alias, "g3"} {
		if strings.Contains(aheadOut, "] "+folded+"  ") {
			t.Errorf("ahead output = %q, must not print a per-Gate row for %s (Blocked by other work/external fold)", aheadOut, folded)
		}
	}
	if !strings.Contains(aheadOut, "- Blocked by other work: 1 gates, waiting=3 est=n/a(no-data)") {
		t.Errorf("ahead output = %q, want g1's folded line waiting=3 (A, Q, Epic)", aheadOut)
	}
	if !strings.Contains(aheadOut, "- Opens automatically: 1 gates, waiting=3 est=n/a(no-data)") {
		t.Errorf("ahead output = %q, want g3's folded line waiting=3 (E, D, C)", aheadOut)
	}
}

func TestGateSectionsAheadAndGateAgree(t *testing.T) {
	initBeadsDir(t)
	mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "ext")}, "ext", "--kind", "external", "--resolver", "watcher")
	mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "adj")}, "adj", "--kind", "adjudicate")
	unknown := mustGateCreateCmd(t, []string{mustCreateBead(t, "--title", "unk")}, "unk", "--kind", "human")
	if _, errS, code := runCmd(t, []string{"update", unknown, "--remove-label", "kind:human"}, ""); code != 0 {
		t.Fatalf("update --remove-label kind:human: exit code = %d, stderr = %q", code, errS)
	}
	held := mustCreateBead(t, "--title", "held")
	mustGateCreateCmd(t, []string{held}, "held-ext", "--kind", "external", "--resolver", "watcher")
	mustGateCreateCmd(t, []string{held}, "held-human", "--kind", "human")
	free := mustCreateBead(t, "--title", "free")
	mustGateCreateCmd(t, []string{held, free}, "one-free", "--kind", "human")
	pair := mustCreateBead(t, "--title", "pair")
	mustGateCreateCmd(t, []string{pair}, "cycle-1", "--kind", "human")
	mustGateCreateCmd(t, []string{pair}, "cycle-2", "--kind", "confirm")

	aheadOut, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	gateOut, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("gate: exit code = %d, stderr = %q", code, errS)
	}
	aheadRows := gateRowsBySection(aheadOut, "## Gate waiting")
	gateRows := gateRowsBySection(gateOut, "")

	if !slices.Equal(aheadRows["Needs you"], gateRows["Needs you"]) {
		t.Errorf("### Needs you rows differ from ## Needs you:\nahead: %q\ngate:  %q\n--- ahead\n%s\n--- gate\n%s", aheadRows["Needs you"], gateRows["Needs you"], aheadOut, gateOut)
	}
	for key := range aheadRows {
		if key != "Needs you" {
			t.Errorf("ahead printed a per-Gate row under %q, want only Needs you:\n%s", key, aheadOut)
		}
	}

	for _, heading := range []string{"Opens automatically", "Waiting on judge", "Unknown kind (fix labels)", "Blocked by other work"} {
		want := fmt.Sprintf("- %s: %d gates, ", heading, len(gateRows[heading]))
		if !strings.Contains(aheadOut, want) {
			t.Errorf("ahead output = %q, want folded line %q (from ## %s's %d rows)", aheadOut, want, heading, len(gateRows[heading]))
		}
	}
}

func TestGateSectionOfKeepsPersonWaits(t *testing.T) {
	prereq := []domain.Blocker{{}}
	for _, tc := range []struct {
		kind   string
		prereq []domain.Blocker
		own    string
		person bool
		file   bool
	}{
		{"kind:human", nil, gateSecHuman, true, true},
		{"kind:confirm", nil, gateSecHuman, true, true},
		{"kind:human", prereq, gateSecPrereq, true, true},
		{"kind:confirm", prereq, gateSecPrereq, true, true},
		{"kind:adjudicate", nil, gateSecAdjudicate, true, false},
		{"kind:external", nil, gateSecExternal, false, false},
	} {
		for _, k := range allWaitKinds {
			l := k.Prefix() + "x"
			want := gateSecExternal
			if tc.person && waitlabel.WaitsOnPerson(l) || tc.file && k == waitlabel.File {
				want = tc.own
			}
			e := domain.GateListEntry{Labels: []string{tc.kind, l}, PrereqBlockers: tc.prereq}
			if got := gateSectionOf(e); got != want {
				t.Errorf("%s (prereq=%d) + %s: section = %q, want %q", tc.kind, len(tc.prereq), l, got, want)
			}
			e.Labels = append(e.Labels, waitlabel.PRMerged.Prefix()+"u")
			if got := gateSectionOf(e); want == gateSecExternal && got != gateSecExternal {
				t.Errorf("%s (prereq=%d) + %s + wait:pr-merged: section = %q, want %q", tc.kind, len(tc.prereq), l, got, gateSecExternal)
			}
		}
	}
	for _, k := range allWaitKinds {
		if !waitlabel.IsProbe(k.Prefix() + "x") {
			t.Errorf("%s is listed but not a probe kind", k)
		}
	}
}

func TestGateSectionOfUnknownWaitLabel(t *testing.T) {
	prereq := []domain.Blocker{{}}
	for _, kind := range []string{"kind:human", "kind:confirm"} {
		for _, p := range [][]domain.Blocker{nil, prereq} {
			for _, extra := range [][]string{nil, {waitlabel.PRMerged.Prefix() + "u"}, {waitlabel.Date.Prefix() + "2026-10-01"}, {waitlabel.Any}} {
				labels := append([]string{kind, "wait:runners-online:o"}, extra...)
				e := domain.GateListEntry{Labels: labels, PrereqBlockers: p}
				if got := gateSectionOf(e); got != gateSecUnknown {
					t.Errorf("%v (prereq=%d): section = %q, want %q", labels, len(p), got, gateSecUnknown)
				}
			}
		}
	}
	for labels, want := range map[[2]string]string{
		{"kind:adjudicate", "wait:runners-online:o"}: gateSecAdjudicate,
		{"kind:external", "wait:runners-online:o"}:   gateSecExternal,
		{"kind:human", waitlabel.Any}:                gateSecHuman,
	} {
		e := domain.GateListEntry{Labels: labels[:]}
		if got := gateSectionOf(e); got != want {
			t.Errorf("%v: section = %q, want %q", labels, got, want)
		}
	}
}

var allWaitKinds = waitlabel.Kinds

func execLoomDB(t *testing.T, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestAheadShowsUnclassifiedCycle(t *testing.T) {
	initBeadsDir(t)
	y := mustCreateBead(t, "--title", "Y")
	x := mustCreateBead(t, "--title", "X", "--blocked-by", y)
	execLoomDB(t, `INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed) VALUES (?, ?, 'blocks', 'n1', 0)`,
		mustCanonicalID(t, y), mustCanonicalID(t, x))

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	yAlias, xAlias := mustDisplayAlias(t, y), mustDisplayAlias(t, x)
	body := aheadSection(t, out, "## 未分類")
	if len(body) != 2 || !strings.HasPrefix(body[0], "- "+yAlias+" ") || !strings.HasPrefix(body[1], "- "+xAlias+" ") {
		t.Errorf("## 未分類 body = %q, want Y then X", body)
	}
	if !strings.Contains(out, "- Gate waiting: 0 tokens (0 beads)\n- 未分類: 2 件\n") {
		t.Errorf("ahead output = %q, want the 未分類 count after the Gate waiting summary line", out)
	}

	corruptDepth(t, x)
	if _, errS, code := runCmd(t, []string{"ahead"}, ""); code == 0 || errS == "" {
		t.Errorf("ahead with an unclassified Bead's depth malformed: exit code = %d, stderr = %q, want a failure", code, errS)
	}
}

func TestGateFailsWhenGateWaitInputFails(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	mustGateCreateCmd(t, []string{target}, "wait", "--kind", "human")
	execLoomDB(t, `UPDATE beads SET reasoning_depth = 'deep' WHERE id = ?`, mustCanonicalID(t, target))

	if _, errS, code := runCmd(t, []string{"gate"}, ""); code == 0 || errS == "" {
		t.Errorf("lm gate with an unscannable depth: exit code = %d, stderr = %q, want a failure", code, errS)
	}
}

func TestGateFailsWhenViaAliasFails(t *testing.T) {
	initBeadsDir(t)
	b := mustCreateBead(t, "--title", "B")
	mustGateCreateCmd(t, []string{b}, "wait", "--kind", "human")
	a := mustCreateBead(t, "--title", "A", "--blocked-by", b)
	child := mustCreateBead(t, "--title", "child", "--parent", a)
	execLoomDB(t, `INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed) VALUES (?, ?, 'parent-child', 'n1', 0)`,
		mustCanonicalID(t, a), mustCanonicalID(t, child))

	if _, errS, code := runCmd(t, []string{"gate"}, ""); code == 0 || errS == "" {
		t.Errorf("lm gate with a looping parent chain on a via Bead: exit code = %d, stderr = %q, want a failure", code, errS)
	}
}
