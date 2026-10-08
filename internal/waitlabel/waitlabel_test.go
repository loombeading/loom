// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package waitlabel

import (
	"slices"
	"testing"
)

func TestKindLabel(t *testing.T) {
	if got := PRMerged.Prefix(); got != "wait:pr-merged:" {
		t.Errorf("PRMerged.Prefix() = %q", got)
	}
	consts := []Kind{Disk, Date, File, CIGreen, Runs, Proofs, Log, PRMerged, After, Closes, Tier, RepoVisibility, RepoAbsent, GitHub,
		"tag", "sli", "public-path", "public-review", "public-check", "schema-version", "url"}
	got := Kinds
	want := slices.Sorted(slices.Values(consts))
	if !slices.Equal(got, want) {
		t.Errorf("Kinds = %q, want the Kind constants %q", got, want)
	}
}

func TestIsProbe(t *testing.T) {
	for label, want := range map[string]bool{
		"wait:disk:/:5": true, "wait:date:2026-10-01": true, "wait:file:/x": true,
		"wait:pr-merged:u": true, "wait:ci-green:u": true, "wait:runs:5:20": true,
		"wait:proofs:10": true, "wait:log:3:auth failed": true,
		"wait:after:2026-10-01T22:40:00Z": true, "wait:closes:sonnet:5": true,
		"wait:public-review:main": true, "wait:tag:v1": true, "wait:public-review": true,
		"wait:limit:seven_day:default": false, "wait:rm -rf /": false, "kind:external": false,
		RunnersOnline.Prefix() + "o": false,
	} {
		if got := IsProbe(label); got != want {
			t.Errorf("IsProbe(%q)=%v want %v", label, got, want)
		}
	}
}

func TestIsUnknown(t *testing.T) {
	for label, want := range map[string]bool{
		"wait:runners-online:o": true, "wait:limit:seven_day:default": true, "wait:": true,
		"wait:any": false, "wait:file:/x": false, "wait:pr-merged:u": false, "wait:public-review": false,
		"kind:human": false, "abolished-by:x": false,
	} {
		if got := IsUnknown(label); got != want {
			t.Errorf("IsUnknown(%q)=%v want %v", label, got, want)
		}
	}
}

func TestWaitsOnPerson(t *testing.T) {
	for label, want := range map[string]bool{
		"wait:pr-merged:u": true, "wait:repo-visibility:o/r:public": true, "wait:repo-absent:o/r": true,
		"wait:disk:/:5": false, "wait:date:2026-10-01": false, "wait:file:/x": false,
		"wait:ci-green:u": false, "wait:runs:5:20": false, "wait:proofs:10": false,
		"wait:log:3:x": false, "wait:limit:seven_day:default": false, "kind:human": false,
	} {
		if got := WaitsOnPerson(label); got != want {
			t.Errorf("WaitsOnPerson(%q)=%v want %v", label, got, want)
		}
	}
}
