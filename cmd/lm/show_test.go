// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

func frontMatterSection(t *testing.T, out string) string {
	t.Helper()
	if !strings.HasPrefix(out, "---\n") {
		t.Fatalf("show output = %q, want it to start with \"---\\n\"", out)
	}
	end := strings.Index(out[4:], "\n---\n")
	if end < 0 {
		t.Fatalf("show output = %q, want a closing \"---\" line", out)
	}
	return out[:4+end+len("\n---\n")]
}

func TestShowFrontMatterStartsAtFirstLineNoBlankLine(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	lines := strings.SplitN(out, "\n", 3)
	if len(lines) < 3 {
		t.Fatalf("show output = %q, want at least 3 lines", out)
	}
	if lines[0] != "---" {
		t.Errorf("show output first line = %q, want \"---\"", lines[0])
	}
	if lines[1] == "" {
		t.Errorf("show output second line is blank, want it to be the first front matter key with no blank line after \"---\"")
	}
}

func TestShowFrontMatterClosingFollowedByHeading(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	displayID := mustDisplayAlias(t, id)

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	fm := frontMatterSection(t, out)
	rest := out[len(fm):]
	if !strings.HasPrefix(rest, "# "+displayID+"\n") {
		t.Errorf("text after front matter = %q, want it to start with \"# %s\\n\"", rest, displayID)
	}
}

func TestShowFrontMatterAllJSONLKeysPresent(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	fm := frontMatterSection(t, out)
	wantOrder := []string{
		"id", "alias", "title", "status", "priority", "effective_priority",
		"effective_priority_source", "type", "labels",
		"claimed_by", "claim_expires_at", "external_refs", "reasoning_depth", "created_at", "updated_at",
		"closed_at", "token_cost", "token_cost_total",
	}
	last := -1
	for _, key := range wantOrder {
		idx := strings.Index(fm, "\n"+key+": ")
		if idx < 0 {
			t.Errorf("front matter = %q, missing key %q", fm, key)
			continue
		}
		if idx < last {
			t.Errorf("front matter = %q, key %q out of order", fm, key)
		}
		last = idx
	}
	if strings.Contains(fm, "\nsummary: ") {
		t.Errorf("front matter = %q, want no \"summary\" key (moved to the ## Summary body section)", fm)
	}
	if strings.Contains(fm, "\nunknown: ") {
		t.Errorf("front matter = %q, want no \"unknown\" key", fm)
	}
}

func TestShowFrontMatterUnsetFieldsAreNull(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	for _, want := range []string{
		`claimed_by: null`, `claim_expires_at: null`, `external_refs: []`, `reasoning_depth: null`,
		`closed_at: null`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(none)\n") == false {
		t.Errorf("show output = %q, want the body sections to still use \"(none)\"", out)
	}
}

func TestShowFrontMatterEscapesQuotesBackslashesAndControlChars(t *testing.T) {
	initBeadsDir(t)
	title := "a \"quote\", a \\backslash\\, a\nline break, and a\ttab"
	id := mustCreateBead(t, "--title", title)

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	want := `title: "a \"quote\", a \\backslash\\, a\nline break, and a\ttab"` + "\n"
	if !strings.Contains(out, want) {
		t.Errorf("show output = %q, want the title line %q", out, want)
	}

	fm := frontMatterSection(t, out)
	for line := range strings.SplitSeq(fm, "\n") {
		if strings.HasPrefix(line, "title: ") && !strings.HasSuffix(line, `"`) {
			t.Errorf("front matter title line = %q, want it to stay on one physical line", line)
		}
	}
}

func TestShowFrontMatterLabelsFlowArray(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--label", "b", "--label", "a")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(out, `labels: ["a", "b"]`+"\n") {
		t.Errorf("show output = %q, want labels: [\"a\", \"b\"]", out)
	}

	unlabeled := mustCreateBead(t, "--title", "u")
	out2, _, code := runCmd(t, []string{"show", unlabeled}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(out2, "labels: []\n") {
		t.Errorf("show output = %q, want labels: []", out2)
	}
}

func TestShowFrontMatterNumbersUnquoted(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "-p", "3", "--reasoning-depth", "2")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	for _, want := range []string{"priority: 3\n", "reasoning_depth: 2\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestShowSummaryShownOnceUnderCompaction(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--description", "the long body", "--summary", "the summary")
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")
	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if got := strings.Count(out, "the summary"); got != 1 {
		t.Errorf("show output = %q, want \"the summary\" to appear exactly once under Compaction, got %d", out, got)
	}
	if strings.Contains(out, "## Summary") {
		t.Errorf("show output = %q, want no \"## Summary\" section under Compaction", out)
	}
	if !strings.Contains(out, "Compacted:") {
		t.Errorf("show output = %q, want a \"Compacted:\" line", out)
	}
	if strings.Contains(out, "the long body") {
		t.Errorf("show output = %q, want the Description withheld under Compaction", out)
	}
	if strings.Contains(out, "summary:") {
		t.Errorf("show output = %q, want no \"summary\" key in the front matter", out)
	}
}

func TestShowSummarySectionShownWhenOpenNotCompacted(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--description", "the long body", "--summary", "the summary")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(out, "## Summary\n\nthe summary\n") {
		t.Errorf("show output = %q, want the Summary section rendered for an open Bead", out)
	}
	if !strings.Contains(out, "the long body") {
		t.Errorf("show output = %q, want the full Description shown for an open Bead", out)
	}
}

func TestShowFullShowsSummarySectionAndBodyUnderCompaction(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--description", "the long body", "--summary", "the summary")
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")
	out, _, code := runCmd(t, []string{"show", "--full", id}, "")
	if code != 0 {
		t.Fatal("show --full failed")
	}
	if !strings.Contains(out, "## Summary\n\nthe summary\n") {
		t.Errorf("show --full output = %q, want the Summary section rendered", out)
	}
	if !strings.Contains(out, "the long body") {
		t.Errorf("show --full output = %q, want the full Description body shown", out)
	}
	if strings.Contains(out, "Compacted:") {
		t.Errorf("show --full output = %q, want no Compacted: notice under --full", out)
	}
}

func TestShowSummarySectionOmittedWhenUnset(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "## Summary") {
		t.Errorf("show output = %q, want no \"## Summary\" section when unset", out)
	}
}

func TestShowSummarySectionOmittedWhenNoSummaryEvenLongAfterClosing(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--description", "the long body")
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")
	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "## Summary") {
		t.Errorf("show output = %q, want no \"## Summary\" section when the Bead has no attached summary, even long after closing", out)
	}
	if !strings.Contains(out, "the long body") {
		t.Errorf("show output = %q, want the full description shown since a Bead with an unset summary never compacts", out)
	}
	if strings.Contains(out, "Compacted:") {
		t.Errorf("show output = %q, want no Compacted: notice when the Bead has no attached summary", out)
	}
}

func TestShowIdempotentAcrossRepeatedCalls(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--description", "d", "--summary", "s")

	out1, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed (1st)")
	}
	out2, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed (2nd)")
	}
	if out1 != out2 {
		t.Errorf("show output not idempotent:\n--- 1st ---\n%s\n--- 2nd ---\n%s", out1, out2)
	}
}

func linksSection(t *testing.T, out string) string {
	t.Helper()
	start := strings.Index(out, "## Dependencies")
	if start < 0 {
		t.Fatalf("show output missing ## Dependencies:\n%s", out)
	}
	rest := out[start:]
	before, _, ok := strings.Cut(rest, "## History")
	if !ok {
		t.Fatalf("show output = %q, want a ## History section after ## Dependencies", out)
	}
	return before
}

func historySection(t *testing.T, out string) string {
	t.Helper()
	start := strings.Index(out, "## History")
	if start < 0 {
		t.Fatalf("show output missing ## History:\n%s", out)
	}
	return out[start:]
}

func TestShowLinksOneLinePerLink(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker task")
	blocked := mustCreateBead(t, "--title", "blocked task", "--blocked-by", blocker)
	blocker = mustDisplayAlias(t, blocker)

	out, _, code := runCmd(t, []string{"show", blocked}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	links := linksSection(t, out)
	want := "- blocks ← " + blocker + " [open/P2] blocker task"
	if !strings.Contains(links, want) {
		t.Errorf("links section = %q, want line %q", links, want)
	}
}

func TestShowLinksSentLinkShownWithArrow(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker task")
	blocked := mustCreateBead(t, "--title", "blocked task", "--blocked-by", blocker)
	blocked = mustDisplayAlias(t, blocked)

	out, _, code := runCmd(t, []string{"show", blocker}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	links := linksSection(t, out)
	want := "- blocks → " + blocked + " [open/P2] blocked task"
	if !strings.Contains(links, want) {
		t.Errorf("links section = %q, want line %q", links, want)
	}
}

func TestShowLinksSentParentChildShownWithArrow(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "epic")
	child := mustCreateBead(t, "--title", "step", "--parent", parent)

	out, _, code := runCmd(t, []string{"show", parent}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	links := linksSection(t, out)
	if !strings.Contains(links, "- parent-child → ") || !strings.Contains(links, " [open/P2] step") {
		t.Errorf("links section = %q, want a \"parent-child →\" line for %q", links, child)
	}
}

func TestShowLinksReceivedBeforeSent(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	middle := mustCreateBead(t, "--title", "middle", "--blocked-by", blocker)
	downstream := mustCreateBead(t, "--title", "blocked by middle")
	if _, errS, code := runCmd(t, []string{"dep", "add", downstream, middle, "--type", "blocks"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", middle}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	links := linksSection(t, out)
	receivedIdx := strings.Index(links, "blocks ← ")
	sentIdx := strings.Index(links, "blocks → ")
	if receivedIdx < 0 || sentIdx < 0 {
		t.Fatalf("links section = %q, want both a received and a sent \"blocks\" line", links)
	}
	if receivedIdx > sentIdx {
		t.Errorf("links section = %q, want the received (←) line before the sent (→) line", links)
	}
}

func TestShowLinksDanglingReference(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	cid := canonicalID(t, id)
	ghost := "zzzzzzzzzzzzzzzzzzzzzzzzzz"
	jsonl := `{"_type":"dependency","bead_id":"` + cid + `","depends_on_id":"` + ghost + `","type":"blocks","created_at":"2026-01-01T00:00:00.000Z","removed":false}`
	mustImport(t, jsonl)

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	links := linksSection(t, out)
	want := "- blocks ← " + ghost + " (dangling)"
	if !strings.Contains(links, want) {
		t.Errorf("links section = %q, want line %q", links, want)
	}
}

func TestShowLinksRemovedHiddenByDefaultShownWithAllDeps(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	blocked := mustCreateBead(t, "--title", "blocked", "--blocked-by", blocker)
	if _, errS, code := runCmd(t, []string{"dep", "remove", blocked, blocker}, ""); code != 0 {
		t.Fatalf("dep remove: exit code = %d, stderr = %q", code, errS)
	}
	blocker = mustDisplayAlias(t, blocker)

	out, _, code := runCmd(t, []string{"show", blocked}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(linksSection(t, out), blocker) {
		t.Errorf("links section = %q, want the removed link hidden by default", linksSection(t, out))
	}

	allOut, _, code := runCmd(t, []string{"show", blocked, "--all-deps"}, "")
	if code != 0 {
		t.Fatal("show --all-deps failed")
	}
	allDeps := linksSection(t, allOut)
	want := "- blocks ← " + blocker + " [open/P2] blocker  (removed)"
	if !strings.Contains(allDeps, want) {
		t.Errorf("links section (--all-deps) = %q, want line %q", allDeps, want)
	}
}

func TestShowLinksEmptyIsNoneLine(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	links := strings.TrimSpace(linksSection(t, out))
	if links != "## Dependencies\n(none)" {
		t.Errorf("links section = %q, want %q", links, "## Dependencies\n(none)")
	}
}

func childrenSection(out string) (string, bool) {
	start := strings.Index(out, "## Children")
	if start < 0 {
		return "", false
	}
	rest := out[start:]
	if before, _, ok := strings.Cut(rest, "## History"); ok {
		return before, true
	}
	return rest, true
}

func TestShowChildrenOmittedWhenNoChildren(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "solo")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "## Children") {
		t.Errorf("show output = %q, want no ## Children section for a Bead with no children", out)
	}
}

func TestShowChildrenOmittedWhenOnlyNonParentChildSentLinks(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked", "--blocked-by", blocker)

	out, _, code := runCmd(t, []string{"show", blocker}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "## Children") {
		t.Errorf("show output = %q, want no ## Children section for a Bead whose only sent links are not parent-child", out)
	}
}

func TestShowChildrenSingleChildCounted(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic", "--type", "task")
	mustCreateBead(t, "--title", "child", "--parent", epic)

	out, _, code := runCmd(t, []string{"show", epic}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	section, ok := childrenSection(out)
	if !ok {
		t.Fatalf("show output = %q, want a ## Children section", out)
	}
	want := "## Children\n\nopen 1"
	if got := strings.TrimSpace(section); got != want {
		t.Errorf("children section = %q, want %q", got, want)
	}
}

func TestShowChildrenMultipleStatusesCountedInFixedOrderZerosOmitted(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic", "--type", "task")
	mustCreateBead(t, "--title", "open-child", "--parent", epic)
	inProgressChild := mustCreateBead(t, "--title", "in-progress-child", "--parent", epic)
	closedChild1 := mustCreateBead(t, "--title", "closed-child-1", "--parent", epic)
	closedChild2 := mustCreateBead(t, "--title", "closed-child-2", "--parent", epic)
	closedChild3 := mustCreateBead(t, "--title", "closed-child-3", "--parent", epic)

	if _, errS, code := runCmd(t, []string{"update", inProgressChild, "--claim"}, ""); code != 0 {
		t.Fatalf("claim in-progress-child: exit code = %d, stderr = %q", code, errS)
	}
	for _, id := range []string{closedChild1, closedChild2, closedChild3} {
		if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
			t.Fatalf("close %s: exit code = %d, stderr = %q", id, code, errS)
		}
	}

	out, _, code := runCmd(t, []string{"show", epic}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	section, ok := childrenSection(out)
	if !ok {
		t.Fatalf("show output = %q, want a ## Children section", out)
	}
	want := "## Children\n\nopen 1, in_progress 1, closed 3"
	if got := strings.TrimSpace(section); got != want {
		t.Errorf("children section = %q, want %q", got, want)
	}
}

func TestShowChildrenCountsDirectChildrenOnlyNotGrandchildren(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic", "--type", "task")
	child := mustCreateBead(t, "--title", "child", "--parent", epic)
	mustCreateBead(t, "--title", "grandchild", "--parent", child)

	out, _, code := runCmd(t, []string{"show", epic}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	section, ok := childrenSection(out)
	if !ok {
		t.Fatalf("show output = %q, want a ## Children section", out)
	}
	want := "## Children\n\nopen 1"
	if got := strings.TrimSpace(section); got != want {
		t.Errorf("children section = %q, want %q", got, want)
	}
}

func TestShowChildrenExcludesRemovedParentChildLinkEvenWithAllDeps(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic", "--type", "task")
	child := mustCreateBead(t, "--title", "child", "--parent", epic)
	if _, errS, code := runCmd(t, []string{"dep", "remove", child, epic, "--type", "parent-child"}, ""); code != 0 {
		t.Fatalf("dep remove: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", epic}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "## Children") {
		t.Errorf("show output = %q, want no ## Children section once the only parent-child link is removed", out)
	}

	allOut, _, code := runCmd(t, []string{"show", epic, "--all-deps"}, "")
	if code != 0 {
		t.Fatal("show --all-deps failed")
	}
	if strings.Contains(allOut, "## Children") {
		t.Errorf("show --all-deps output = %q, want the removed child still excluded from ## Children", allOut)
	}
}

func TestShowChildrenSkipsDanglingChild(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic", "--type", "task")
	ghost := "zzzzzzzzzzzzzzzzzzzzzzzzzz"
	jsonl := `{"_type":"dependency","bead_id":"` + ghost + `","depends_on_id":"` + epic + `","type":"parent-child","created_at":"2026-01-01T00:00:00.000Z","removed":false}`
	mustImport(t, jsonl)

	out, _, code := runCmd(t, []string{"show", epic}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "## Children") {
		t.Errorf("show output = %q, want no ## Children section when the only child reference is dangling", out)
	}
}

func derivedSection(out string) (string, bool) {
	start := strings.Index(out, "## Derived")
	if start < 0 {
		return "", false
	}
	rest := out[start:]
	if before, _, ok := strings.Cut(rest, "## History"); ok {
		return before, true
	}
	return rest, true
}

func TestShowDerivedOmittedWhenNoDiscoveredFromLinks(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "solo")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "## Derived") {
		t.Errorf("show output = %q, want no ## Derived section for a Bead with no discovered-from targets", out)
	}
}

func TestShowDerivedOmittedForNonDiscoveredFromSentLinks(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked", "--blocked-by", blocker)

	out, _, code := runCmd(t, []string{"show", blocker}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "## Derived") {
		t.Errorf("show output = %q, want no ## Derived section for a Bead whose only sent links are not discovered-from", out)
	}
}

func TestShowDerivedListsDiscoveredFromTargetWithAliasAndStatus(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")
	found := mustCreateBead(t, "--title", "found while working")
	if _, errS, code := runCmd(t, []string{"dep", "add", found, origin, "--type", "discovered-from"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}
	found = mustDisplayAlias(t, found)

	out, _, code := runCmd(t, []string{"show", origin}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	section, ok := derivedSection(out)
	if !ok {
		t.Fatalf("show output = %q, want a ## Derived section", out)
	}
	want := "- " + found + " [open/P2] found while working"
	if !strings.Contains(section, want) {
		t.Errorf("derived section = %q, want line %q", section, want)
	}
}

func TestShowDerivedDanglingReference(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")
	ghost := "zzzzzzzzzzzzzzzzzzzzzzzzzz"
	jsonl := `{"_type":"dependency","bead_id":"` + ghost + `","depends_on_id":"` + origin + `","type":"discovered-from","created_at":"2026-01-01T00:00:00.000Z","removed":false}`
	mustImport(t, jsonl)

	out, _, code := runCmd(t, []string{"show", origin}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	section, ok := derivedSection(out)
	if !ok {
		t.Fatalf("show output = %q, want a ## Derived section", out)
	}
	want := "- " + ghost + " (dangling)"
	if !strings.Contains(section, want) {
		t.Errorf("derived section = %q, want line %q", section, want)
	}
}

func TestShowDerivedOmittedWhenOnlyRemovedDiscoveredFromLink(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")
	found := mustCreateBead(t, "--title", "found while working")
	if _, errS, code := runCmd(t, []string{"dep", "add", found, origin, "--type", "discovered-from"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"dep", "remove", found, origin, "--type", "discovered-from"}, ""); code != 0 {
		t.Fatalf("dep remove: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", origin}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "## Derived") {
		t.Errorf("show output = %q, want no ## Derived section once the only discovered-from link is removed", out)
	}
}

func TestShowHistoryCreationFoldedIntoCreatedLine(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--description", "d")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	hist := historySection(t, out)
	if !strings.Contains(hist, "test-actor created (namespace, title, status, priority, type, description)") {
		t.Errorf("history section = %q, want a folded created(...) line", hist)
	}
	if strings.Contains(hist, "field namespace") || strings.Contains(hist, "field title") {
		t.Errorf("history section = %q, want the creation fields folded, not shown individually", hist)
	}
}

func TestShowHistoryOneLinePerRecordChronological(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	if _, errS, code := runCmd(t, []string{"update", "--title", "t2", "--reason", "renaming", id}, ""); code != 0 {
		t.Fatalf("update: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"update", "--reason", "status-quo note", id}, ""); code != 0 {
		t.Fatalf("reason-only update: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	hist := historySection(t, out)
	wantRename := "field title = t2  (renaming)"
	wantNote := "note  (status-quo note)"
	if !strings.Contains(hist, wantRename) {
		t.Errorf("history section = %q, want line %q", hist, wantRename)
	}
	if !strings.Contains(hist, wantNote) {
		t.Errorf("history section = %q, want line %q", hist, wantNote)
	}
	if strings.Index(hist, wantRename) > strings.Index(hist, wantNote) {
		t.Errorf("history section = %q, want chronological order (rename before note)", hist)
	}
}

func TestShowHistoryValueAndReasonTruncatedAt80Runes(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	longReason := strings.Repeat("a", 90)
	if _, errS, code := runCmd(t, []string{"update", "--title", "t2", "--reason", longReason, id}, ""); code != 0 {
		t.Fatalf("update: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	hist := historySection(t, out)
	want := "(" + strings.Repeat("a", 80) + "…)"
	if !strings.Contains(hist, want) {
		t.Errorf("history section = %q, want the reason truncated to 80 runes with a trailing …", hist)
	}
	if strings.Contains(hist, strings.Repeat("a", 81)) {
		t.Errorf("history section = %q, want no run of 81+ untruncated 'a's", hist)
	}
	wantLast := "Truncated: history values cut at 80 characters; use `lm show --full` to see them in full"
	lines := strings.Split(strings.TrimRight(hist, "\n"), "\n")
	if lines[len(lines)-1] != wantLast {
		t.Errorf("history section = %q, want last line %q", hist, wantLast)
	}
}

func TestShowHistoryNoTruncatedLineWhenNothingCut(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"update", "--title", "t2", "--reason", strings.Repeat("a", 80), id}, ""); code != 0 {
		t.Fatalf("update: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if strings.Contains(out, "Truncated:") {
		t.Errorf("show output = %q, want no Truncated line when no history value exceeds 80 runes", out)
	}
}

func TestShowHistoryTruncatedLineForLongValue(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"update", "--title", strings.Repeat("b", 90), id}, ""); code != 0 {
		t.Fatalf("update: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.HasSuffix(strings.TrimRight(historySection(t, out), "\n"), "\nTruncated: history values cut at 80 characters; use `lm show --full` to see them in full") {
		t.Errorf("show output = %q, want the history Truncated line after a value cut at 80 runes", out)
	}
}

func TestShowHistoryDependencyValueFormatted(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	blocked := mustCreateBead(t, "--title", "blocked")
	if _, errS, code := runCmd(t, []string{"dep", "add", blocked, blocker, "--type", "blocks"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}
	blocker = mustDisplayAlias(t, blocker)

	out, _, code := runCmd(t, []string{"show", blocked}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	hist := historySection(t, out)
	want := "dependency = blocks ← " + blocker
	if !strings.Contains(hist, want) {
		t.Errorf("history section = %q, want the dependency value formatted as %q, not raw JSON", hist, want)
	}
	if strings.Contains(hist, "depends_on_id") {
		t.Errorf("history section = %q, want no raw JSON in the dependency value", hist)
	}
}

func TestShowTokenCostTotalNoChildrenEqualsTokenCost(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"cost", "add", id, "--in", "10", "--out", "5"}, ""); code != 0 {
		t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	for _, want := range []string{"token_cost: 15\n", "token_cost_total: 15\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestShowTokenCostTotalSumsChildAndGrandchild(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic")
	child := mustCreateBead(t, "--title", "child")
	grandchild := mustCreateBead(t, "--title", "grandchild")
	if _, errS, code := runCmd(t, []string{"dep", "add", child, epic, "--type", "parent-child"}, ""); code != 0 {
		t.Fatalf("dep add child epic: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"dep", "add", grandchild, child, "--type", "parent-child"}, ""); code != 0 {
		t.Fatalf("dep add grandchild child: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"cost", "add", epic, "--in", "1", "--out", "1"}, ""); code != 0 {
		t.Fatalf("cost add epic: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"cost", "add", child, "--in", "100", "--out", "50"}, ""); code != 0 {
		t.Fatalf("cost add child: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"cost", "add", grandchild, "--in", "1000", "--out", "500"}, ""); code != 0 {
		t.Fatalf("cost add grandchild: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", epic}, "")
	if code != 0 {
		t.Fatal("show failed")
	}

	for _, want := range []string{"token_cost: 2\n", "token_cost_total: 1652\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestShowTokenCostTotalExcludesRemovedParentChild(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic")
	child := mustCreateBead(t, "--title", "child")
	if _, errS, code := runCmd(t, []string{"dep", "add", child, epic, "--type", "parent-child"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"cost", "add", child, "--in", "100", "--out", "50"}, ""); code != 0 {
		t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"dep", "remove", child, epic, "--type", "parent-child"}, ""); code != 0 {
		t.Fatalf("dep remove: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"show", epic}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(out, "token_cost_total: 0\n") {
		t.Errorf("show output = %q, want token_cost_total: 0 once the parent-child link is removed", out)
	}
}

func TestShowTokenCostTotalNotInExport(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic")
	child := mustCreateBead(t, "--title", "child")
	if _, errS, code := runCmd(t, []string{"dep", "add", child, epic, "--type", "parent-child"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"cost", "add", child, "--in", "10", "--out", "5"}, ""); code != 0 {
		t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "token_cost_total") {
		t.Errorf("export output = %q, want no token_cost_total (derived value, not exported)", out)
	}
}

func TestShowTokenCostTotalIdempotent(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "epic")
	child := mustCreateBead(t, "--title", "child")
	if _, errS, code := runCmd(t, []string{"dep", "add", child, epic, "--type", "parent-child"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"cost", "add", child, "--in", "10", "--out", "5"}, ""); code != 0 {
		t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
	}

	out1, _, code := runCmd(t, []string{"show", epic}, "")
	if code != 0 {
		t.Fatal("show failed (1st)")
	}
	out2, _, code := runCmd(t, []string{"show", epic}, "")
	if code != 0 {
		t.Fatal("show failed (2nd)")
	}
	if out1 != out2 {
		t.Errorf("show output differs between two calls:\n1st: %q\n2nd: %q", out1, out2)
	}
}

func pickupLineFromShow(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			if i+2 >= len(lines) {
				t.Fatalf("show output ends right after the heading:\n%s", out)
			}
			if lines[i+1] != "" {
				t.Fatalf("show output line after heading = %q, want a blank line", lines[i+1])
			}
			row := lines[i+2]
			if strings.HasPrefix(row, "Pickup:") || strings.HasPrefix(row, "Waiting:") {
				return row
			}
			return ""
		}
	}
	t.Fatalf("show output missing a \"# \" heading:\n%s", out)
	return ""
}

func TestShowPickupMatchesAheadReleaseETA(t *testing.T) {
	initBeadsDir(t)
	orig := time.Local
	time.Local = time.FixedZone("JST", 9*60*60)
	t.Cleanup(func() { time.Local = orig })

	for i := range 20 {
		id := mustCreateBead(t, "--title", fmt.Sprintf("filler-%d", i))
		if i < 15 {
			if _, errS, code := runCmd(t, []string{"update", id, "--external-ref", fmt.Sprintf("https://github.com/o/r/pull/%d", i)}, ""); code != 0 {
				t.Fatalf("update filler %d external-ref: exit code = %d, stderr = %q", i, code, errS)
			}
		}
		if _, errS, code := runCmd(t, []string{"close", id, "--summary", "s"}, ""); code != 0 {
			t.Fatalf("close filler %d: exit code = %d, stderr = %q", i, code, errS)
		}
	}

	epic := mustCreateBead(t, "--title", "release", "--type", "task", "--label", "milestone:test", "--priority", "1")
	mustCreateBead(t, "--title", "c1", "--parent", epic, "--priority", "1")
	mustCreateBead(t, "--title", "c2", "--parent", epic, "--priority", "1")
	self := mustCreateBead(t, "--title", "self", "--priority", "2")

	aheadOut, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("ahead: exit code = %d, stderr = %q", code, errS)
	}
	body := aheadSection(t, aheadOut, "## Milestones")
	if len(body) != 2 {
		t.Fatalf("## Milestones body = %q, want the Window line and exactly the release line", body)
	}
	etaPattern := regexp.MustCompile(`  eta=(.+)$`)
	m := etaPattern.FindStringSubmatch(body[1])
	if m == nil {
		t.Fatalf("## Milestones line = %q, want an eta= range", body[1])
	}
	wantEta := m[1]
	if strings.Contains(wantEta, "発散") {
		t.Fatalf("## Milestones eta = %q, want a non-diverging range so this test exercises the shared calculation", wantEta)
	}

	showOut, errS, code := runCmd(t, []string{"show", self}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	got := pickupLineFromShow(t, showOut)
	want := "Pickup: ahead=2 eta=" + wantEta
	if got != want {
		t.Errorf("show pickup line = %q, want %q", got, want)
	}
}

func TestShowPickupOverrunWhenInterruptsOutrunClose(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "interrupt", "--priority", "1")
	self := mustCreateBead(t, "--title", "self", "--priority", "2")

	out, errS, code := runCmd(t, []string{"show", self}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	got := pickupLineFromShow(t, out)
	want := "Pickup: ahead=1 eta=不定(割り込み超過)"
	if got != want {
		t.Errorf("show pickup line = %q, want %q", got, want)
	}
}

func TestShowPickupNextWhenNothingAhead(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "only")

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if got := pickupLineFromShow(t, out); got != "Pickup: ahead=0 next" {
		t.Errorf("show pickup line = %q, want %q", got, "Pickup: ahead=0 next")
	}
}

func TestShowWaitingListsUnfinishedBlocker(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	blockerAlias := mustDisplayAlias(t, blocker)
	blocked := mustCreateBead(t, "--title", "blocked", "--blocked-by", blocker)

	out, errS, code := runCmd(t, []string{"show", blocked}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	want := "Waiting: " + blockerAlias
	if got := pickupLineFromShow(t, out); got != want {
		t.Errorf("show pickup line = %q, want %q", got, want)
	}
}

func TestShowPickupOmittedForNonOpenAndGate(t *testing.T) {
	initBeadsDir(t)

	inProgress := mustCreateBead(t, "--title", "in-progress")
	if _, errS, code := runCmd(t, []string{"update", inProgress, "--claim"}, ""); code != 0 {
		t.Fatalf("update --claim: exit code = %d, stderr = %q", code, errS)
	}
	closedID := mustCreateBead(t, "--title", "closed")
	if _, errS, code := runCmd(t, []string{"close", closedID, "--summary", "s"}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	cancelled := mustCreateBead(t, "--title", "cancelled")
	if _, errS, code := runCmd(t, []string{"update", cancelled, "--status", "cancelled"}, ""); code != 0 {
		t.Fatalf("update --status cancelled: exit code = %d, stderr = %q", code, errS)
	}
	gate := mustCreateBead(t, "--title", "gate", "--type", "gate", "--label", "kind:human")

	for _, id := range []string{inProgress, closedID, cancelled, gate} {
		out, errS, code := runCmd(t, []string{"show", id}, "")
		if code != 0 {
			t.Fatalf("show %s: exit code = %d, stderr = %q", id, code, errS)
		}
		if got := pickupLineFromShow(t, out); got != "" {
			t.Errorf("show %s pickup line = %q, want none", id, got)
		}
	}
}

func TestShowPickupFailsWhenReadyQueryErrors(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	dropTable(t, "dependencies")

	_, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 1 {
		t.Fatalf("show: exit code = %d, stderr = %q, want 1", code, errS)
	}
	if !strings.HasPrefix(errS, "Error:") {
		t.Errorf("show stderr = %q, want it to start with \"Error:\"", errS)
	}
}

func TestShowPickupFailsWhenAheadBeadCreatedAtCorrupt(t *testing.T) {
	initBeadsDir(t)
	ahead := mustCreateBead(t, "--title", "ahead", "--priority", "1")
	corruptCreatedAt(t, ahead)
	self := mustCreateBeadAlias(t, "--title", "self", "--priority", "2")

	_, errS, code := runCmd(t, []string{"show", self}, "")
	if code != 1 {
		t.Fatalf("show: exit code = %d, stderr = %q, want 1", code, errS)
	}
	if !strings.HasPrefix(errS, "Error:") {
		t.Errorf("show stderr = %q, want it to start with \"Error:\"", errS)
	}
}

func TestShowPickupFailsWhenThroughputCreatedAtCorrupt(t *testing.T) {
	initBeadsDir(t)
	closedID := mustCreateBead(t, "--title", "closed")
	if _, errS, code := runCmd(t, []string{"close", closedID, "--summary", "s"}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	corruptCreatedAt(t, closedID)
	self := mustCreateBeadAlias(t, "--title", "self")

	_, errS, code := runCmd(t, []string{"show", self}, "")
	if code != 1 {
		t.Fatalf("show: exit code = %d, stderr = %q, want 1", code, errS)
	}
	if !strings.HasPrefix(errS, "Error:") {
		t.Errorf("show stderr = %q, want it to start with \"Error:\"", errS)
	}
}
