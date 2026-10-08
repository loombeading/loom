// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import "testing"

func row(id, parent string) BeadRow {
	return BeadRow{ID: id, CanonicalID: id, ParentCanonicalID: parent}
}

func TestBuildTree(t *testing.T) {
	t.Run("parent, child, grandchild nest in order", func(t *testing.T) {
		in := []BeadRow{
			row("epic", ""),
			row("child", "epic"),
			row("grandchild", "child"),
		}
		got := BuildTree(in)
		want := []string{"epic", "child", "grandchild"}
		if len(got) != len(want) {
			t.Fatalf("BuildTree() = %v, want %d rows", got, len(want))
		}
		for i, id := range want {
			if got[i].CanonicalID != id {
				t.Errorf("row %d CanonicalID = %q, want %q", i, got[i].CanonicalID, id)
			}
		}
		if got[0].Depth != 0 || got[1].Depth != 1 || got[2].Depth != 2 {
			t.Errorf("depths = [%d %d %d], want [0 1 2]", got[0].Depth, got[1].Depth, got[2].Depth)
		}
	})

	t.Run("siblings keep contract order under their parent", func(t *testing.T) {
		in := []BeadRow{
			row("epic", ""),
			row("b", "epic"),
			row("a", "epic"),
		}
		got := BuildTree(in)
		want := []string{"epic", "b", "a"}
		for i, id := range want {
			if got[i].CanonicalID != id {
				t.Errorf("row %d CanonicalID = %q, want %q", i, got[i].CanonicalID, id)
			}
		}
	})

	t.Run("child whose parent is not in the result set becomes a root", func(t *testing.T) {
		in := []BeadRow{
			row("child", "missing-parent"),
		}
		got := BuildTree(in)
		if len(got) != 1 || got[0].CanonicalID != "child" || got[0].Depth != 0 {
			t.Errorf("BuildTree() = %v, want a single root row for %q", got, "child")
		}
	})

	t.Run("multiple root Beads with no parent-child relation stay flat", func(t *testing.T) {
		in := []BeadRow{
			row("a", ""),
			row("b", ""),
		}
		got := BuildTree(in)
		if len(got) != 2 || got[0].Depth != 0 || got[1].Depth != 0 {
			t.Errorf("BuildTree() = %v, want two depth-0 roots", got)
		}
	})
}
