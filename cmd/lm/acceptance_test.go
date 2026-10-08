// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
)

func parseTableIDs(md string) []string {
	var ids []string
	for line := range strings.SplitSeq(md, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		idField, _, _ := strings.Cut(strings.TrimPrefix(trimmed, "- "), " [")
		ids = append(ids, idField)
	}
	return ids
}

func TestAcceptanceReferenceTaskSetDrainsWithoutStarvation(t *testing.T) {
	initBeadsDir(t)

	epic := mustCreateBead(t, "--title", "reference", "--type", "task")
	t1 := mustCreateBead(t, "--title", "t1", "--parent", epic)
	t2 := mustCreateBead(t, "--title", "t2", "--parent", epic)
	t3 := mustCreateBead(t, "--title", "t3", "--parent", epic, "--blocked-by", t1, "--blocked-by", t2)
	t4 := mustCreateBead(t, "--title", "t4", "--parent", epic)
	t5 := mustCreateBead(t, "--title", "t5", "--parent", epic, "--blocked-by", t3)
	mustCreateBead(t, "--title", "t6", "--parent", epic, "--blocked-by", t4)
	t7 := mustCreateBead(t, "--title", "t7", "--parent", epic, "--blocked-by", t5)
	mustCreateBead(t, "--title", "t8", "--parent", epic, "--blocked-by", t7, "--blocked-by", t4)
	t9 := mustCreateBead(t, "--title", "t9", "--parent", epic)
	mustCreateBead(t, "--title", "t10", "--parent", epic, "--blocked-by", t9)

	listAllOut, errS, code := runCmd(t, []string{"list", "--all", "--limit", "0"}, "")
	if code != 0 {
		t.Fatalf("list --all: exit code = %d, stderr = %q", code, errS)
	}
	all := parseTableIDs(listAllOut)
	if len(all) < 11 {
		t.Fatalf("reference task set has %d Beads, want at least 11", len(all))
	}

	closed := map[string]bool{}
	for len(closed) < len(all) {
		readyOut, errS, code := runCmd(t, []string{"ready", "--limit", "0"}, "")
		if code != 0 {
			t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
		}
		ids := parseTableIDs(readyOut)
		if len(ids) == 0 {
			t.Fatalf("ready starved with %d/%d Beads closed:\n%s", len(closed), len(all), readyOut)
		}
		id := ids[0]

		if _, errS, code := runCmd(t, []string{"update", "--claim", id}, ""); code != 0 {
			t.Fatalf("claim %s: exit code = %d, stderr = %q", id, code, errS)
		}
		if out, errS, code := runCmd(t, []string{"close", "--reason", "reference task set drain", id}, ""); code != 0 {
			t.Fatalf("close %s: exit code = %d, stdout = %q, stderr = %q", id, code, out, errS)
		}
		closed[id] = true
	}

	listOut, _, code := runCmd(t, []string{"list", "--all", "--status", "closed", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("list --all --status closed failed")
	}

	for _, title := range []string{"reference", "t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10"} {
		if !strings.Contains(listOut, title) {
			t.Errorf("list --all --status closed = %q, missing closed Bead titled %q", listOut, title)
		}
	}
}

func TestAcceptanceNonInteractiveDoesNotHang(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	cases := [][]string{
		{"where"},
		{"list"},
		{"ready"},
		{"blocked"},
		{"show", id},
		{"dep", "add", id, id},
		{"update", "--claim", id},
		{"close", id},
		{"update", "--reopen", id},
	}
	for _, args := range cases {
		done := make(chan struct{})
		go func(args []string) {
			runCmd(t, args, "")
			close(done)
		}(args)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%v: timed out, want non-interactive completion", args)
		}
	}
}

func TestAcceptanceNoOutputFormatFlag(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	for _, flag := range []string{"--format", "--output", "--json"} {
		for _, args := range [][]string{
			{"list", flag, "json"},
			{"show", flag, "json", id},
			{"ready", flag, "json"},
		} {
			_, errS, code := runCmd(t, args, "")
			if code == 0 {
				t.Fatalf("%v unexpectedly succeeded; want rejection of an unknown flag", args)
			}
			if !strings.Contains(errS, "flag provided but not defined") {
				t.Fatalf("%v stderr = %q, want an unknown-flag rejection", args, errS)
			}
		}
	}
}

func TestAcceptanceNoDecorationWhenNotTTY(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "plain output check")

	for _, args := range [][]string{{"list"}, {"ready"}, {"blocked"}, {"where"}} {
		out, _, code := runCmd(t, args, "")
		if code != 0 {
			t.Fatalf("%v: exit code = %d", args, code)
		}
		if strings.ContainsRune(out, 0x1b) {
			t.Errorf("%v output contains an ANSI escape byte:\n%q", args, out)
		}
	}
}

func TestAcceptanceShowHistoryTimeOrderedWithReasons(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	if _, errS, code := runCmd(t, []string{"update", "--title", "t2", "--reason", "renaming", id}, ""); code != 0 {
		t.Fatalf("update --title: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"update", "--reason", "status-quo note", id}, ""); code != 0 {
		t.Fatalf("reason-only update: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	histIdx := strings.Index(out, "## History")
	if histIdx < 0 {
		t.Fatalf("show output missing ## History:\n%s", out)
	}
	hist := out[histIdx:]
	if !strings.Contains(hist, "renaming") {
		t.Errorf("history = %q, missing the field-change reason", hist)
	}
	if !strings.Contains(hist, "status-quo note") {
		t.Errorf("history = %q, missing the state-unchanged note", hist)
	}
	renameOffset := strings.Index(hist, "renaming")
	noteOffset := strings.Index(hist, "status-quo note")
	if renameOffset > noteOffset {
		t.Errorf("history entries out of time order: renaming at %d, note at %d", renameOffset, noteOffset)
	}
}

func TestAcceptanceListDefaultExcludesTerminalReadySubsetSameOrder(t *testing.T) {
	initBeadsDir(t)
	ready1 := mustCreateBead(t, "--title", "ready one")
	ready2 := mustCreateBead(t, "--title", "ready two")
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked one", "--blocked-by", blocker)
	closedID := mustCreateBead(t, "--title", "closed one")
	if _, errS, code := runCmd(t, []string{"close", closedID}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	listOut, _, code := runCmd(t, []string{"list", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("list failed")
	}
	if strings.Contains(listOut, "closed one") {
		t.Errorf("default list = %q, must exclude the closed (terminal) Bead", listOut)
	}
	listAllOut, _, code := runCmd(t, []string{"list", "--all", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("list --all failed")
	}
	if !strings.Contains(listAllOut, "closed one") {
		t.Errorf("list --all = %q, must include the closed Bead when explicitly requested", listAllOut)
	}

	readyOut, _, code := runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	listIDs := parseTableIDs(listOut)
	readyIDs := parseTableIDs(readyOut)

	listSet := map[string]bool{}
	for _, id := range listIDs {
		listSet[id] = true
	}
	for _, id := range readyIDs {
		if !listSet[id] {
			t.Errorf("ready ID %s not present in list output; ready must be a subset of list", id)
		}
	}

	var listOrder, readyOrder []string
	for _, id := range listIDs {
		if id == ready1 || id == ready2 || id == blocker {
			listOrder = append(listOrder, id)
		}
	}
	for _, id := range readyIDs {
		if id == ready1 || id == ready2 || id == blocker {
			readyOrder = append(readyOrder, id)
		}
	}
	if strings.Join(listOrder, ",") != strings.Join(readyOrder, ",") {
		t.Errorf("list order %v != ready order %v for the same input", listOrder, readyOrder)
	}
}

func TestAcceptanceClaimedExcludedFromReadyReleaseAndForceReleaseReappear(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	idAlias := mustDisplayAlias(t, id)

	if _, errS, code := runCmd(t, []string{"update", "--claim", "--actor", "alice", id}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}
	readyOut, _, code := runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	if strings.Contains(readyOut, idAlias) {
		t.Errorf("ready output = %q, must not list a claimed Bead", readyOut)
	}

	if _, errS, code := runCmd(t, []string{"update", "--release", "--actor", "alice", id}, ""); code != 0 {
		t.Fatalf("self-release exit code = %d, stderr = %q", code, errS)
	}
	readyOut, _, code = runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	if !strings.Contains(readyOut, idAlias) {
		t.Errorf("ready output = %q, want the Bead back after self-release", readyOut)
	}

	if _, errS, code := runCmd(t, []string{"update", "--claim", "--actor", "bob", id}, ""); code != 0 {
		t.Fatalf("bob claims: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"update", "--release", "--force", "--actor", "carol", id}, ""); code != 0 {
		t.Fatalf("force-release by a third actor: exit code = %d, stderr = %q", code, errS)
	}
	readyOut, _, code = runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	if !strings.Contains(readyOut, idAlias) {
		t.Errorf("ready output = %q, want the Bead back after a force-release", readyOut)
	}

	showOut, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, "force_release") {
		t.Errorf("show history = %q, want the force-release recorded distinctly from an ordinary release", showOut)
	}
}

func TestAcceptanceCancelledExcludedNoDeleteCommandRemovedLinkIgnored(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	if _, errS, code := runCmd(t, []string{"update", "--status", "cancelled", blocker}, ""); code != 0 {
		t.Fatalf("cancel: exit code = %d, stderr = %q", code, errS)
	}
	blockerAlias := mustDisplayAlias(t, blocker)

	readyOut, _, code := runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	if strings.Contains(readyOut, blockerAlias) {
		t.Errorf("ready output = %q, must not list the cancelled Bead", readyOut)
	}

	for _, name := range []string{"delete", "rm", "destroy", "purge"} {
		_, errS, code := runCmd(t, []string{name, blocker}, "")
		if code != 1 || !strings.Contains(errS, "unknown subcommand") {
			t.Errorf("%q subcommand = (code=%d, stderr=%q), want an unknown-subcommand rejection", name, code, errS)
		}
	}

	blocker2 := mustCreateBead(t, "--title", "blocker2")
	blocked2 := mustCreateBead(t, "--title", "blocked2", "--blocked-by", blocker2)
	blocked2Alias := mustDisplayAlias(t, blocked2)
	if _, errS, code := runCmd(t, []string{"dep", "remove", blocked2, blocker2}, ""); code != 0 {
		t.Fatalf("dep remove: exit code = %d, stderr = %q", code, errS)
	}
	readyOut, _, code = runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	if !strings.Contains(readyOut, blocked2Alias) {
		t.Errorf("ready output = %q, want the Bead unblocked once its link is removed", readyOut)
	}
}

func TestAcceptanceAliasAndShortIDRoundTripAmbiguousOffersCandidates(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic", "--type", "task")
	child := mustCreateBead(t, "--title", "child", "--parent", epic)

	showOut, _, code := runCmd(t, []string{"show", child}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	var alias, canonical string
	for line := range strings.SplitSeq(showOut, "\n") {
		if strings.HasPrefix(line, "alias: ") {
			alias = frontMatterStringValue(line, "alias")
		}
		if strings.HasPrefix(line, "id: ") {
			canonical = frontMatterStringValue(line, "id")
		}
	}
	if alias == "" {
		t.Fatalf("show output missing an Alias line:\n%s", showOut)
	}
	if len(canonical) != 26 {
		t.Fatalf("show output Canonical ID = %q, want a 26-char Crockford Base32 ID:\n%s", canonical, showOut)
	}

	byAlias, _, code := runCmd(t, []string{"show", alias}, "")
	if code != 0 {
		t.Fatalf("show by alias %q failed", alias)
	}
	if !strings.Contains(byAlias, "child") {
		t.Errorf("show by alias %q = %q, want the same child Bead", alias, byAlias)
	}

	short := domain.DefaultNamespace + "-" + canonical[:8]
	byShort, _, code := runCmd(t, []string{"show", short}, "")
	if code != 0 {
		t.Fatalf("show by short id %q failed", short)
	}
	if !strings.Contains(byShort, "child") {
		t.Errorf("show by short id %q = %q, want the same child Bead", short, byShort)
	}

	seen := map[byte]bool{canonical[0]: true}
	var collidingPrefix byte
	found := false
	for i := 0; i < 32 && !found; i++ {
		other := mustCreateBead(t, "--title", "filler")
		out, _, code := runCmd(t, []string{"show", other}, "")
		if code != 0 {
			t.Fatal("show filler failed")
		}
		var otherCanonical string
		for line := range strings.SplitSeq(out, "\n") {
			if strings.HasPrefix(line, "id: ") {
				otherCanonical = frontMatterStringValue(line, "id")
			}
		}
		if seen[otherCanonical[0]] {
			collidingPrefix = otherCanonical[0]
			found = true
			break
		}
		seen[otherCanonical[0]] = true
	}
	if !found {
		t.Fatal("failed to force a hex-namespace collision within 32 draws (pigeonhole guarantees one within 17)")
	}

	_, errS, code := runCmd(t, []string{"show", string(collidingPrefix)}, "")
	if code == 0 {
		t.Fatalf("show by the deliberately colliding namespace %q succeeded, want an ambiguous rejection", string(collidingPrefix))
	}
	if !strings.Contains(errS, "ambiguous") {
		t.Errorf("show by colliding namespace stderr = %q, want an ambiguous-candidates error", errS)
	}
}

func TestAcceptanceParentAndBlockedByAtCreateNeverReadyBeforeOrAfter(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic", "--type", "task")
	blocker := mustCreateBead(t, "--title", "blocker")

	mustCreateBead(t, "--title", "child", "--parent", epic, "--blocked-by", blocker)

	readyOut, _, code := runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}

	if strings.Contains(readyOut, "child") {
		t.Errorf("ready output right after create = %q, must not list the child (create+link must be atomic)", readyOut)
	}

	if _, errS, code := runCmd(t, []string{"close", blocker}, ""); code != 0 {
		t.Fatalf("close blocker: exit code = %d, stderr = %q", code, errS)
	}
	readyOut, _, code = runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	if !strings.Contains(readyOut, "child") {
		t.Errorf("ready output after closing blocker = %q, want the child now listed", readyOut)
	}
}

func TestAcceptanceBlockedAndReadyDisjointUnionCoversAllOpen(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked", "--blocked-by", blocker)
	mustCreateBead(t, "--title", "ready one")
	claimed := mustCreateBead(t, "--title", "claimed")
	if _, errS, code := runCmd(t, []string{"update", "--claim", claimed}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}

	readyOut, _, code := runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	blockedOut, _, code := runCmd(t, []string{"blocked", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("blocked failed")
	}

	openOut, _, code := runCmd(t, []string{"list", "--status", "open", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("list --status open failed")
	}

	readySet := map[string]bool{}
	for _, id := range parseTableIDs(readyOut) {
		readySet[id] = true
	}
	blockedSet := map[string]bool{}
	for _, id := range parseTableIDs(blockedOut) {
		if readySet[id] {
			t.Errorf("Bead %s present in both ready and blocked", id)
		}
		blockedSet[id] = true
	}

	openIDs := parseTableIDs(openOut)
	if len(openIDs) == 0 {
		t.Fatal("no open Beads found; test setup is broken")
	}
	for _, id := range openIDs {
		if !readySet[id] && !blockedSet[id] {
			t.Errorf("open Bead %s is in neither ready nor blocked", id)
		}
	}
	for id := range readySet {
		found := false
		for _, o := range openIDs {
			if o == id {
				found = true
			}
		}
		if !found {
			t.Errorf("ready Bead %s is not open", id)
		}
	}
}

func TestAcceptanceClaimWithOpenBlockersRejectedForceClaimAudited(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	byBead := mustCreateBead(t, "--title", "blocked by bead", "--blocked-by", blocker)
	byGate := mustCreateBead(t, "--title", "blocked by gate")
	mustGateCreateCmd(t, []string{byGate}, "wait")

	for _, id := range []string{byBead, byGate} {
		out, _, code := runCmd(t, []string{"update", "--claim", "--actor", "alice", id}, "")
		if code != 1 || !strings.Contains(out, "blocked by") {
			t.Errorf("claim %s = (code=%d, stdout=%q), want exit 1 with a blocked-by rejection", id, code, out)
		}
		showOut, _, code := runCmd(t, []string{"show", id}, "")
		if code != 0 {
			t.Fatal("show failed")
		}
		if !strings.Contains(showOut, "status: \"open\"") || !strings.Contains(showOut, "claimed_by: null") {
			t.Errorf("show %s after a rejected claim = %q, want status open and no claimed_by", id, showOut)
		}
	}

	if _, errS, code := runCmd(t, []string{"update", "--claim", "--force", "--actor", "alice", byBead}, ""); code != 1 {
		t.Errorf("force claim without reason = (code=%d, stderr=%q), want exit 1", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"update", "--claim", "--force", "--reason", "fix the PR", "--actor", "alice", byBead}, ""); code != 0 {
		t.Fatalf("force claim with reason: exit code = %d, stderr = %q", code, errS)
	}
	showOut, _, code := runCmd(t, []string{"show", byBead}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, "status: \"in_progress\"") || !strings.Contains(showOut, "force_claim") {
		t.Errorf("show after force claim = %q, want in_progress and a force_claim history entry", showOut)
	}
}

func TestAcceptanceBlockedReadyPartitionOnRandomGraphs(t *testing.T) {
	for seed := int64(1); seed <= 5; seed++ {
		t.Run(strconv.FormatInt(seed, 10), func(t *testing.T) {
			initBeadsDir(t)
			rng := rand.New(rand.NewSource(seed))
			ids := make([]string, 0, 10)
			var gates []string
			for i := range 10 {
				args := []string{"--title", fmt.Sprintf("n%d", i)}
				if i > 0 && rng.Intn(4) == 0 {
					args = append(args, "--parent", ids[rng.Intn(i)])
				}
				ids = append(ids, mustCreateBead(t, args...))
			}
			for j := 1; j < len(ids); j++ {
				for i := range j {
					if rng.Intn(5) != 0 {
						continue
					}
					if _, errS, code := runCmd(t, []string{"dep", "add", ids[j], ids[i], "--type", "blocks"}, ""); code != 0 {
						t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
					}
				}
			}
			for _, id := range ids {
				if rng.Intn(5) == 0 {
					gates = append(gates, mustGateCreateCmd(t, []string{id}, "wait"))
				}
			}
			for _, id := range slices.Backward(ids) {
				var args []string
				switch rng.Intn(6) {
				case 0:
					args = []string{"close", id}
				case 1:
					args = []string{"update", "--status", "cancelled", id}
				default:
					continue
				}
				if _, errS, code := runCmd(t, args, ""); code != 0 && !strings.Contains(errS, "unfinished ") {
					t.Fatalf("%v: exit code = %d, stderr = %q", args, code, errS)
				}
			}
			assertReadyBlockedPartitionOpenNonGate(t, gates)
		})
	}

	t.Run("parent_with_open_child", func(t *testing.T) {
		initBeadsDir(t)
		parent := mustCreateBead(t, "--title", "parent")
		child := mustCreateBead(t, "--title", "child", "--parent", parent)
		parentAlias, childAlias := mustDisplayAlias(t, parent), mustDisplayAlias(t, child)

		readyOut, _, code := runCmd(t, []string{"ready", "--limit", "0"}, "")
		if code != 0 {
			t.Fatal("ready failed")
		}
		if slices.Contains(parseTableIDs(readyOut), parentAlias) {
			t.Errorf("ready output = %q, must not list the parent of an open child", readyOut)
		}
		blockedOut, _, code := runCmd(t, []string{"blocked", "--limit", "0"}, "")
		if code != 0 {
			t.Fatal("blocked failed")
		}
		var parentLine string
		for line := range strings.SplitSeq(blockedOut, "\n") {
			if strings.HasPrefix(strings.TrimLeft(line, " "), "- "+parentAlias+" ") {
				parentLine = line
			}
		}
		if !strings.Contains(parentLine, "← blocked by:") || !strings.Contains(parentLine, childAlias) {
			t.Errorf("blocked output = %q, want the parent listed with its child as a blocker", blockedOut)
		}

		if _, errS, code := runCmd(t, []string{"close", child}, ""); code != 0 {
			t.Fatalf("close child: exit code = %d, stderr = %q", code, errS)
		}
		readyOut, _, code = runCmd(t, []string{"ready", "--limit", "0"}, "")
		if code != 0 {
			t.Fatal("ready failed")
		}
		if !slices.Contains(parseTableIDs(readyOut), parentAlias) {
			t.Errorf("ready output = %q, want the parent once its only child is terminal", readyOut)
		}
	})
}

func assertReadyBlockedPartitionOpenNonGate(t *testing.T, gates []string) {
	t.Helper()
	gateSet := map[string]bool{}
	for _, g := range gates {
		gateSet[mustDisplayAlias(t, g)] = true
	}
	ids := func(args ...string) []string {
		out, errS, code := runCmd(t, append(args, "--limit", "0"), "")
		if code != 0 {
			t.Fatalf("%v: exit code = %d, stderr = %q", args, code, errS)
		}
		return parseTableIDs(out)
	}
	readySet := map[string]bool{}
	for _, id := range ids("ready") {
		readySet[id] = true
	}
	union := maps.Clone(readySet)
	for _, id := range ids("blocked") {
		if readySet[id] {
			t.Errorf("Bead %s present in both ready and blocked", id)
		}
		union[id] = true
	}
	openSet := map[string]bool{}
	for _, id := range ids("list", "--status", "open") {
		if !gateSet[id] {
			openSet[id] = true
		}
	}
	if !maps.Equal(union, openSet) {
		t.Errorf("ready ∪ blocked = %v, want the open non-Gate Beads %v", slices.Sorted(maps.Keys(union)), slices.Sorted(maps.Keys(openSet)))
	}
}

func TestAcceptanceBDDirResolvesFromSubdirAndWorktreeWhereReportsIt(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".loom")
	t.Setenv("LM_DIR", beadsDir)

	rootCwd := t.TempDir()
	t.Chdir(rootCwd)
	if code := run([]string{"init"}, strings.NewReader(""), new(strings.Builder), new(strings.Builder)); code != 0 {
		t.Fatal("init failed")
	}

	subdir := filepath.Join(rootCwd, "a", "b", "c")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(subdir)
	var stdout1 strings.Builder
	if code := run([]string{"where"}, strings.NewReader(""), &stdout1, new(strings.Builder)); code != 0 {
		t.Fatal("where from subdir failed")
	}
	if !strings.Contains(stdout1.String(), beadsDir) || !strings.Contains(stdout1.String(), "Resolved via: LM_DIR") {
		t.Errorf("where from subdir = %q, want it to resolve LM_DIR to %s", stdout1.String(), beadsDir)
	}

	worktreeLike := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(worktreeLike, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(worktreeLike)
	var stdout2 strings.Builder
	if code := run([]string{"where"}, strings.NewReader(""), &stdout2, new(strings.Builder)); code != 0 {
		t.Fatal("where from worktree-like dir failed")
	}
	if !strings.Contains(stdout2.String(), beadsDir) || !strings.Contains(stdout2.String(), "Resolved via: LM_DIR") {
		t.Errorf("where from worktree-like dir = %q, want it to resolve LM_DIR to %s", stdout2.String(), beadsDir)
	}
}

func TestAcceptanceLocateIgnoresCWDLoomDirAndDoesNotSearchParents(t *testing.T) {
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, ".loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)
	if err := os.MkdirAll(filepath.Join(child, ".loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	setIsolatedXDGHome(t)

	var stdout, stderr strings.Builder
	code := run([]string{"where"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("where with only cwd/parent .loom dirs: exit code = %d, want 1 (cwd/parent must not be searched)", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("where stdout = %q, want empty on failure", stdout.String())
	}
}

func TestAcceptanceXDGDefaultDBInitAndUseLMDirTakesPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)
	t.Setenv("LM_ACTOR", "test-actor")

	if out, _, code := runCmd(t, []string{"ready"}, ""); code != 1 || out != "" {
		t.Fatalf("ready with no DB in the XDG data home: exit code = %d, stdout = %q, want 1 and empty", code, out)
	}

	if _, errS, code := runCmd(t, []string{"init"}, ""); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, errS)
	}
	xdgDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "loom")
	out, _, code := runCmd(t, []string{"where"}, "")
	if code != 0 || !strings.Contains(out, xdgDir) || !strings.Contains(out, "Resolved via: default") {
		t.Fatalf("where after init = %q (exit %d), want %s resolved via default", out, code, xdgDir)
	}
	alias := mustCreateBeadAlias(t, "--title", "in xdg")
	if out, _, code := runCmd(t, []string{"ready"}, ""); code != 0 || !strings.Contains(out, alias) {
		t.Fatalf("ready = %q (exit %d), want it to list %s from the XDG database", out, code, alias)
	}

	lmDir := t.TempDir()
	t.Setenv("LM_DIR", lmDir)
	if _, errS, code := runCmd(t, []string{"init"}, ""); code != 0 {
		t.Fatalf("init with LM_DIR: exit code = %d, stderr = %q", code, errS)
	}
	out, _, code = runCmd(t, []string{"where"}, "")
	if code != 0 || !strings.Contains(out, lmDir) || !strings.Contains(out, "Resolved via: LM_DIR") {
		t.Fatalf("where with LM_DIR = %q (exit %d), want %s resolved via LM_DIR", out, code, lmDir)
	}
	if out, _, code := runCmd(t, []string{"ready"}, ""); code != 0 || strings.Contains(out, alias) {
		t.Fatalf("ready with LM_DIR = %q (exit %d), must not list %s from the XDG database", out, code, alias)
	}
}

func TestAcceptanceReopenRecordedAndClearsClaimedBy(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"update", "--claim", "--actor", "alice", id}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	if _, errS, code := runCmd(t, []string{"update", "--reopen", id}, ""); code != 0 {
		t.Fatalf("reopen: exit code = %d, stderr = %q", code, errS)
	}

	showOut, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, `status: "open"`) {
		t.Errorf("show after reopen = %q, want status: \"open\"", showOut)
	}
	if !strings.Contains(showOut, `claimed_by: null`) {
		t.Errorf("show after reopen = %q, want the claimed_by cleared", showOut)
	}
	if !strings.Contains(showOut, "reopen") {
		t.Errorf("show history = %q, want the reopen recorded", showOut)
	}
}

func TestAcceptanceMultiIDClosePartialRejectionShowsIDsAndReasons(t *testing.T) {
	initBeadsDir(t)
	ok := mustCreateBead(t, "--title", "ok")
	gate := mustCreateBead(t, "--title", "g", "--type", "gate", "--label", "kind:human")
	okAlias := mustDisplayAlias(t, ok)

	out, _, code := runCmd(t, []string{"close", ok, gate}, "")
	if code != 1 {
		t.Fatalf("close [ok, gate]: exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "- Closed: "+okAlias) {
		t.Errorf("close output = %q, want the ok Bead marked closed", out)
	}
	if !strings.Contains(out, gate) {
		t.Errorf("close output = %q, want the rejected gate's ID shown", out)
	}
	if !strings.Contains(out, "Rejected:") {
		t.Errorf("close output = %q, want a rejection reason shown", out)
	}
	showOut, _, code := runCmd(t, []string{"show", ok}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, `status: "closed"`) {
		t.Errorf("show %s = %q, want it closed despite the sibling rejection", okAlias, showOut)
	}
}

func TestAcceptanceCloseOutputOnlyListsNewlyReadyBeads(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "newly ready", "--blocked-by", blocker)
	mustCreateBead(t, "--title", "already ready")

	out, errS, code := runCmd(t, []string{"close", blocker}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	newlyIdx := strings.Index(out, "## Newly ready")
	if newlyIdx < 0 {
		t.Fatalf("close output missing ## Newly ready:\n%s", out)
	}
	section := out[newlyIdx:]
	if !strings.Contains(section, "newly ready") {
		t.Errorf("Newly ready section = %q, want the newly-unblocked Bead", section)
	}
	if strings.Contains(section, "already ready") {
		t.Errorf("Newly ready section = %q, must not include a Bead that was already Ready", section)
	}
}

func TestAcceptanceReadOnlyModeRejectsWritesAllowsReads(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	setIsolatedXDGHome(t)
	t.Setenv("LM_READONLY", "0")
	if _, errS, code := runCmd(t, []string{"init"}, ""); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, errS)
	}
	id := mustCreateBead(t, "--title", "t")

	t.Setenv("LM_READONLY", "1")

	for _, args := range [][]string{
		{"update", "--claim", id},
		{"close", id},
		{"dep", "add", id, id},
		{"create", "--title", "x"},
		{"cost", "add", id, "--in", "1", "--out", "1"},
	} {
		out, errS, code := runCmd(t, args, "")
		if code != 1 {
			t.Errorf("%v under LM_READONLY=1: exit code = %d, want 1", args, code)
		}
		if errS == "" && !strings.Contains(out, "Rejected:") {
			t.Errorf("%v under LM_READONLY=1: no error message on stdout or stderr (stdout=%q, stderr=%q)", args, out, errS)
		}
	}

	for _, args := range [][]string{
		{"list"},
		{"show", id},
		{"ready"},
		{"blocked"},
		{"where"},
	} {
		_, errS, code := runCmd(t, args, "")
		if code != 0 {
			t.Errorf("%v under LM_READONLY=1: exit code = %d, stderr = %q, want a normal read", args, code, errS)
		}
	}
}

func TestAcceptanceHeadingsAndColumnsStableWithinPhase1(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	blocker := mustCreateBead(t, "--title", "b")
	blockedID := mustCreateBead(t, "--title", "bt", "--blocked-by", blocker)
	idAlias, blockerAlias, blockedIDAlias := mustDisplayAlias(t, id), mustDisplayAlias(t, blocker), mustDisplayAlias(t, blockedID)

	listOut, _, _ := runCmd(t, []string{"list"}, "")
	wantListLine := "- " + idAlias + " [open/P2] t"
	if !strings.Contains(listOut, wantListLine) {
		t.Errorf("list line changed: want %q in:\n%s", wantListLine, listOut)
	}
	blockedOut, _, _ := runCmd(t, []string{"blocked"}, "")
	wantBlockedLine := "- " + blockedIDAlias + " [open/P2] bt  ← blocked by: " + blockerAlias
	if !strings.Contains(blockedOut, wantBlockedLine) {
		t.Errorf("blocked line changed: want %q in:\n%s", wantBlockedLine, blockedOut)
	}
	showOut, _, _ := runCmd(t, []string{"show", id}, "")
	for _, heading := range []string{"## Description", "## Dependencies", "## History"} {
		if !strings.Contains(showOut, heading) {
			t.Errorf("show output missing heading %q:\n%s", heading, showOut)
		}
	}
}

func TestAcceptanceFormerNamespaceIDResolvesWithNotice(t *testing.T) {
	initBeadsDir(t)

	parent := mustCreateBead(t, "--title", "parent", "--namespace", "lm")
	child := mustCreateBead(t, "--title", "child", "--parent", parent)
	if _, errS, code := runCmd(t, []string{"update", "--namespace", "gr", parent}, ""); code != 0 {
		t.Fatalf("update --namespace gr: exit code = %d, stderr = %q", code, errS)
	}
	oldID := "lm-" + parent[:8]

	showOut, showErr, code := runCmd(t, []string{"show", oldID}, "")
	if code != 0 {
		t.Fatalf("show %s: exit code = %d, stderr = %q", oldID, code, showErr)
	}
	if !strings.Contains(showOut, `id: "`+parent+`"`) {
		t.Fatalf("show %s resolved to the wrong Bead:\n%s", oldID, showOut)
	}
	wantShow := "Note: " + oldID + " uses a former namespace; the current display ID is " + mustDisplayAlias(t, parent) + "\n"
	if showErr != wantShow {
		t.Fatalf("show %s stderr = %q, want %q", oldID, showErr, wantShow)
	}
	if strings.Contains(showOut, "former namespace") {
		t.Fatalf("show %s stdout carries the notice:\n%s", oldID, showOut)
	}

	_, updErr, code := runCmd(t, []string{"update", oldID + ".1", "--reason", "via former namespace"}, "")
	if code != 0 {
		t.Fatalf("update %s.1: exit code = %d, stderr = %q", oldID, code, updErr)
	}
	wantUpd := "Note: " + oldID + ".1 uses a former namespace; the current display ID is " + mustDisplayAlias(t, parent) + ".1\n"
	if updErr != wantUpd {
		t.Fatalf("update %s.1 stderr = %q, want %q", oldID, updErr, wantUpd)
	}
	childOut, _, _ := runCmd(t, []string{"show", child}, "")
	if !strings.Contains(childOut, "via former namespace") {
		t.Fatalf("update %s.1 did not reach the child:\n%s", oldID, childOut)
	}
}

func TestAcceptanceNamespaceFromEnvAppliesAtCreateAndChangeIsAudited(t *testing.T) {
	initBeadsDir(t)
	t.Setenv("LM_NAMESPACE", "web")

	id := mustCreateBead(t, "--title", "t")
	idAlias := mustDisplayAlias(t, id)
	if !strings.HasPrefix(idAlias, "web-") {
		t.Fatalf("create with LM_NAMESPACE=web: display ID = %q, want a web- namespace", idAlias)
	}

	showOut, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show %s: exit code = %d", id, code)
	}
	if !strings.Contains(showOut, "id:") {
		t.Fatalf("show output missing id line:\n%s", showOut)
	}

	updateOut, updateErr, code := runCmd(t, []string{"update", "--namespace", "app", id}, "")
	if code != 0 {
		t.Fatalf("update --namespace: exit code = %d, stderr = %q", code, updateErr)
	}
	if !strings.Contains(updateOut, "app-") {
		t.Fatalf("update --namespace output = %q, want the new app- namespace", updateOut)
	}
	newID := strings.Fields(updateOut)[2]
	if !strings.HasPrefix(newID, "app-") {
		t.Fatalf("update --namespace output = %q, want new display ID to start with app-", updateOut)
	}

	showOut, _, code = runCmd(t, []string{"show", newID}, "")
	if code != 0 {
		t.Fatalf("show %s: exit code = %d", newID, code)
	}
	if !strings.Contains(showOut, "field namespace = app") {
		t.Fatalf("show history missing the namespace change record:\n%s", showOut)
	}
}

func TestAcceptanceNamespaceFilterOnlyAffectsListingsAndCanBeReset(t *testing.T) {
	initBeadsDir(t)
	webID := mustCreateBead(t, "--title", "web task", "--namespace", "web")
	appID := mustCreateBead(t, "--title", "app task", "--namespace", "app")
	webAlias, appAlias := mustDisplayAlias(t, webID), mustDisplayAlias(t, appID)

	out, _, code := runCmd(t, []string{"list", "--namespace", "web"}, "")
	if code != 0 {
		t.Fatalf("list --namespace web: exit code = %d", code)
	}
	if !strings.Contains(out, `Filter: namespace="web"`) {
		t.Fatalf("list --namespace web stdout = %q, want a Filter: line naming namespace=web", out)
	}
	if !strings.Contains(out, webAlias) || strings.Contains(out, appAlias) {
		t.Fatalf("list --namespace web stdout = %q, want only %s, not %s", out, webAlias, appAlias)
	}

	t.Setenv("LM_NAMESPACE", "web")
	showOut, _, code := runCmd(t, []string{"show", appID}, "")
	if code != 0 {
		t.Fatalf("show %s under LM_NAMESPACE=web: exit code = %d, want unaffected", appID, code)
	}
	if !strings.Contains(showOut, "app task") {
		t.Fatalf("show %s under LM_NAMESPACE=web = %q, want the Bead unaffected by the filter", appID, showOut)
	}

	out, _, code = runCmd(t, []string{"list", "--namespace", ""}, "")
	if code != 0 {
		t.Fatalf(`list --namespace "": exit code = %d`, code)
	}
	if strings.Contains(out, "namespace=") {
		t.Fatalf(`list --namespace "" stdout = %q, want no namespace= condition once the default is reset`, out)
	}
	if !strings.Contains(out, webAlias) || !strings.Contains(out, appAlias) {
		t.Fatalf(`list --namespace "" stdout = %q, want both %s and %s`, out, webAlias, appAlias)
	}
}

func TestAcceptanceReadyClaimClaimsHeadOfOrderAndAuditsOrdinaryClaim(t *testing.T) {
	initBeadsDir(t)
	low := mustCreateBead(t, "--title", "low priority", "--priority", "3")
	high := mustCreateBead(t, "--title", "high priority", "--priority", "1")
	highAlias := mustDisplayAlias(t, high)

	out, errS, code := runCmd(t, []string{"ready", "--claim", "--actor", "alice"}, "")
	if code != 0 {
		t.Fatalf("ready --claim: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "Claimed: "+highAlias) {
		t.Fatalf("ready --claim stdout = %q, want the head of the order (%s) claimed, not the lower-priority %s", out, highAlias, low)
	}

	showOut, _, code := runCmd(t, []string{"show", high}, "")
	if code != 0 {
		t.Fatalf("show %s: exit code = %d", high, code)
	}
	if !strings.Contains(showOut, "field claim = alice") {
		t.Fatalf("show history = %q, want an ordinary claim audit record for alice", showOut)
	}
}

func TestAcceptanceSearchFindsBeadByAuditReasonAlone(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "unrelated title", "--description", "unrelated body")
	other := mustCreateBead(t, "--title", "other bead", "--description", "other body")
	idAlias, otherAlias := mustDisplayAlias(t, id), mustDisplayAlias(t, other)

	if _, errS, code := runCmd(t, []string{"update", "--reason", "handoffnote-zephyr", id}, ""); code != 0 {
		t.Fatalf("reason-only update: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"search", "handoffnote-zephyr"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, idAlias) {
		t.Fatalf("search output = %q, want the Bead reachable only via its audit reason", out)
	}
	if strings.Contains(out, otherAlias) {
		t.Fatalf("search output = %q, must not return the unrelated Bead", out)
	}
}

func TestAcceptanceSearchFindsBeadByExternalRef(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "has a ref", "--external-ref", "https://example.invalid/pr/4242")
	other := mustCreateBead(t, "--title", "no ref here")
	idAlias, otherAlias := mustDisplayAlias(t, id), mustDisplayAlias(t, other)

	showOut, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(showOut, "https://example.invalid/pr/4242") {
		t.Fatalf("show output = %q, want the external ref displayed", showOut)
	}

	out, errS, code := runCmd(t, []string{"search", "4242"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, idAlias) {
		t.Fatalf("search output = %q, want the Bead reachable via its external ref", out)
	}
	if strings.Contains(out, otherAlias) {
		t.Fatalf("search output = %q, must not return the unrelated Bead", out)
	}
}

func TestAcceptanceNoNetworkImports(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	for _, dir := range []string{"cmd", "internal"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				importPath := strings.Trim(imp.Path.Value, `"`)
				if importPath == "net" || importPath == "net/http" {
					t.Errorf("%s imports %q, want no network package imported anywhere under cmd/ or internal/", path, importPath)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
}

func TestAcceptanceImportCycleResolutionIsAudited(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")
	c := mustCreateBead(t, "--title", "c")
	ca, cc := canonicalID(t, a), canonicalID(t, c)

	if _, _, code := runCmd(t, []string{"dep", "add", b, a, "--type", "blocks"}, ""); code != 0 {
		t.Fatalf("dep add b a")
	}
	if _, _, code := runCmd(t, []string{"dep", "add", c, b, "--type", "blocks"}, ""); code != 0 {
		t.Fatalf("dep add c b")
	}

	jsonl := `{"_type":"dependency","bead_id":"` + ca + `","depends_on_id":"` + cc + `","type":"blocks","created_at":"2099-01-01T00:00:00.000Z","removed":false}`
	out := mustImport(t, jsonl)
	if !strings.Contains(out, "Cycle resolved:") {
		t.Fatalf("expected a Cycle resolved line:\n%s", out)
	}

	showOut, _, code := runCmd(t, []string{"show", a}, "")
	if code != 0 {
		t.Fatalf("show %s: exit code = %d", a, code)
	}
	if !strings.Contains(showOut, "dependency = ") {
		t.Fatalf("show %s history missing a dependency audit record for the cycle resolution:\n%s", a, showOut)
	}
}

func TestAcceptanceDanglingLinkIgnoredUntilTargetArrives(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "x")
	cid := canonicalID(t, id)
	ghost := "zzzzzzzzzzzzzzzzzzzzzzzzzz"

	jsonl := `{"_type":"dependency","bead_id":"` + cid + `","depends_on_id":"` + ghost + `","type":"blocks","created_at":"2026-01-01T00:00:00.000Z","removed":false}`
	out := mustImport(t, jsonl)
	if !strings.Contains(out, "Dangling: 1 link(s)") {
		t.Fatalf("expected Dangling: 1 link(s):\n%s", out)
	}

	idAlias := mustDisplayAlias(t, id)
	readyOut, _, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d", code)
	}
	if !strings.Contains(readyOut, idAlias) {
		t.Fatalf("ready output = %q, want %s still Ready while its blocker is dangling", readyOut, idAlias)
	}

	ghostJSONL := `{"_type":"bead","id":"` + ghost + `","namespace":"lm","title":"ghost","status":"open","priority":2,"type":"task","labels":[],"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}`
	mustImport(t, ghostJSONL)

	idAlias = mustDisplayAlias(t, id)
	readyOut, _, code = runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d", code)
	}
	if strings.Contains(readyOut, idAlias) {
		t.Fatalf("ready output = %q, want %s no longer Ready once its blocker (%s) has arrived", readyOut, idAlias, ghost)
	}
	blockedOut, _, code := runCmd(t, []string{"blocked"}, "")
	if code != 0 {
		t.Fatalf("blocked: exit code = %d", code)
	}
	if !strings.Contains(blockedOut, "x") {
		t.Fatalf("blocked output = %q, want %s listed now that its blocker has arrived", blockedOut, id)
	}
}

func TestAcceptanceDefaultLimitTruncationShowsTotal(t *testing.T) {
	initBeadsDir(t)
	const total = output.DefaultLimit + 1
	for range total {
		mustCreateBead(t, "--title", "t")
	}

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	want := "Truncated: showing " + strconv.Itoa(output.DefaultLimit) + " of " + strconv.Itoa(total)
	if !strings.Contains(out, want) {
		t.Fatalf("list stdout does not contain %q:\n%s", want, out)
	}
}

func TestAcceptanceImportDryRunDoesNotChangeAuditLog(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "x")
	cid := canonicalID(t, id)
	before, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show %s: exit code = %d", id, code)
	}

	jsonl := `{"_type":"bead","id":"` + cid + `","namespace":"lm","title":"changed via dry-run","status":"open","priority":2,"type":"task","labels":[],"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2099-01-01T00:00:00.000Z"}`
	out := mustImport(t, jsonl, "--dry-run")
	if !strings.HasPrefix(out, "Dry run: no changes written\n") {
		t.Fatalf("expected the dry-run line first:\n%s", out)
	}

	after, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show %s: exit code = %d", id, code)
	}
	if before != after {
		t.Errorf("dry-run must not change the audit log:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestAcceptanceGateBlocksReadyUntilResolvedAndCannotBeClaimedOrClosed(t *testing.T) {
	t.Run("blocked_target_absent_then_present_after_resolve_gate_itself_never_ready", func(t *testing.T) {
		initBeadsDir(t)
		reviewID := mustCreateBead(t, "--title", "レビュー")
		gateID := mustGateCreateCmd(t, []string{reviewID}, "prod への承認を待つ")

		readyOut, _, code := runCmd(t, []string{"ready"}, "")
		if code != 0 {
			t.Fatalf("ready: exit code = %d", code)
		}
		if strings.Contains(readyOut, "レビュー") {
			t.Errorf("ready output = %q, must not list the gate-blocked review Bead", readyOut)
		}
		if strings.Contains(readyOut, "承認を待つ") {
			t.Errorf("ready output = %q, must not list the Gate Bead itself", readyOut)
		}

		if _, errS, code := runCmd(t, []string{"gate", "resolve", gateID, "--reason", "approved"}, ""); code != 0 {
			t.Fatalf("gate resolve: exit code = %d, stderr = %q", code, errS)
		}

		readyOut2, _, code := runCmd(t, []string{"ready"}, "")
		if code != 0 {
			t.Fatalf("ready: exit code = %d", code)
		}
		if !strings.Contains(readyOut2, "レビュー") {
			t.Errorf("ready output after gate resolve = %q, want the review Bead to appear", readyOut2)
		}
	})
	t.Run("gate_cannot_be_claimed", TestGateCannotBeClaimed)
	t.Run("gate_cannot_be_closed_or_status_closed", TestGateCloseAndUpdateStatusClosedRejected)
}

func TestAcceptanceGateResolveRequiresReasonAuditedAndByAnyActor(t *testing.T) {
	t.Run("resolve_without_reason_rejected", TestGateResolveRequiresReason)
	t.Run("resolve_recorded_with_actor_time_reason_distinct_from_close", TestGateResolveOpensTargetAndReportsNewlyReady)

	t.Run("resolve_by_non_claimant_actor_not_rejected_via_cli", func(t *testing.T) {
		initBeadsDir(t)
		target := mustCreateBead(t, "--title", "target")
		gateID := mustGateCreateCmd(t, []string{target}, "x", "--actor", "creator")

		_, errS, code := runCmd(t, []string{"gate", "resolve", gateID, "--reason", "ok", "--actor", "someone-else"}, "")
		if code != 0 {
			t.Fatalf("gate resolve by a different actor: exit code = %d, stderr = %q", code, errS)
		}
	})
}

func TestAcceptanceGateResolvedOnlyByExternalCallNoPollingNoTimeBasedOpen(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "external CI signal")

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")
	readyOut, _, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d", code)
	}
	if strings.Contains(readyOut, "target") {
		t.Errorf("ready output = %q after simulated time passed with no resolve call, want the Gate to remain open (no time-based auto-open)", readyOut)
	}
	showOut, errS, code := runCmd(t, []string{"show", gateID}, "")
	if code != 0 {
		t.Fatalf("show gate: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(showOut, `status: "open"`) {
		t.Errorf(`show gate = %q after simulated time passed with no resolve call, want status: "open"`, showOut)
	}

	if _, errS, code := runCmd(t, []string{"gate", "resolve", gateID, "--reason", "external CI passed"}, ""); code != 0 {
		t.Fatalf("gate resolve: exit code = %d, stderr = %q", code, errS)
	}
	readyOut, _, code = runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d", code)
	}
	if !strings.Contains(readyOut, "target") {
		t.Errorf("ready output after external gate resolve = %q, want the target now Ready", readyOut)
	}
}

func TestAcceptanceGateRejectCancelsBlockedAndGateAuditsBothAndReportsNewlyReady(t *testing.T) {
	t.Run("reject_without_reason_rejected", TestGateRejectRequiresReason)
	t.Run("reject_on_non_gate_rejected", TestGateRejectNonGateRejected)
	t.Run("in_progress_claim_released_before_cancel", TestGateRejectReleasesInProgressClaimBeforeCancelling)
	t.Run("newly_ready_listed", TestGateRejectReportsNewlyReady)

	t.Run("blocked_and_gate_cancelled_with_actor_time_reason_audited", func(t *testing.T) {
		initBeadsDir(t)
		target := mustCreateBead(t, "--title", "target")
		gateID := mustGateCreateCmd(t, []string{target}, "x")

		if _, errS, code := runCmd(t, []string{"gate", "reject", gateID, "--reason", "not-worth-doing", "--actor", "rejecter"}, ""); code != 0 {
			t.Fatalf("gate reject: exit code = %d, stderr = %q", code, errS)
		}
		reTimestamped := regexp.MustCompile(`^- \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
		for id, field := range map[string]string{gateID: "gate_reject", target: "status"} {
			showOut, errS, code := runCmd(t, []string{"show", id}, "")
			if code != 0 {
				t.Fatalf("show %s: exit code = %d, stderr = %q", id, code, errS)
			}
			if !strings.Contains(showOut, `status: "cancelled"`) {
				t.Errorf("show %s = %q, want status cancelled", id, showOut)
			}
			var found bool
			for line := range strings.SplitSeq(showOut, "\n") {
				if reTimestamped.MatchString(line) && strings.Contains(line, " rejecter field "+field+" = cancelled") && strings.Contains(line, "(not-worth-doing)") {
					found = true
				}
			}
			if !found {
				t.Errorf("show %s history = %q, want a timestamped %s = cancelled line with the actor and reason", id, showOut, field)
			}
		}
	})
}

func TestAcceptanceAhead(t *testing.T) {
	initBeadsDir(t)

	actualCosts := [][2]int{{5000, 2500}, {10000, 5000}, {15000, 7500}}
	actualIDs := make([]string, 0, len(actualCosts))
	for _, c := range actualCosts {
		id := mustCreateBead(t, "--title", "actual", "--type", "task", "--priority", "1")
		if _, errS, code := runCmd(t, []string{"cost", "add", id, "--in", strconv.Itoa(c[0]), "--out", strconv.Itoa(c[1])}, ""); code != 0 {
			t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
		}
		if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
			t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
		}
		actualIDs = append(actualIDs, id)
	}

	depth1CohortAliases := make([]string, 0, 3)
	depth1CohortIDs := make([]string, 0, 3)
	for range 3 {
		alias := mustCreateBead(t, "--title", "depth1-cohort", "--type", "task", "--priority", "3", "--reasoning-depth", "1")
		id := mustCanonicalID(t, alias)
		if _, errS, code := runCmd(t, []string{"cost", "add", id, "--in", "5000", "--out", "5000"}, ""); code != 0 {
			t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
		}
		if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
			t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
		}
		depth1CohortAliases = append(depth1CohortAliases, alias)
		depth1CohortIDs = append(depth1CohortIDs, id)
	}
	depthOutlierAlias := mustCreateBead(t, "--title", "depth1-outlier", "--type", "task", "--priority", "3", "--reasoning-depth", "1")
	depthOutlierID := mustCanonicalID(t, depthOutlierAlias)
	if _, errS, code := runCmd(t, []string{"cost", "add", depthOutlierID, "--in", "12500", "--out", "12500"}, ""); code != 0 {
		t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", depthOutlierID}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	if _, errS, code := runCmd(t, []string{"rework", "add", actualIDs[0], "--cause", "ci", "--reason", "ci bounce"}, ""); code != 0 {
		t.Fatalf("rework add: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"rework", "add", depth1CohortIDs[0], "--cause", "review", "--reason", "review comment"}, ""); code != 0 {
		t.Fatalf("rework add: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"rework", "add", depth1CohortIDs[0], "--cause", "followup", "--reason", "post-merge fix"}, ""); code != 0 {
		t.Fatalf("rework add: exit code = %d, stderr = %q", code, errS)
	}

	d1 := mustCreateBead(t, "--title", "d1-ready", "--type", "task", "--priority", "1")
	d1ID := mustCanonicalID(t, d1)

	e1 := mustCreateBead(t, "--title", "e1-course1", "--type", "task", "--priority", "1")
	e1ID := mustCanonicalID(t, e1)
	if _, errS, code := runCmd(t, []string{"dep", "add", e1ID, d1ID, "--type", "blocks"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}

	f1 := mustCreateBead(t, "--title", "f1-gate-waiting", "--type", "task", "--priority", "1")
	f1ID := mustCanonicalID(t, f1)
	gateOut, errS, code := runCmd(t, withAdjudication(t, []string{"gate", "create", "--subject", "human decision", "--blocks", f1ID, "--kind", "human"}), "")
	if code != 0 {
		t.Fatalf("gate create: exit code = %d, stderr = %q", code, errS)
	}
	gateAlias := mustParseCreatedID(t, gateOut)
	gateID := mustCanonicalID(t, gateAlias)

	d1 = mustDisplayAlias(t, d1ID)
	e1 = mustDisplayAlias(t, e1ID)
	f1 = mustDisplayAlias(t, f1ID)
	gateAlias = mustDisplayAlias(t, gateID)

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("lm ahead: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, "# Ahead\n") {
		t.Fatalf("lm ahead output = %q, want it to start with '# Ahead'", out)
	}
	if !strings.Contains(out, d1+" ") || !strings.Contains(out, "est=15.0k(type/prio)") {
		t.Errorf("lm ahead output = %q, want %s in Ready with est=15.0k(type/prio)", out, d1)
	}
	if !strings.Contains(out, "## Course 1") {
		t.Errorf("lm ahead output = %q, want a ## Course 1 section", out)
	}
	courseIdx := strings.Index(out, "## Course 1")
	gateIdx := strings.Index(out, "## Gate waiting")
	if courseIdx < 0 || gateIdx < 0 || !strings.Contains(out[courseIdx:gateIdx], e1) {
		t.Errorf("lm ahead output = %q, want %s in Course 1", out, e1)
	}
	if !strings.Contains(out[gateIdx:], "- "+gateAlias+" [gate/open/") || !strings.Contains(out[gateIdx:], "  → blocks: "+f1) {
		t.Errorf("lm ahead output = %q, want Gate %s blocking %s in Gate waiting", out, gateAlias, f1)
	}
	if strings.Contains(out, "## Rework") {
		t.Errorf("lm ahead output = %q, must not contain ## Rework without --calibration", out)
	}

	budgetOut, errS, code := runCmd(t, []string{"ahead", "--budget", "300000"}, "")
	if code != 0 {
		t.Fatalf("lm ahead --budget: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(budgetOut, "- Budget: ") {
		t.Errorf("lm ahead --budget output = %q, want a Budget line in ## Summary", budgetOut)
	}

	growsAlias := mustCreateBead(t, "--title", "grows-after-claim", "--type", "task", "--priority", "3")
	growsID := mustCanonicalID(t, growsAlias)
	growsBlocker := mustCreateBead(t, "--title", "grows-blocker", "--type", "task", "--priority", "3")
	if _, errS, code := runCmd(t, []string{"update", growsID, "--claim"}, ""); code != 0 {
		t.Fatalf("update --claim: exit code = %d, stderr = %q", code, errS)
	}
	time.Sleep(2 * time.Millisecond)
	mustCreateBead(t, "--title", "grows-child", "--type", "task", "--priority", "3", "--parent", growsID)
	if _, errS, code := runCmd(t, []string{"dep", "add", growsID, growsBlocker, "--type", "blocks"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}

	depthOutlierAlias = mustDisplayAlias(t, depthOutlierID)
	depth1CohortAliases[0] = mustDisplayAlias(t, depth1CohortIDs[0])
	growsAlias = mustDisplayAlias(t, growsID)
	d1 = mustDisplayAlias(t, d1ID)

	calibrationOut, errS, code := runCmd(t, []string{"ahead", "--calibration"}, "")
	if code != 0 {
		t.Fatalf("lm ahead --calibration: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(calibrationOut, "\n## Calibration\n") {
		t.Errorf("lm ahead --calibration output = %q, want a ## Calibration section", calibrationOut)
	}
	if !strings.Contains(calibrationOut, "\n\n## Calibration\n") {
		t.Errorf("lm ahead --calibration output = %q, want a blank line before ## Calibration", calibrationOut)
	}
	if !strings.HasSuffix(calibrationOut, "\n") || strings.HasSuffix(calibrationOut, "\n\n") {
		t.Errorf("lm ahead --calibration output = %q, want exactly one trailing newline, no trailing blank line", calibrationOut)
	}

	if !strings.Contains(calibrationOut, "\n## Depth calibration\n") {
		t.Errorf("lm ahead --calibration output = %q, want a ## Depth calibration section", calibrationOut)
	}
	if !strings.Contains(calibrationOut, "| 1 | 4 | 10.0k | 10.0k | 13.8k |") {
		t.Errorf("lm ahead --calibration output = %q, want the depth=1 cohort row '| 1 | 4 | 10.0k | 10.0k | 13.8k |'", calibrationOut)
	}
	if !strings.Contains(calibrationOut, "\n## Depth outliers\n") {
		t.Errorf("lm ahead --calibration output = %q, want a ## Depth outliers section", calibrationOut)
	}
	if !strings.Contains(calibrationOut, "| "+depthOutlierAlias+" | 1 | 25.0k | 10.0k | 2.50 |") {
		t.Errorf("lm ahead --calibration output = %q, want the outlier row for %s (ratio 2.50), addressed by its short display ID", calibrationOut, depthOutlierAlias)
	}
	if strings.Contains(calibrationOut, depthOutlierID) {
		t.Errorf("lm ahead --calibration output = %q, must not leak the canonical id %s anywhere in ## Depth outliers", calibrationOut, depthOutlierID)
	}
	if strings.Contains(calibrationOut, "| "+depth1CohortAliases[0]+" | 1 |") {
		t.Errorf("lm ahead --calibration output = %q, must not list the non-outlier cohort Bead %s in ## Depth outliers", calibrationOut, depth1CohortAliases[0])
	}

	if !strings.Contains(calibrationOut, "\n## Rework\n") {
		t.Errorf("lm ahead --calibration output = %q, want a ## Rework section", calibrationOut)
	}
	if !strings.Contains(calibrationOut, "| - | 3 | 1 | 33% | 1 |") {
		t.Errorf("lm ahead --calibration output = %q, want the depth=\"-\" row '| - | 3 | 1 | 33%% | 1 |'", calibrationOut)
	}
	if !strings.Contains(calibrationOut, "| 1 | 4 | 1 | 25% | 2 |") {
		t.Errorf("lm ahead --calibration output = %q, want the depth=1 row '| 1 | 4 | 1 | 25%% | 2 |'", calibrationOut)
	}
	if !strings.Contains(calibrationOut, "| (all) | 7 | 2 | 29% | 3 |") {
		t.Errorf("lm ahead --calibration output = %q, want the (all) row '| (all) | 7 | 2 | 29%% | 3 |'", calibrationOut)
	}
	reworkIdx := strings.Index(calibrationOut, "\n## Rework\n")
	scopeGrowthHeadingIdx := strings.Index(calibrationOut, "\n## Scope growth\n")
	if reworkIdx < 0 || scopeGrowthHeadingIdx < 0 || reworkIdx > scopeGrowthHeadingIdx {
		t.Errorf("lm ahead --calibration output = %q, want ## Rework before ## Scope growth", calibrationOut)
	}

	if !strings.Contains(calibrationOut, "\n## Scope growth\n") {
		t.Errorf("lm ahead --calibration output = %q, want a ## Scope growth section", calibrationOut)
	}
	growsLine := ""
	for line := range strings.SplitSeq(calibrationOut, "\n") {
		if strings.HasPrefix(line, "| "+growsAlias+" | ") {
			growsLine = line
		}
	}
	if !strings.HasSuffix(growsLine, " | 1 | 1 |") {
		t.Errorf("lm ahead --calibration output growsAlias row = %q, want it to end with ' | 1 | 1 |' (1 dep, 1 child after claim)", growsLine)
	}
	if strings.Contains(calibrationOut, growsID) {
		t.Errorf("lm ahead --calibration output = %q, must not leak the canonical id %s anywhere in ## Scope growth", calibrationOut, growsID)
	}
	if strings.Contains(calibrationOut, "| "+d1+" | ") {
		t.Errorf("lm ahead --calibration output = %q, must not list the never-claimed Bead %s in ## Scope growth", calibrationOut, d1)
	}

	exportOut, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(exportOut, "estimate") {
		t.Errorf("export JSONL contains %q, want no estimate key (derived, not persisted)", "estimate")
	}
}

func TestAcceptanceAheadAutoBudget(t *testing.T) {
	initBeadsDir(t)

	t.Run("fewer_than_3_recorded_days_is_na", func(t *testing.T) {
		initBeadsDir(t)
		id := mustCreateBead(t, "--title", "carrier", "--type", "task", "--priority", "1")
		lines := []string{
			mustTokenCostJSONL(id, "d1a", "2026-09-08T01:00:00.000Z", 50000, 50000),
			mustTokenCostJSONL(id, "d2a", "2026-09-09T01:00:00.000Z", 50000, 50000),
		}
		if _, errS, code := runCmd(t, []string{"import", "-"}, strings.Join(lines, "\n")+"\n"); code != 0 {
			t.Fatalf("import: exit code = %d, stderr = %q", code, errS)
		}
		t.Setenv("LM_NOW", "2026-09-10T00:00:00.000Z")
		out, errS, code := runCmd(t, []string{"ahead"}, "")
		if code != 0 {
			t.Fatalf("lm ahead: exit code = %d, stderr = %q", code, errS)
		}
		if !strings.Contains(out, "- Budget: n/a (fewer than 3 recorded days; pass --budget)") {
			t.Errorf("lm ahead output = %q, want the n/a Budget line", out)
		}
	})

	t.Run("5_recorded_days_uses_median", func(t *testing.T) {
		initBeadsDir(t)
		id := mustCreateBead(t, "--title", "carrier", "--type", "task", "--priority", "1")
		lines := []string{
			mustTokenCostJSONL(id, "d1b", "2026-09-05T01:00:00.000Z", 50000, 50000),
			mustTokenCostJSONL(id, "d2b", "2026-09-06T01:00:00.000Z", 100000, 100000),
			mustTokenCostJSONL(id, "d3b", "2026-09-07T01:00:00.000Z", 150000, 150000),
			mustTokenCostJSONL(id, "d4b", "2026-09-08T01:00:00.000Z", 200000, 200000),
			mustTokenCostJSONL(id, "d5b", "2026-09-09T01:00:00.000Z", 250000, 250000),
		}
		if _, errS, code := runCmd(t, []string{"import", "-"}, strings.Join(lines, "\n")+"\n"); code != 0 {
			t.Fatalf("import: exit code = %d, stderr = %q", code, errS)
		}
		t.Setenv("LM_NOW", "2026-09-10T00:00:00.000Z")
		out, errS, code := runCmd(t, []string{"ahead"}, "")
		if code != 0 {
			t.Fatalf("lm ahead: exit code = %d, stderr = %q", code, errS)
		}
		if !strings.Contains(out, "- Budget (auto, median of last 5 recorded days): 300k, ") {
			t.Errorf("lm ahead output = %q, want the auto Budget line with median 300k over 5 days", out)
		}
	})

	t.Run("LM_NOW_excludes_later_records", func(t *testing.T) {
		initBeadsDir(t)
		id := mustCreateBead(t, "--title", "carrier", "--type", "task", "--priority", "1")
		lines := []string{
			mustTokenCostJSONL(id, "d1c", "2026-09-05T01:00:00.000Z", 50000, 50000),
			mustTokenCostJSONL(id, "d2c", "2026-09-06T01:00:00.000Z", 100000, 100000),
			mustTokenCostJSONL(id, "d3c", "2026-09-07T01:00:00.000Z", 150000, 150000),
			mustTokenCostJSONL(id, "d4c", "2099-01-01T01:00:00.000Z", 500000, 500000),
		}
		if _, errS, code := runCmd(t, []string{"import", "-"}, strings.Join(lines, "\n")+"\n"); code != 0 {
			t.Fatalf("import: exit code = %d, stderr = %q", code, errS)
		}
		t.Setenv("LM_NOW", "2026-09-08T00:00:00.000Z")
		out, errS, code := runCmd(t, []string{"ahead"}, "")
		if code != 0 {
			t.Fatalf("lm ahead: exit code = %d, stderr = %q", code, errS)
		}
		if !strings.Contains(out, "- Budget (auto, median of last 3 recorded days): 200k, ") {
			t.Errorf("lm ahead output = %q, want the auto Budget line ignoring the 2099 record (n=3, median 200k)", out)
		}
	})

	t.Run("explicit_budget_skips_auto", func(t *testing.T) {
		initBeadsDir(t)
		id := mustCreateBead(t, "--title", "carrier", "--type", "task", "--priority", "1")
		lines := []string{
			mustTokenCostJSONL(id, "d1d", "2026-09-05T01:00:00.000Z", 50000, 50000),
			mustTokenCostJSONL(id, "d2d", "2026-09-06T01:00:00.000Z", 100000, 100000),
			mustTokenCostJSONL(id, "d3d", "2026-09-07T01:00:00.000Z", 150000, 150000),
		}
		if _, errS, code := runCmd(t, []string{"import", "-"}, strings.Join(lines, "\n")+"\n"); code != 0 {
			t.Fatalf("import: exit code = %d, stderr = %q", code, errS)
		}
		t.Setenv("LM_NOW", "2026-09-10T00:00:00.000Z")
		out, errS, code := runCmd(t, []string{"ahead", "--budget", "10000"}, "")
		if code != 0 {
			t.Fatalf("lm ahead --budget: exit code = %d, stderr = %q", code, errS)
		}
		if !strings.Contains(out, "- Budget: 10.0k, ") {
			t.Errorf("lm ahead --budget output = %q, want the explicit Budget line", out)
		}
		if strings.Contains(out, "auto") {
			t.Errorf("lm ahead --budget output = %q, want no auto-budget derivation when --budget is explicit", out)
		}
	})
}

func mustTokenCostJSONL(beadID, id, recordedAt string, tokensIn, tokensOut int) string {
	return fmt.Sprintf(`{"_type":"token_cost","id":%q,"bead_id":%q,"actor":"test-actor","recorded_at":%q,"tokens_in":%d,"tokens_out":%d}`,
		id, beadID, recordedAt, tokensIn, tokensOut)
}
