// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import "strings"

func normalizeInvisible(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0xE0000 && r <= 0xE007F:
			return -1
		case r == 0x200B || r == 0x200C || r == 0x200D || r == 0x2060 || r == 0xFEFF:
			return -1
		case (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069):
			return -1
		case (r <= 0x1F && r != 0x09 && r != 0x0A && r != 0x0D) || r == 0x7F || (r >= 0x80 && r <= 0x9F):
			return 0xFFFD
		default:
			return r
		}
	}, s)
}
