// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var (
	listedIDRE     = regexp.MustCompile(`^- (\S+) \[`)
	gateBlockerRE  = regexp.MustCompile(`gate:(\S+)`)
	gateBlocksIDRE = regexp.MustCompile(`→ blocks: ([^ ]+)`)
)

func listedIDs(out, from, until string) []string {
	var ids []string
	started := from == ""
	for line := range strings.SplitSeq(out, "\n") {
		if !started {
			started = line == from
			continue
		}
		if until != "" && line != from && strings.HasPrefix(line, until) {
			break
		}
		if m := listedIDRE.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
		}
	}
	slices.Sort(ids)
	return ids
}

func TestReadyAndGateJudgmentsAgreeAcrossCommands(t *testing.T) {
	initBeadsDir(t)
	free := mustCreateBead(t, "--title", "free")
	chained := mustCreateBead(t, "--title", "chained", "--blocked-by", free)
	human := mustCreateBead(t, "--title", "held-human")
	mustGateCreateCmd(t, []string{human}, "hg", "--kind", "human")
	ext := mustCreateBead(t, "--title", "held-ext")
	mustGateCreateCmd(t, []string{ext}, "xg", "--kind", "external", "--resolver", "w")
	adj := mustCreateBead(t, "--title", "held-adj")
	mustGateCreateCmd(t, []string{adj}, "ag", "--kind", "adjudicate")
	free2 := mustCreateBead(t, "--title", "free2")

	readyOut := mustRun(t, "ready")
	blockedOut := mustRun(t, "blocked")
	gateOut := mustRun(t, "gate")
	aheadOut := mustRun(t, "ahead")

	alias := func(ids ...string) []string {
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = mustDisplayAlias(t, id)
		}
		slices.Sort(out)
		return out
	}

	ready := listedIDs(readyOut, "", "")
	if want := alias(free, free2); !slices.Equal(ready, want) {
		t.Errorf("lm ready = %q, want %q\n%s", ready, want, readyOut)
	}
	if aheadReady := listedIDs(aheadOut, "## Ready", "## "); !slices.Equal(aheadReady, ready) {
		t.Errorf("lm ahead ## Ready = %q, lm ready = %q\n%s", aheadReady, ready, aheadOut)
	}

	blocked := listedIDs(blockedOut, "", "")
	if want := alias(chained, human, ext, adj); !slices.Equal(blocked, want) {
		t.Errorf("lm blocked = %q, want %q\n%s", blocked, want, blockedOut)
	}
	for _, id := range ready {
		if slices.Contains(blocked, id) {
			t.Errorf("%s is listed by both lm ready and lm blocked", id)
		}
	}

	gateOf := map[string]string{}
	for line := range strings.SplitSeq(blockedOut, "\n") {
		m := listedIDRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if g := gateBlockerRE.FindStringSubmatch(line); g != nil {
			gateOf[m[1]] = g[1]
		}
	}
	gateBlocks := map[string]string{}
	for line := range strings.SplitSeq(gateOut, "\n") {
		m := listedIDRE.FindStringSubmatch(line)
		b := gateBlocksIDRE.FindStringSubmatch(line)
		if m != nil && b != nil {
			gateBlocks[b[1]] = m[1]
		}
	}
	if len(gateOf) != len(gateBlocks) {
		t.Errorf("lm blocked holds %d Beads by a Gate, lm gate lists %d\nblocked: %v\ngate: %v", len(gateOf), len(gateBlocks), gateOf, gateBlocks)
	}
	for bead, g := range gateOf {
		if gateBlocks[bead] != g {
			t.Errorf("lm blocked: %s ← gate:%s, lm gate: %s → blocks %s", bead, g, gateBlocks[bead], bead)
		}
	}

	gateRows := gateRowsBySection(gateOut, "")
	aheadRows := gateRowsBySection(aheadOut, "## Gate waiting")
	if !slices.Equal(aheadRows["Needs you"], gateRows["Needs you"]) {
		t.Errorf("ahead ### Needs you = %q, lm gate ## Needs you = %q", aheadRows["Needs you"], gateRows["Needs you"])
	}
	for _, heading := range []string{"Opens automatically", "Waiting on judge"} {
		if len(gateRows[heading]) != 1 {
			t.Errorf("lm gate ## %s rows = %q, want 1\n%s", heading, gateRows[heading], gateOut)
		}
		if want := "- " + heading + ": 1 gates, "; !strings.Contains(aheadOut, want) {
			t.Errorf("lm ahead output lacks %q\n%s", want, aheadOut)
		}
	}
	if want := "- Gate waiting: 0 tokens (" + strconv.Itoa(len(gateOf)) + " beads)"; !strings.Contains(aheadOut, want) {
		t.Errorf("lm ahead output lacks %q (lm blocked holds %d Beads by a Gate)\n%s", want, len(gateOf), aheadOut)
	}
}
