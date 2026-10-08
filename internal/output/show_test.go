// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import "testing"

func TestChildrenLineOmitsZeroCountsInGivenOrder(t *testing.T) {
	got := ChildrenLine([]ChildStatusCount{
		{Status: "open", Count: 5},
		{Status: "in_progress", Count: 0},
		{Status: "closed", Count: 3},
		{Status: "cancelled", Count: 1},
	})
	want := "open 5, closed 3, cancelled 1"
	if got != want {
		t.Errorf("ChildrenLine() = %q, want %q", got, want)
	}
}

func TestChildrenLineSingleCount(t *testing.T) {
	got := ChildrenLine([]ChildStatusCount{
		{Status: "open", Count: 1},
	})
	want := "open 1"
	if got != want {
		t.Errorf("ChildrenLine() = %q, want %q", got, want)
	}
}

func TestChildrenLineAllZeroIsEmpty(t *testing.T) {
	got := ChildrenLine([]ChildStatusCount{
		{Status: "open", Count: 0},
		{Status: "in_progress", Count: 0},
		{Status: "closed", Count: 0},
		{Status: "cancelled", Count: 0},
	})
	if got != "" {
		t.Errorf("ChildrenLine() = %q, want %q for a childless Bead", got, "")
	}
}

func TestChildrenLineEmptyInputIsEmpty(t *testing.T) {
	if got := ChildrenLine(nil); got != "" {
		t.Errorf("ChildrenLine(nil) = %q, want %q", got, "")
	}
}
