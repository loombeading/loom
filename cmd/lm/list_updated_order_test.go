// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
	"time"
)

func createABCFixture(t *testing.T, titleSuffix string) (string, string, string) {
	t.Helper()
	a := mustCanonicalID(t, mustCreateBead(t, "--title", "A-"+titleSuffix, "--expedite", "1h", "--reason", "interrupt"))
	time.Sleep(20 * time.Millisecond)
	b := mustCanonicalID(t, mustCreateBead(t, "--title", "B-"+titleSuffix, "--priority", "2"))
	time.Sleep(20 * time.Millisecond)
	c := mustCanonicalID(t, mustCreateBead(t, "--title", "C-"+titleSuffix, "--priority", "1"))
	time.Sleep(20 * time.Millisecond)
	if _, errS, code := runCmd(t, []string{"update", b, "--priority", "2"}, ""); code != 0 {
		t.Fatalf("touch B (%s) to make it the most recently updated: exit code = %d, stderr = %q", b, code, errS)
	}
	return a, b, c
}

func displayAliases(t *testing.T, ids ...string) []string {
	t.Helper()
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = mustDisplayAlias(t, id)
	}
	return out
}

func assertOrder(t *testing.T, out string, want []string) {
	t.Helper()
	idx := make([]int, len(want))
	for i, id := range want {
		idx[i] = strings.Index(out, id)
		if idx[i] < 0 {
			t.Fatalf("output = %q, want it to contain %q", out, id)
		}
	}
	for i := 1; i < len(idx); i++ {
		if idx[i-1] >= idx[i] {
			t.Fatalf("output = %q, want order %v (got %q before %q)", out, want, want[i], want[i-1])
		}
	}
}

func TestListDefaultOrdersByUpdatedAtDescending(t *testing.T) {
	initBeadsDir(t)
	a, b, c := createABCFixture(t, "list-updated")

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	assertOrder(t, out, displayAliases(t, b, c, a))
}

func TestListSortPriorityOrdersByPriority(t *testing.T) {
	initBeadsDir(t)
	a, b, c := createABCFixture(t, "list-priority")

	out, errS, code := runCmd(t, []string{"list", "--sort", "priority"}, "")
	if code != 0 {
		t.Fatalf("list --sort priority: exit code = %d, stderr = %q", code, errS)
	}
	assertOrder(t, out, displayAliases(t, a, c, b))
}

func TestSearchOrdersByUpdatedAtDescending(t *testing.T) {
	initBeadsDir(t)
	a, b, c := createABCFixture(t, "quazoid-marker")

	out, errS, code := runCmd(t, []string{"search", "quazoid-marker"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	assertOrder(t, out, displayAliases(t, b, c, a))
}

func TestBlockedOrdersByPriority(t *testing.T) {
	initBeadsDir(t)
	a, b, c := createABCFixture(t, "blocked-order")
	mustGateCreateCmd(t, []string{a, b, c}, "wait for all three")

	out, errS, code := runCmd(t, []string{"blocked"}, "")
	if code != 0 {
		t.Fatalf("blocked: exit code = %d, stderr = %q", code, errS)
	}
	assertOrder(t, out, displayAliases(t, a, c, b))
}

func TestGateListOrdersByEffectivePriority(t *testing.T) {
	initBeadsDir(t)
	targetA := mustCanonicalID(t, mustCreateBead(t, "--title", "target-a"))
	targetB := mustCanonicalID(t, mustCreateBead(t, "--title", "target-b"))
	targetC := mustCanonicalID(t, mustCreateBead(t, "--title", "target-c", "--expedite", "1h", "--reason", "interrupt"))

	gateA := mustCanonicalID(t, mustGateCreateCmd(t, []string{targetA}, "await A"))
	time.Sleep(20 * time.Millisecond)
	gateB := mustCanonicalID(t, mustGateCreateCmd(t, []string{targetB}, "await B"))
	time.Sleep(20 * time.Millisecond)
	gateC := mustCanonicalID(t, mustGateCreateCmd(t, []string{targetC}, "await C"))
	time.Sleep(20 * time.Millisecond)
	if _, errS, code := runCmd(t, []string{"update", gateB, "--priority", "2"}, ""); code != 0 {
		t.Fatalf("touch gateB to make it the most recently updated: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	assertOrder(t, out, displayAliases(t, gateC, gateA, gateB))
	targetC = mustDisplayAlias(t, targetC)
	if !strings.Contains(out, "eff=P0("+targetC+")") {
		t.Errorf("output = %q, want gateC marked eff=P0(%s)", out, targetC)
	}
	if strings.Count(out, "eff=") != 1 {
		t.Errorf("output = %q, want eff= only on the raised Gate", out)
	}
}

func TestReadyStillOrdersByPriority(t *testing.T) {
	initBeadsDir(t)
	a, b, c := createABCFixture(t, "ready-order")

	out, errS, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
	}
	assertOrder(t, out, displayAliases(t, a, c, b))

	claimOut, errS, code := runCmd(t, []string{"ready", "--claim", "--actor", "alice"}, "")
	if code != 0 {
		t.Fatalf("ready --claim: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(claimOut, "Claimed: "+mustDisplayAlias(t, a)) {
		t.Fatalf("ready --claim output = %q, want %q claimed (the highest-priority Bead, ready order unchanged)", claimOut, a)
	}
}

func TestListTreeChildOrderFollowsUpdatedAt(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "Parent", "--type", "task")
	mustCreateBead(t, "--title", "X", "--parent", parent)
	time.Sleep(20 * time.Millisecond)
	mustCreateBead(t, "--title", "Y", "--parent", parent)
	parentAlias := mustDisplayAlias(t, parent)
	x := parentAlias + ".1"
	y := parentAlias + ".2"

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	assertOrder(t, out, []string{y, x})
	if !strings.Contains(out, "  - "+y) {
		t.Errorf("list output = %q, want %q indented under the parent", out, y)
	}
	if !strings.Contains(out, "  - "+x) {
		t.Errorf("list output = %q, want %q indented under the parent", out, x)
	}
}
