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
