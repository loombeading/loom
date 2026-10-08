// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import "testing"

func TestCanTransition(t *testing.T) {
	statuses := []string{StatusOpen, StatusInProgress, StatusClosed, StatusCancelled}
	allowed := map[[2]string]bool{
		{StatusOpen, StatusInProgress}:      true,
		{StatusInProgress, StatusOpen}:      true,
		{StatusOpen, StatusClosed}:          true,
		{StatusInProgress, StatusClosed}:    true,
		{StatusOpen, StatusCancelled}:       true,
		{StatusInProgress, StatusCancelled}: true,
		{StatusClosed, StatusOpen}:          true,
		{StatusCancelled, StatusOpen}:       true,
	}

	for _, from := range statuses {
		for _, to := range statuses {
			want := allowed[[2]string{from, to}]
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%q, %q) = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestIsTerminalStatus(t *testing.T) {
	cases := map[string]bool{
		StatusOpen:       false,
		StatusInProgress: false,
		StatusClosed:     true,
		StatusCancelled:  true,
	}
	for status, want := range cases {
		if got := IsTerminalStatus(status); got != want {
			t.Errorf("IsTerminalStatus(%q) = %v, want %v", status, got, want)
		}
	}
}
