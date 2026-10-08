// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestUpdateMalformedAliasUpdatesNothing(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent")
	child := mustCreateBead(t, "--title", "child", "--parent", parent)
	parentAlias := mustDisplayAlias(t, parent)

	before := map[string]string{}
	for _, id := range []string{parent, child} {
		out, errS, code := runCmd(t, []string{"show", id}, "")
		if code != 0 {
			t.Fatalf("show %s: exit code = %d, stderr = %q", id, code, errS)
		}
		before[id] = out
	}

	for _, in := range []string{parentAlias + ".0", parentAlias + ".x", parentAlias + ".", parentAlias + ".1x"} {
		out, _, code := runCmd(t, []string{"update", "--title", "changed", in}, "")
		if code == 0 {
			t.Errorf("update %q: exit code = 0, want non-zero; stdout = %q", in, out)
		}
		if !strings.Contains(out, "could not be resolved") {
			t.Errorf("update %q: stdout = %q, want alias resolution error", in, out)
		}
	}

	for _, id := range []string{parent, child} {
		out, _, _ := runCmd(t, []string{"show", id}, "")
		if out != before[id] {
			t.Errorf("show %s changed after rejected updates:\nbefore:\n%s\nafter:\n%s", id, before[id], out)
		}
	}
}
