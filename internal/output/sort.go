// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"cmp"
	"slices"
)

type BeadRow struct {
	ID                string
	Alias             string
	Title             string
	Status            string
	Priority          int
	Type              string
	ClaimedBy         string
	BlockedBy         []string
	CreatedAt         string
	CanonicalID       string
	ParentCanonicalID string
	Depth             int

	ExternalRefs []string

	EffPriority int
	EffSource   string

	ExpediteExpired bool
}

func (r BeadRow) sortPriority() int {
	if r.EffSource != "" {
		return r.EffPriority
	}
	return r.Priority
}

func SortBeads(rows []BeadRow) {
	slices.SortStableFunc(rows, func(a, b BeadRow) int {
		return cmp.Or(
			cmp.Compare(a.sortPriority(), b.sortPriority()),
			cmp.Compare(a.CreatedAt, b.CreatedAt),
			cmp.Compare(a.CanonicalID, b.CanonicalID),
		)
	})
}
