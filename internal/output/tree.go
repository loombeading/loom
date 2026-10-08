// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

func BuildTree(rows []BeadRow) []BeadRow {
	present := make(map[string]bool, len(rows))
	for _, r := range rows {
		present[r.CanonicalID] = true
	}

	children := make(map[string][]BeadRow, len(rows))
	var roots []BeadRow
	for _, r := range rows {
		if r.ParentCanonicalID != "" && present[r.ParentCanonicalID] {
			children[r.ParentCanonicalID] = append(children[r.ParentCanonicalID], r)
		} else {
			roots = append(roots, r)
		}
	}

	out := make([]BeadRow, 0, len(rows))
	var walk func(r BeadRow, depth int)
	walk = func(r BeadRow, depth int) {
		r.Depth = depth
		out = append(out, r)
		for _, c := range children[r.CanonicalID] {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}
