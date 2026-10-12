// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import "fmt"

func isForbiddenIdentRune(r rune) bool {
	switch {
	case r <= 0x1F || r == 0x7F || (r >= 0x80 && r <= 0x9F):
		return true
	case (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069):
		return true
	case r == 0x200B || r == 0x200C || r == 0x200D || r == 0x2060 || r == 0xFEFF:
		return true
	case r >= 0xE0000 && r <= 0xE007F:
		return true
	default:
		return false
	}
}

func ValidateIdentValues(field string, values ...string) error {
	for _, s := range values {
		if r, bad := ForbiddenIdentRune(s); bad {
			return &ValidationError{Msg: fmt.Sprintf("%s contains forbidden character U+%04X", field, r)}
		}
	}
	return nil
}

func ForbiddenIdentRune(s string) (rune, bool) {
	for _, r := range s {
		if isForbiddenIdentRune(r) {
			return r, true
		}
	}
	return 0, false
}
