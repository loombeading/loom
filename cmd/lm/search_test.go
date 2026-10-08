// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestSearchMatchesTitle(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "gizmo-quasar widget", "--description", "irrelevant")
	other := mustCreateBead(t, "--title", "irrelevant")
	id, other = mustDisplayAlias(t, id), mustDisplayAlias(t, other)

	out, errS, code := runCmd(t, []string{"search", "quasar"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("search output = %q, want the title match", out)
	}
	if strings.Contains(out, other) {
		t.Fatalf("search output = %q, must not return the unrelated Bead", out)
	}
}

func TestSearchMatchesDescriptionOnly(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "plain", "--description", "contains nebulon-nine deep inside")
	other := mustCreateBead(t, "--title", "plain two", "--description", "unrelated")
	id, other = mustDisplayAlias(t, id), mustDisplayAlias(t, other)

	out, errS, code := runCmd(t, []string{"search", "nebulon-nine"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("search output = %q, want the description match", out)
	}
	if strings.Contains(out, other) {
		t.Fatalf("search output = %q, must not return the unrelated Bead", out)
	}
}

func TestSearchPercentAndUnderscoreAreLiteral(t *testing.T) {
	initBeadsDir(t)
	literal := mustCreateBead(t, "--title", "has a 50%_off marker")
	decoy := mustCreateBead(t, "--title", "has a 50Xoff marker")
	literal, decoy = mustDisplayAlias(t, literal), mustDisplayAlias(t, decoy)

	out, errS, code := runCmd(t, []string{"search", "50%_off"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, literal) {
		t.Fatalf("search output = %q, want the literal %%/_ match", out)
	}
	if strings.Contains(out, decoy) {
		t.Fatalf("search output = %q, must not treat %%/_ as wildcards (matched %s)", out, decoy)
	}
}

func TestSearchCaseInsensitiveASCIIOnly(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "FooBarBaz token")
	id = mustDisplayAlias(t, id)

	out, errS, code := runCmd(t, []string{"search", "foobarbaz"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("search output = %q, want a case-insensitive ASCII match", out)
	}
}

func TestSearchDefaultExcludesTerminalAllIncludes(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "closable-xenon item")
	id = mustDisplayAlias(t, id)
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"search", "closable-xenon"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, id) {
		t.Fatalf("search output = %q, want closed Bead excluded by default", out)
	}

	allOut, errS, code := runCmd(t, []string{"search", "--all", "closable-xenon"}, "")
	if code != 0 {
		t.Fatalf("search --all: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(allOut, id) {
		t.Fatalf("search --all output = %q, want closed Bead included", allOut)
	}
}

func TestSearchTruncated(t *testing.T) {
	initBeadsDir(t)
	for i := range 3 {
		mustCreateBead(t, "--title", fmt.Sprintf("truncatable-orbit %d", i))
	}

	out, errS, code := runCmd(t, []string{"search", "--limit", "2", "truncatable-orbit"}, "")
	if code != 0 {
		t.Fatalf("search --limit 2: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "Truncated: showing 2 of 3") {
		t.Fatalf("search --limit 2 output = %q, want a Truncated: line", out)
	}
}

func TestSearchMultipleWordsIsError(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"search", "foo", "bar"}, "")
	if code == 0 {
		t.Fatalf("search foo bar: exit code = 0, want non-zero (no implicit AND)")
	}
	if !strings.Contains(errS, "Error:") {
		t.Fatalf("search foo bar stderr = %q, want an Error: line", errS)
	}
}

func TestSearchNoWordIsError(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"search"}, "")
	if code == 0 {
		t.Fatalf("search: exit code = 0, want non-zero")
	}
	if !strings.Contains(errS, "Error:") {
		t.Fatalf("search stderr = %q, want an Error: line", errS)
	}
}

func TestSearchFilterLineIncludesQueryFirst(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "orderable-quokka thing", "--namespace", "web")
	id = mustDisplayAlias(t, id)

	out, errS, code := runCmd(t, []string{"search", "--namespace", "web", "orderable-quokka"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, `Filter: query="orderable-quokka", namespace="web"`) {
		t.Fatalf("search output = %q, want the Filter: line to start with query then namespace", out)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("search output = %q, want the matching Bead", out)
	}
}

func TestSearchFlatListNoTree(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "epic-parent-flarion", "--type", "task")
	child := mustCreateBead(t, "--title", "child-flarion-item", "--parent", parent)

	out, errS, code := runCmd(t, []string{"search", "flarion"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "  -") {
			t.Fatalf("search output = %q, want a flat list with no indentation, got %q", out, line)
		}
	}
	_ = child
}
