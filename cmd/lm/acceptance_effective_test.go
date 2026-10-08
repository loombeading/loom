// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mustShowField(t *testing.T, id, want string) {
	t.Helper()
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show %s: exit code = %d, stderr = %q", id, code, errS)
	}
	if !strings.Contains(out, want) {
		t.Errorf("show %s missing %q:\n%s", id, want, out)
	}
}

func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, errS, code := runCmd(t, args, "")
	if code != 0 {
		t.Fatalf("%v: exit code = %d, stderr = %q", args, code, errS)
	}
	return out
}

func TestAcceptanceEffectivePriorityPropagatesToBlockersTransitivelyAndChildren(t *testing.T) {
	t.Run("p2_blocker_of_p0_before_p1_in_ready_and_claim", TestReadyShowsEffectivePriorityAndOrdersByIt)

	t.Run("transitive_blocker_is_raised", func(t *testing.T) {
		initBeadsDir(t)
		b := mustCreateBead(t, "--title", "b", "--priority", "3")
		a := mustCreateBead(t, "--title", "a", "--priority", "3", "--blocked-by", b)
		mustCreateBead(t, "--title", "urgent", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", a)
		mustShowField(t, b, "effective_priority: 0\n")
	})

	t.Run("child_of_open_p0_parent_is_raised", func(t *testing.T) {
		initBeadsDir(t)
		parent := mustCreateBead(t, "--title", "parent", "--expedite", "1h", "--reason", "interrupt")
		child := mustCreateBead(t, "--title", "child", "--priority", "3", "--parent", parent)
		mustShowField(t, child, "effective_priority: 0\n")
		mustShowField(t, child, `effective_priority_source: "`+mustDisplayAlias(t, parent)+`"`)
	})
}

func TestAcceptanceEffectivePriorityRevertsAndStoredPriorityUnchanged(t *testing.T) {
	t.Run("downstream_closed_and_export_has_no_effective", TestShowEffectivePriority)

	revert := map[string]func(t *testing.T, blocker, p0 string){
		"downstream_cancelled": func(t *testing.T, _, p0 string) {
			t.Helper()
			mustRun(t, "update", p0, "--status", "cancelled", "--reason", "drop")
		},
		"link_removed": func(t *testing.T, blocker, p0 string) {
			t.Helper()
			mustRun(t, "dep", "remove", p0, blocker)
		},
	}
	for name, act := range revert {
		t.Run(name, func(t *testing.T) {
			initBeadsDir(t)
			blocker := mustCreateBead(t, "--title", "blocker", "--priority", "2")
			p0 := mustCreateBead(t, "--title", "urgent", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
			mustShowField(t, blocker, "effective_priority: 0\n")
			act(t, blocker, p0)
			mustShowField(t, blocker, "priority: 2\neffective_priority: 2\neffective_priority_source: null\n")
		})
	}

	t.Run("parent_cancelled", func(t *testing.T) {
		initBeadsDir(t)
		parent := mustCreateBead(t, "--title", "parent", "--expedite", "1h", "--reason", "interrupt")
		child := mustCreateBead(t, "--title", "child", "--priority", "3", "--parent", parent)
		mustShowField(t, child, "effective_priority: 0\n")
		mustRun(t, "update", parent, "--status", "cancelled", "--reason", "drop")
		mustShowField(t, child, "effective_priority: 3\n")
	})

	t.Run("stored_priority_unchanged_in_show_and_export", func(t *testing.T) {
		initBeadsDir(t)
		blocker := mustCreateBead(t, "--title", "blocker", "--priority", "2")
		mustCreateBead(t, "--title", "urgent", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
		mustShowField(t, blocker, "priority: 2\neffective_priority: 0\n")
		out := mustRun(t, "export")
		var line string
		for l := range strings.SplitSeq(out, "\n") {
			if strings.Contains(l, `"`+blocker+`"`) && strings.Contains(l, `"title":"blocker"`) {
				line = l
			}
		}
		if !strings.Contains(line, `"priority":2`) || strings.Contains(out, "effective") {
			t.Errorf("export = %q, want the blocker at stored priority 2 and no effective priority", out)
		}
	})
}

func TestAcceptanceEffMarkOnlyOnRaisedRowsNamesEarliestSource(t *testing.T) {
	t.Run("eff_only_on_raised_rows", TestReadyShowsEffectivePriorityAndOrdersByIt)

	t.Run("source_is_earliest_created", func(t *testing.T) {
		initBeadsDir(t)
		blocker := mustCreateBead(t, "--title", "blocker", "--priority", "3")
		first := mustCreateBead(t, "--title", "first p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
		time.Sleep(20 * time.Millisecond)
		mustCreateBead(t, "--title", "second p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
		line, _ := lineOf(t, mustRun(t, "ready"), "blocker")
		if !strings.HasSuffix(line, "  eff=P0("+mustDisplayAlias(t, first)+")") {
			t.Errorf("ready line = %q, want eff=P0 naming the earlier-created downstream", line)
		}
	})
}

func aliasesInOrder(out string, want []string) []string {
	var got []string
	for line := range strings.SplitSeq(out, "\n") {
		for _, a := range want {
			if strings.Contains(line, a+" ") {
				got = append(got, a)
			}
		}
	}
	return got
}

func TestAcceptanceEffectiveOrderAcrossListingsAndAheadUsesStoredPriority(t *testing.T) {
	t.Run("blocked_order", TestBlockedOrdersByEffectivePriority)
	t.Run("gate_order", TestGateListOrdersByEffectivePriority)

	t.Run("ready_matches_list_sort_priority", func(t *testing.T) {
		initBeadsDir(t)
		p2 := mustCreateBead(t, "--title", "plain p2", "--priority", "2")
		p1 := mustCreateBead(t, "--title", "plain p1", "--priority", "1")
		blocker := mustCreateBead(t, "--title", "blocker p3", "--priority", "3")
		mustCreateBead(t, "--title", "urgent p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
		ready := []string{mustDisplayAlias(t, blocker), mustDisplayAlias(t, p1), mustDisplayAlias(t, p2)}

		gotReady := aliasesInOrder(mustRun(t, "ready"), ready)
		gotList := aliasesInOrder(mustRun(t, "list", "--sort", "priority"), ready)
		if strings.Join(gotReady, ",") != strings.Join(ready, ",") || strings.Join(gotList, ",") != strings.Join(ready, ",") {
			t.Errorf("ready order = %v, list --sort priority order = %v, want both %v", gotReady, gotList, ready)
		}
	})

	t.Run("ahead_estimates_from_stored_priority_cohort", func(t *testing.T) {
		initBeadsDir(t)
		for _, c := range []struct{ prio, cost int }{{1, 90000}, {3, 3000}} {
			for range 3 {
				id := mustCreateBead(t, "--title", "closed", "--type", "task", "--priority", strconv.Itoa(c.prio))
				mustRun(t, "cost", "add", id, "--in", strconv.Itoa(c.cost), "--out", "0")
				mustRun(t, "close", id)
			}
		}
		blocker := mustCreateBead(t, "--title", "blocker p3", "--type", "task", "--priority", "3")
		mustCreateBead(t, "--title", "urgent p0", "--type", "task", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
		line, _ := lineOf(t, mustRun(t, "ahead"), "blocker p3")
		if !strings.Contains(line, "est=3.00k(type/prio)") {
			t.Errorf("ahead line = %q, want the P3 cohort estimate est=3.00k(type/prio)", line)
		}
	})
}

func TestAcceptanceCancellingBlocksPrerequisiteRefusesUnfinishedDownstream(t *testing.T) {
	initBeadsDir(t)
	pre := mustCreateBead(t, "--title", "pre", "--priority", "3")
	down := mustCreateBead(t, "--title", "down", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", pre)

	out, errOut, code := runCmd(t, []string{"update", pre, "--status", "cancelled", "--reason", "not needed"}, "")
	wantReason := "unfinished dependents: " + mustDisplayAlias(t, down)
	if code != 1 || !strings.Contains(out, "Rejected: ") || !strings.Contains(out, wantReason) || !strings.Contains(errOut, "lm dep remove と lm dep add") {
		t.Errorf("cancel = (%q, %q, %d), want Rejected naming the downstream, the re-link hint on stderr, exit 1", out, errOut, code)
	}
	mustShowField(t, pre, "status: \"open\"\n")

	mustRun(t, "update", down, pre, "--status", "cancelled", "--reason", "not needed")
	mustShowField(t, pre, "status: \"cancelled\"\n")
}

func TestAcceptancePriorityChangeUnrestrictedAuditedAndTemplateDocumentsIt(t *testing.T) {
	t.Run("any_actor_may_change_and_it_is_audited", func(t *testing.T) {
		initBeadsDir(t)
		id := mustCreateBead(t, "--title", "t", "--priority", "2")
		mustRun(t, "update", id, "--priority", "1", "--actor", "any-runner", "--reason", "urgent now")
		mustShowField(t, id, "priority: 1\n")
		out := mustRun(t, "show", id)
		var found bool
		for line := range strings.SplitSeq(out, "\n") {
			if strings.Contains(line, " any-runner ") && strings.Contains(line, "priority") && strings.Contains(line, "(urgent now)") {
				found = true
			}
		}
		if !found {
			t.Errorf("show history = %q, want the priority change audited with actor and reason", out)
		}
	})

	t.Run("template_documents_levels_owners_and_cancel_procedure", func(t *testing.T) {
		_, thisFile, _, _ := runtime.Caller(0)
		b, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "skills", "loom", "SKILL.md"))
		if err != nil {
			t.Fatalf("read SKILL.md: %v", err)
		}
		doc := string(b)
		start := strings.Index(doc, "\n## 優先度\n")
		if start < 0 {
			t.Fatal("SKILL.md has no ## 優先度 section")
		}
		section := doc[start+1:]
		if end := strings.Index(section[1:], "\n## "); end >= 0 {
			section = section[:end+1]
		}
		for _, want := range []string{
			"`P1` は", "`P2` は", "`P3` は", "`P4` は",
			"起票者が付ける", "変えるのは",
			"前提を取り消すときは",
		} {
			if !strings.Contains(section, want) {
				t.Errorf("SKILL.md ## 優先度 lacks %q", want)
			}
		}
	})
}

func TestAcceptanceRepeatedReadyClaimTakesHighestEffectiveFirstWithoutLimit(t *testing.T) {
	initBeadsDir(t)
	p2 := mustCreateBead(t, "--title", "plain p2", "--priority", "2")
	p1 := mustCreateBead(t, "--title", "plain p1", "--priority", "1")
	blocker := mustCreateBead(t, "--title", "blocker p3", "--priority", "3")
	mustCreateBead(t, "--title", "urgent p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)

	for i, want := range []string{blocker, p1, p2} {
		out := mustRun(t, "ready", "--claim", "--actor", "worker-"+strconv.Itoa(i))
		if !strings.Contains(out, "Claimed: "+mustDisplayAlias(t, want)+"\n") && !strings.Contains(out, "Claimed: "+mustDisplayAlias(t, want)+" ") {
			t.Errorf("claim %d output = %q, want %s claimed", i, out, mustDisplayAlias(t, want))
		}
	}
}
