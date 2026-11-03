// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/domain"
)

func TestUpdateWaitDateOnTimedGateWarns(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "t")
	gateOut, errS, code := runCmd(t, []string{"gate", "create", "--subject", "23:00 に窓が閉じる", "--blocks", target, "--kind", "external", "--resolver", "watcher"}, "")
	if code != 0 {
		t.Fatalf("gate create: exit code = %d, stderr = %q", code, errS)
	}
	gateID := mustParseCreatedID(t, gateOut)

	_, errS, code = runCmd(t, []string{"update", gateID, "--add-label", "wait:date:2026-10-01"}, "")
	if code != 0 {
		t.Fatalf("update --add-label wait:date: exit code = %d, want 0; stderr = %q", code, errS)
	}
	if !strings.Contains(errS, "warning:") || !strings.Contains(errS, "wait:date は日単位に丸める") {
		t.Fatalf("stderr = %q, want a wait:date warning", errS)
	}

	out, errS, code := runCmd(t, []string{"show", gateID}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "wait:date:2026-10-01") {
		t.Fatalf("show output %q: wait:date: label not applied despite the warning-only path", out)
	}
}

func TestUpdateWaitDateNoTimeNoWarning(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "t")
	gateOut, errS, code := runCmd(t, []string{"gate", "create", "--subject", "日付だけで窓が閉じる", "--blocks", target, "--kind", "external", "--resolver", "watcher"}, "")
	if code != 0 {
		t.Fatalf("gate create: exit code = %d, stderr = %q", code, errS)
	}
	gateID := mustParseCreatedID(t, gateOut)

	_, errS, code = runCmd(t, []string{"update", gateID, "--add-label", "wait:date:2026-10-01"}, "")
	if code != 0 {
		t.Fatalf("update --add-label wait:date: exit code = %d, want 0; stderr = %q", code, errS)
	}
	if strings.Contains(errS, "warning:") {
		t.Fatalf("stderr = %q, want no warning (no time of day named)", errS)
	}
}

func TestParseClearFlags(t *testing.T) {
	cases := []struct {
		name     string
		clear    []string
		fieldSet map[string]bool
		depth    int
		wantCols []string
		wantErr  string
	}{
		{name: "all", clear: []string{"description", "summary", "reasoning_depth", "reasoning_depth"}, wantCols: []string{"description", "reasoning_depth", "summary"}},
		{name: "unknown", clear: []string{"summary", "title"}, wantErr: `unknown --clear field "title"`},
		{name: "both", clear: []string{"reasoning_depth"}, fieldSet: map[string]bool{"reasoning-depth": true}, depth: 2, wantErr: "--clear and --reasoning-depth both given"},
		{name: "depth zero", fieldSet: map[string]bool{"reasoning-depth": true}, depth: 0, wantCols: []string{}, wantErr: "reasoning_depth must be between"},
		{name: "depth ok", fieldSet: map[string]bool{"reasoning-depth": true}, depth: 1, wantCols: []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cols, errMsg := parseClearFlags(c.clear, c.fieldSet, c.depth)
			if c.wantErr == "" && errMsg != "" || !strings.Contains(errMsg, c.wantErr) {
				t.Fatalf("err = %q, want containing %q", errMsg, c.wantErr)
			}
			if c.wantCols == nil {
				return
			}
			sort.Strings(cols)
			if strings.Join(cols, ",") != strings.Join(c.wantCols, ",") {
				t.Fatalf("cols = %v, want %v", cols, c.wantCols)
			}
		})
	}
}

func TestPartialFlagError(t *testing.T) {
	set := map[string]bool{"partial": true}
	cases := []struct {
		fieldSet map[string]bool
		partial  string
		refs     []string
		want     string
	}{
		{fieldSet: nil, want: ""},
		{fieldSet: set, partial: "", refs: []string{"u"}, want: "--partial requires a non-empty value"},
		{fieldSet: set, partial: "x", want: "--partial requires --external-ref"},
		{fieldSet: set, partial: "x", refs: []string{"u"}, want: ""},
	}
	for _, c := range cases {
		if got := partialFlagError(c.fieldSet, c.partial, c.refs); got != c.want {
			t.Errorf("partialFlagError(%v, %q, %v) = %q, want %q", c.fieldSet, c.partial, c.refs, got, c.want)
		}
	}
}

func TestSetGivenFields(t *testing.T) {
	v := updateFieldValues{title: "T", description: "D", beadType: "bug", namespace: "ns", summary: "S", priority: 0, depth: 3}

	in := domain.UpdateInput{Priority: -1}
	setGivenFields(&in, map[string]bool{}, v)
	if fmt.Sprintf("%+v", in) != fmt.Sprintf("%+v", domain.UpdateInput{Priority: -1}) {
		t.Fatalf("no flags given, got %+v", in)
	}

	in = domain.UpdateInput{Priority: -1}
	setGivenFields(&in, map[string]bool{"title": true, "description": true, "p": true, "type": true, "namespace": true, "summary": true, "reasoning-depth": true}, v)
	want := domain.UpdateInput{Title: "T", Description: "D", Type: "bug", Namespace: "ns", Summary: "S", Priority: 0, Depth: 3}
	if fmt.Sprintf("%+v", in) != fmt.Sprintf("%+v", want) {
		t.Fatalf("got %+v, want %+v", in, want)
	}
}

func TestUpdateCancelAutoResolvesGateOnlyWhenAllBlockedTerminal(t *testing.T) {
	initBeadsDir(t)
	target1 := mustCreateBead(t, "--title", "target1")
	target2 := mustCreateBead(t, "--title", "target2")
	soleGate := mustGateCreateCmd(t, []string{target1}, "sole")
	sharedGate := mustGateCreateCmd(t, []string{target1, target2}, "shared")

	out, errS, code := runCmd(t, []string{"update", target1, "--status", "cancelled", "--reason", "r"}, "")
	if code != 0 {
		t.Fatalf("update cancel: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Auto-resolved: "+soleGate) {
		t.Errorf("update output = %q, want an Auto-resolved line for %q", out, soleGate)
	}
	if strings.Contains(out, "Auto-resolved: "+sharedGate) {
		t.Errorf("update output = %q, want no Auto-resolved line for %q while target2 is open", out, sharedGate)
	}
	if strings.Index(out, "- Updated: ") > strings.Index(out, "- Auto-resolved: ") {
		t.Errorf("update output = %q, want the Updated line before the Auto-resolved line", out)
	}

	showOut, _, _ := runCmd(t, []string{"show", soleGate}, "")
	if !strings.Contains(showOut, `status: "closed"`) {
		t.Errorf("gate show output = %q, want status closed", showOut)
	}
}

func TestUpdateWithoutCancelDoesNotAutoResolveGate(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	mustGateCreateCmd(t, []string{target}, "gate")

	out, errS, code := runCmd(t, []string{"update", target, "--add-label", "x"}, "")
	if code != 0 {
		t.Fatalf("update: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Auto-resolved") {
		t.Errorf("update output = %q, want no Auto-resolved line without a terminal transition", out)
	}
}

func TestUpdateClearErrorRejectsEveryID(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a", "--description", "d")
	b := mustCreateBead(t, "--title", "b", "--description", "d")

	cases := []struct {
		name   string
		args   []string
		reason string
	}{
		{"unknown field", []string{"update", a, b, "--clear", "title"}, `unknown --clear field "title"`},
		{"clear and value", []string{"update", a, b, "--clear", "description", "--description", "x"}, "--clear and --description both given"},
	}
	for _, c := range cases {
		out, _, code := runCmd(t, c.args, "")
		if code != 1 {
			t.Fatalf("%s: exit code = %d, want 1", c.name, code)
		}
		var rejected []string
		for line := range strings.SplitSeq(out, "\n") {
			if strings.HasPrefix(line, "Rejected: ") {
				rejected = append(rejected, line)
			}
		}
		if len(rejected) != 2 {
			t.Fatalf("%s: Rejected lines = %q, want 2", c.name, rejected)
		}
		for i, id := range []string{a, b} {
			if !strings.Contains(rejected[i], id) || !strings.Contains(rejected[i], c.reason) {
				t.Errorf("%s: Rejected line %q, want id %q and reason %q", c.name, rejected[i], id, c.reason)
			}
		}
	}
	for _, id := range []string{a, b} {
		out, _, _ := runCmd(t, []string{"show", id}, "")
		if !strings.Contains(out, "\nd\n") {
			t.Errorf("show %s = %q, want description d unchanged", id, out)
		}
	}
}
