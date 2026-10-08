// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

func ResolveActor(flag string, env func(string) string, osUsername string) string {
	if flag != "" {
		return flag
	}
	if v := env("LM_ACTOR"); v != "" {
		return v
	}
	if v := env("CLAUDE_CODE_SESSION_ID"); v != "" {
		return v
	}
	return osUsername
}
