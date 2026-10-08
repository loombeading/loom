// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/domain"
)

func TestListGoldenOutput(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "only task", "-p", "3")

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, "Filter: status=\"unfinished\"\n") {
		t.Fatalf("list output = %q, want it to start with the implicit unfinished-only Filter: line", out)
	}
	out = strings.TrimPrefix(out, "Filter: status=\"unfinished\"\n")

	prefix := "- " + domain.DefaultNamespace + "-"
	if !strings.HasPrefix(out, prefix) {
		t.Fatalf("list output = %q, want it to start with %q", out, prefix+"<hex>")
	}
	rest := strings.TrimPrefix(out, "- ")
	idAndRest := strings.SplitN(rest, " ", 2)
	if len(idAndRest) != 2 {
		t.Fatalf("list output = %q, want at least an ID and a rest", out)
	}
	got := "- " + domain.DefaultNamespace + "-XXXX " + idAndRest[1]

	want := "- " + domain.DefaultNamespace + "-XXXX [open/P3] only task\n"
	if got != want {
		t.Errorf("list golden output:\ngot:  %q\nwant: %q", got, want)
	}
}
