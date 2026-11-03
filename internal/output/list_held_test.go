// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import "testing"

func TestBeadLineHeldSince(t *testing.T) {
	got := BeadLine(BeadRow{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t", HeldSince: "2026-06-01T00:00:00.000Z", EffPriority: 3, EffSource: "decayed"})
	want := "- bd-1 [open/P2] t  ← held: no demand since 2026-06-01T00:00:00.000Z  eff=P3(decayed)"
	if got != want {
		t.Errorf("BeadLine() = %q, want %q", got, want)
	}
}
