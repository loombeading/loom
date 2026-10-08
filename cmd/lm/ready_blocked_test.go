// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"sync"
	"testing"
)

func TestReadyExcludesBlockedAndGate(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked", "--blocked-by", blocker)
	mustCreateBead(t, "--title", "a gate", "--type", "gate", "--label", "kind:human")

	out, errS, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "blocker") {
		t.Errorf("ready output = %q, want it to list the unblocked blocker", out)
	}
	if strings.Contains(out, "\"blocked\"") || strings.Contains(out, "| blocked |") {
		t.Errorf("ready output = %q, must not list the blocked Bead", out)
	}
	if strings.Contains(out, "a gate") {
		t.Errorf("ready output = %q, must not list the gate", out)
	}
}

func TestBlockedListsBlockedByColumn(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked task", "--blocked-by", blocker)

	out, errS, code := runCmd(t, []string{"blocked"}, "")
	if code != 0 {
		t.Fatalf("blocked: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "← blocked by:") {
		t.Fatalf("blocked output = %q, want a trailing blocked-by item", out)
	}
	if !strings.Contains(out, "blocked task") {
		t.Fatalf("blocked output = %q, want it to list the blocked task", out)
	}
}

func TestCreateWithBlockedByNeverReady(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked at birth", "--blocked-by", blocker)

	out, _, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready exit code = %d", code)
	}
	if strings.Contains(out, "blocked at birth") {
		t.Fatalf("ready output = %q, must never list a Bead created already blocked", out)
	}
}

func TestCloseNewlyReadySection(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked", "--blocked-by", blocker)
	mustCreateBead(t, "--title", "already ready")

	out, errS, code := runCmd(t, []string{"close", blocker}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "## Newly ready") {
		t.Fatalf("close output = %q, want the Newly ready heading", out)
	}
	if !strings.Contains(out, "blocked") {
		t.Fatalf("close output = %q, want the newly-ready Bead in the Newly ready section", out)
	}
	if strings.Contains(out, "already ready") {
		t.Fatalf("close output = %q, must not list a Bead that was already Ready", out)
	}
}

func TestCloseNewlyReadyNoneWhenNothingChanges(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "solo")

	out, errS, code := runCmd(t, []string{"close", id}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "## Newly ready\n(none)") {
		t.Fatalf("close output = %q, want an empty Newly ready section", out)
	}
}

func TestCloseMultipleIDsSingleNewlyReadySection(t *testing.T) {
	initBeadsDir(t)
	blockerA := mustCreateBead(t, "--title", "blockerA")
	mustCreateBead(t, "--title", "blockedA", "--blocked-by", blockerA)
	blockerB := mustCreateBead(t, "--title", "blockerB")
	mustCreateBead(t, "--title", "blockedB", "--blocked-by", blockerB)

	out, errS, code := runCmd(t, []string{"close", blockerA, blockerB}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if got := strings.Count(out, "## Newly ready"); got != 1 {
		t.Fatalf("close output has %d Newly ready headings, want 1:\n%s", got, out)
	}
	if !strings.Contains(out, "blockedA") || !strings.Contains(out, "blockedB") {
		t.Fatalf("close output = %q, want both newly-ready Beads listed", out)
	}
}

func TestUpdateCancelNewlyReadySection(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "blocked")
	child := mustCreateBead(t, "--title", "child", "--parent", parent)

	out, errS, code := runCmd(t, []string{"update", "--status", "cancelled", child}, "")
	if code != 0 {
		t.Fatalf("update --status cancelled: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "## Newly ready") {
		t.Fatalf("update output = %q, want the Newly ready heading", out)
	}
	if !strings.Contains(out, "blocked") {
		t.Fatalf("update output = %q, want the newly-ready Bead listed", out)
	}
}

func TestUpdateClaimHasNoNewlyReadySection(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "task")

	out, errS, code := runCmd(t, []string{"update", "--claim", id}, "")
	if code != 0 {
		t.Fatalf("update --claim: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Newly ready") {
		t.Fatalf("update --claim output = %q, must not have a Newly ready section", out)
	}
}

func TestReadyClaimClaimsTheHeadOfTheOrder(t *testing.T) {
	initBeadsDir(t)
	low := mustCreateBead(t, "--title", "low priority", "--priority", "3")
	high := mustCreateBead(t, "--title", "high priority", "--priority", "1")
	low, high = mustDisplayAlias(t, low), mustDisplayAlias(t, high)

	out, errS, code := runCmd(t, []string{"ready", "--claim", "--actor", "alice"}, "")
	if code != 0 {
		t.Fatalf("ready --claim: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "Claimed: "+high) {
		t.Fatalf("ready --claim output = %q, want %q claimed (the head of the order)", out, high)
	}
	if !strings.Contains(out, "[in_progress/P1] high priority  @alice") {
		t.Fatalf("ready --claim output = %q, want the claimed row's post-claim status/claimed_by", out)
	}

	readyOut, _, code := runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	if strings.Contains(readyOut, high) {
		t.Errorf("ready output = %q, must not list the just-claimed Bead", readyOut)
	}
	if !strings.Contains(readyOut, low) {
		t.Errorf("ready output = %q, want the lower-priority Bead still Ready", readyOut)
	}
}

func TestReadyClaimRecordsOrdinaryClaimAudit(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	id = mustDisplayAlias(t, id)

	out, errS, code := runCmd(t, []string{"ready", "--claim", "--actor", "bob"}, "")
	if code != 0 {
		t.Fatalf("ready --claim: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "Claimed: "+id) {
		t.Fatalf("ready --claim output = %q, want %q claimed", out, id)
	}

	showOut, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, "field claim = bob") {
		t.Fatalf("show history = %q, want an ordinary claim audit record for bob", showOut)
	}
}

func TestReadyClaimEmptyWhenNoneReady(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"ready", "--claim"}, "")
	if code != 0 {
		t.Fatalf("ready --claim on an empty database: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Claimed:") {
		t.Fatalf("ready --claim output = %q, must not claim anything when nothing is Ready", out)
	}
	if strings.TrimSpace(out) != "(none)" {
		t.Fatalf("ready --claim output = %q, want the same empty table as `lm ready`", out)
	}

	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked", "--blocked-by", blocker)
	if _, errS, code := runCmd(t, []string{"update", "--claim", blocker}, ""); code != 0 {
		t.Fatalf("claim the only Ready Bead: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code = runCmd(t, []string{"ready", "--claim"}, "")
	if code != 0 {
		t.Fatalf("ready --claim with nothing Ready left: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Claimed:") {
		t.Fatalf("ready --claim output = %q, must not claim anything when the only candidate is blocked/claimed", out)
	}
}

func TestReadyClaimRespectsNamespaceFilter(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "a-task", "--namespace", "a")
	bID := mustCreateBead(t, "--title", "b-task", "--namespace", "b")
	bID = mustDisplayAlias(t, bID)

	out, errS, code := runCmd(t, []string{"ready", "--claim", "--namespace", "b"}, "")
	if code != 0 {
		t.Fatalf("ready --claim --namespace b: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "Claimed: "+bID) {
		t.Fatalf("ready --claim --namespace b output = %q, want %q claimed", out, bID)
	}

	readyOut, _, code := runCmd(t, []string{"ready", "--limit", "0"}, "")
	if code != 0 {
		t.Fatal("ready failed")
	}
	if strings.Contains(readyOut, bID) {
		t.Errorf("ready output = %q, must not list the just-claimed Bead", readyOut)
	}
}

func TestReadyClaimReadOnlyModeRejected(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	setIsolatedXDGHome(t)
	t.Setenv("LM_READONLY", "0")
	if _, errS, code := runCmd(t, []string{"init"}, ""); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, errS)
	}
	mustCreateBead(t, "--title", "t")

	t.Setenv("LM_READONLY", "1")
	out, errS, code := runCmd(t, []string{"ready", "--claim"}, "")
	if code != 1 {
		t.Errorf("ready --claim under LM_READONLY=1: exit code = %d, want 1", code)
	}
	if errS == "" && !strings.Contains(out, "Error:") {
		t.Errorf("ready --claim under LM_READONLY=1: no error message (stdout=%q, stderr=%q)", out, errS)
	}
}

func TestReadyClaimConcurrentOnlyOneSucceeds(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	id = mustDisplayAlias(t, id)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claims  []string
		outputs []string
	)
	for _, a := range []string{"alice", "bob"} {
		wg.Add(1)
		go func(actor string) {
			defer wg.Done()
			out, _, code := runCmd(t, []string{"ready", "--claim", "--actor", actor}, "")
			mu.Lock()
			outputs = append(outputs, out)
			if code == 0 && strings.Contains(out, "Claimed: "+id) {
				claims = append(claims, actor)
			}
			mu.Unlock()
		}(a)
	}
	wg.Wait()

	if len(claims) != 1 {
		t.Fatalf("claims = %v (outputs = %v), want exactly one actor to claim %q", claims, outputs, id)
	}

	showOut, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if got := strings.Count(showOut, "field claim ="); got != 1 {
		t.Fatalf("show history = %q, want exactly one claim audit record, got %d", showOut, got)
	}
}
