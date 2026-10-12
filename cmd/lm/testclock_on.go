// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

//go:build lmtestclock

package main

var testClockEnv = []envVar{
	{"LM_NOW", "override the current instant (test builds only)"},
}
