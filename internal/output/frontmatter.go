// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import "strings"

func QuoteYAML(s string) string {
	s = normalizeLineBreaks(s, "\n")

	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
