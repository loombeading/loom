// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"reflect"
	"testing"
)

func TestSortBeads(t *testing.T) {
	rows := []BeadRow{
		{CanonicalID: "bd-3", Priority: 1, CreatedAt: "2026-01-01T00:00:00Z"},
		{CanonicalID: "bd-1", Priority: 0, CreatedAt: "2026-01-02T00:00:00Z"},
		{CanonicalID: "bd-2", Priority: 0, CreatedAt: "2026-01-01T00:00:00Z"},
		{CanonicalID: "bd-0", Priority: 0, CreatedAt: "2026-01-01T00:00:00Z"},
	}
	SortBeads(rows)

	want := []string{"bd-0", "bd-2", "bd-1", "bd-3"}
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.CanonicalID
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SortBeads() order = %v, want %v", got, want)
	}
}
