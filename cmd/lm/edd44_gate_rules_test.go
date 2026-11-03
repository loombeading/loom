// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

const edd44AdjudicationRejected = "a human/confirm Gate needs adjudicated-by:<closed adjudicate Gate>; create it with --kind adjudicate"

func edd44OpenJudge(t *testing.T, target string) string {
	t.Helper()
	return mustGateCreateCmd(t, []string{target}, "judge", "--kind", "adjudicate")
}

func edd44AdjudicationValue(t *testing.T, target, path string) (string, bool) {
	t.Helper()
	switch path {
	case "missing":
		return "", false
	case "empty value":
		return "", true
	case "unknown bead":
		return "lm-zzzz", true
	case "not a gate":
		return mustCreateBead(t, "--title", "task", "--label", "kind:adjudicate"), true
	case "not adjudicate":
		id := mustGateCreateCmd(t, []string{target}, "ext", "--kind", "external")
		if _, errS, code := runCmd(t, []string{"gate", "resolve", id, "--reason", "r"}, ""); code != 0 {
			t.Fatalf("resolve external gate: exit=%d stderr=%q", code, errS)
		}
		return mustCanonicalID(t, id), true
	case "open judge":
		return mustCanonicalID(t, edd44OpenJudge(t, target)), true
	case "cancelled judge":
		id := edd44OpenJudge(t, target)
		if _, errS, code := runCmd(t, []string{"gate", "reject", id, "--reason", "no"}, ""); code != 0 {
			t.Fatalf("reject judge gate: exit=%d stderr=%q", code, errS)
		}
		return mustCanonicalID(t, id), true
	}
	t.Fatalf("unknown path %q", path)
	return "", false
}

func TestEDD44AdjudicationStderrOnEveryEntryAndPath(t *testing.T) {
	paths := []string{"missing", "empty value", "unknown bead", "not a gate", "not adjudicate", "open judge", "cancelled judge"}
	entries := []string{"gate create", "create --type gate", "update --add-label"}
	for _, entry := range entries {
		for _, path := range paths {
			t.Run(entry+"/"+path, func(t *testing.T) {
				initBeadsDir(t)
				target := mustCreateBead(t, "--title", "target")
				value, hasLabel := edd44AdjudicationValue(t, target, path)

				var args []string
				wantID := "new"
				var beforeShow, gate string
				switch entry {
				case "gate create":
					args = []string{"gate", "create", "--blocks", target, "--subject", "s", "--kind", "human"}
					if hasLabel {
						args = append(args, "--adjudicated-by", value)
					}
				case "create --type gate":
					args = []string{"create", "--title", "g", "--type", "gate", "--label", "kind:confirm"}
					if hasLabel {
						args = append(args, "--label", "adjudicated-by:"+value)
					}
				case "update --add-label":
					gate = mustGateCreateCmd(t, []string{target}, "target gate", "--kind", "adjudicate")
					wantID = gate
					beforeShow, _, _ = runCmd(t, []string{"show", gate}, "")
					args = []string{"update", gate, "--add-label", "kind:human"}
					if hasLabel {
						args = append(args, "--add-label", "adjudicated-by:"+value)
					}
				}

				out, errS, code := runCmd(t, args, "")
				want := "Rejected: " + wantID + " (" + edd44AdjudicationRejected + ")\n"
				if code != 1 || out != "" || errS != want {
					t.Fatalf("%v: exit=%d stdout=%q stderr=%q, want exit 1, empty stdout, stderr %q", args, code, out, errS, want)
				}
				if gate != "" {
					afterShow, _, _ := runCmd(t, []string{"show", gate}, "")
					if afterShow != beforeShow {
						t.Errorf("rejected update changed the gate:\nbefore:\n%s\nafter:\n%s", beforeShow, afterShow)
					}
				}
			})
		}
	}
}

func TestEDD44RemoveAdjudicatedByRejected(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	judge := mustClosedJudge(t)
	gate := mustGateCreateCmd(t, []string{target}, "person", "--kind", "human", "--adjudicated-by", judge)
	beforeShow, _, _ := runCmd(t, []string{"show", gate}, "")
	if !strings.Contains(beforeShow, "adjudicated-by:"+judge) {
		t.Fatalf("setup: gate lacks the adjudication record:\n%s", beforeShow)
	}

	out, errS, code := runCmd(t, []string{"update", gate, "--remove-label", "adjudicated-by:" + judge}, "")
	want := "Rejected: " + gate + " (" + edd44AdjudicationRejected + ")\n"
	if code != 1 || out != "" || errS != want {
		t.Fatalf("update --remove-label: exit=%d stdout=%q stderr=%q, want exit 1, empty stdout, stderr %q", code, out, errS, want)
	}
	afterShow, _, _ := runCmd(t, []string{"show", gate}, "")
	if afterShow != beforeShow {
		t.Errorf("rejected update changed the gate:\nbefore:\n%s\nafter:\n%s", beforeShow, afterShow)
	}
}

func TestEDD44GateResolveMultipleIDsIndependent(t *testing.T) {
	initBeadsDir(t)
	t1 := mustCreateBead(t, "--title", "t1")
	t2 := mustCreateBead(t, "--title", "t2")
	g1 := mustGateCreateCmd(t, []string{t1}, "g1")
	g2 := mustGateCreateCmd(t, []string{t2}, "g2")

	out, errS, code := runCmd(t, []string{"gate", "resolve", g1, "lm-zzzz", g2, "--reason", "r"}, "")
	if code != 1 {
		t.Fatalf("gate resolve with an unknown ID: exit=%d stdout=%q stderr=%q, want 1", code, out, errS)
	}
	if !strings.Contains(out, "Rejected: lm-zzzz") && !strings.Contains(errS, "Rejected: lm-zzzz") {
		t.Errorf("gate resolve: stdout=%q stderr=%q, want a Rejected line for the unknown ID", out, errS)
	}
	for _, g := range []string{g1, g2} {
		show, _, _ := runCmd(t, []string{"show", g}, "")
		if !strings.Contains(show, "status: \"closed\"") {
			t.Errorf("gate %s after the multi-ID resolve:\n%s\nwant status closed", g, show)
		}
	}
}

func TestEDD44RejectOutputCancelledThenNewlyReady(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "downstream")
	target := mustCreateBead(t, "--title", "target", "--parent", parent)
	gate := mustGateCreateCmd(t, []string{target}, "x")

	out, errS, code := runCmd(t, []string{"gate", "reject", gate, "--reason", "no"}, "")
	if code != 0 {
		t.Fatalf("gate reject: exit=%d stderr=%q stdout=%q", code, errS, out)
	}
	cancelled := strings.Index(out, "## Cancelled")
	ready := strings.Index(out, "## Newly ready")
	if cancelled < 0 || ready < 0 || cancelled > ready {
		t.Errorf("gate reject output must list ## Cancelled before ## Newly ready:\n%s", out)
	}
}
