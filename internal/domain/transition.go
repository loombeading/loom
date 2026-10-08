// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusClosed     = "closed"
	StatusCancelled  = "cancelled"
)

var ValidStatuses = map[string]bool{
	StatusOpen:       true,
	StatusInProgress: true,
	StatusClosed:     true,
	StatusCancelled:  true,
}

func IsTerminalStatus(status string) bool {
	return status == StatusClosed || status == StatusCancelled
}

var transitions = map[string]map[string]bool{
	StatusOpen:       {StatusInProgress: true, StatusClosed: true, StatusCancelled: true},
	StatusInProgress: {StatusOpen: true, StatusClosed: true, StatusCancelled: true},
	StatusClosed:     {StatusOpen: true},
	StatusCancelled:  {StatusOpen: true},
}

func CanTransition(from, to string) bool {
	return transitions[from][to]
}
