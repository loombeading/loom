// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

//go:build !lmtestclock

package domain

import "time"

func Now(func(string) string) (time.Time, error) {
	return time.Now().UTC(), nil
}

func ClockWarning(env func(string) string) string {
	if env("LM_NOW") == "" {
		return ""
	}
	return "warning: LM_NOW is ignored by release builds\n"
}
