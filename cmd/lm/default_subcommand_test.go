// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestGateBareListsOpenGates(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "wait for it")
	targetAlias := mustDisplayAlias(t, target)

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	wantPrefix := "- " + gateID + " [gate/open/P2] wait for it  → blocks: " + targetAlias
	if !strings.Contains(out, wantPrefix) {
		t.Errorf("lm gate = %q, want a line starting with %q", out, wantPrefix)
	}
}

func TestAheadEstimates(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("lm ahead: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, "# Ahead\n") {
		t.Errorf("lm ahead = %q, want it to start with '# Ahead'", out)
	}

	budgetOut, errS, code := runCmd(t, []string{"ahead", "--budget", "100000"}, "")
	if code != 0 {
		t.Fatalf("lm ahead --budget: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(budgetOut, "- Budget: ") {
		t.Errorf("lm ahead --budget = %q, want a Budget line", budgetOut)
	}
}

func TestDepBareStillErrors(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"dep"}, "")
	if code != 1 {
		t.Errorf("lm dep: exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("lm dep: stdout = %q, want empty", out)
	}
	wantErrS := "Error: lm dep requires a subcommand (add, remove)\n"
	if errS != wantErrS {
		t.Errorf("lm dep: stderr = %q, want %q", errS, wantErrS)
	}
}

func TestGateUnknownSubcommandNotTreatedAsBare(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"gate", "bd-xxx"}, "")
	if code != 1 {
		t.Errorf("lm gate bd-xxx: exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("lm gate bd-xxx: stdout = %q, want empty", out)
	}
	if !strings.Contains(errS, "unknown") {
		t.Errorf("lm gate bd-xxx: stderr = %q, want it to contain \"unknown\"", errS)
	}
}

func TestGateListCostEstimateLongFormsRemoved(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"gate", "list"}, "")
	if code != 1 {
		t.Errorf("gate list: exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("gate list: stdout = %q, want empty", out)
	}
	if !strings.Contains(errS, "unknown") {
		t.Errorf("gate list: stderr = %q, want it to contain \"unknown\"", errS)
	}

	out, errS, code = runCmd(t, []string{"cost", "estimate"}, "")
	if code != 1 {
		t.Errorf("cost estimate: exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("cost estimate: stdout = %q, want empty", out)
	}
	if !strings.Contains(errS, "unknown") {
		t.Errorf("cost estimate: stderr = %q, want it to contain \"unknown\"", errS)
	}
}

func TestGateListSubcommandErrorTextFixed(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"gate", "list"}, "")
	if code != 1 {
		t.Errorf("gate list: exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("gate list: stdout = %q, want empty", out)
	}
	wantErrS := "Error: unknown lm gate subcommand \"list\"\nSee: lm help\n"
	if errS != wantErrS {
		t.Errorf("gate list: stderr = %q, want %q", errS, wantErrS)
	}
}

func TestGateHelpShowsBareLine(t *testing.T) {
	initBeadsDir(t)

	_, errS, code := runCmd(t, []string{"gate", "--help"}, "")
	if code != 0 {
		t.Fatalf("lm gate --help: exit code = %d, stderr = %q", code, errS)
	}
	wantLine := "- gate — List open Gates and the Beads they block\n"
	if !strings.Contains(errS, wantLine) {
		t.Errorf("lm gate --help: stderr = %q, want it to contain %q", errS, wantLine)
	}
}

func TestGateHelpListsBareFlags(t *testing.T) {
	initBeadsDir(t)

	_, errS, code := runCmd(t, []string{"gate", "-h"}, "")
	if code != 0 {
		t.Fatalf("lm gate -h: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(errS, "Flags:\n  --all\n") {
		t.Errorf("lm gate -h: stderr = %q, want a Flags section listing --all", errS)
	}
}
