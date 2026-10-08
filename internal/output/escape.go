// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"regexp"
	"strings"
)

var leadingMarkdownRe = regexp.MustCompile(`^([#\-*>+]|[0-9]+\.)`)

var leadingHeadingRe = regexp.MustCompile(`^( {0,3})#`)

var leadingFenceRe = regexp.MustCompile("^( {0,3})(```|~~~)")

func Escape(s string) string {
	s = escapeInline(s)
	if leadingMarkdownRe.MatchString(s) {
		s = "\\" + s
	}
	return s
}

func normalizeLineBreaks(s, repl string) string {
	s = normalizeInvisible(s)
	s = strings.ReplaceAll(s, "\r\n", repl)
	s = strings.ReplaceAll(s, "\n", repl)
	s = strings.ReplaceAll(s, "\r", repl)
	return s
}

func escapeInline(s string) string {
	s = normalizeLineBreaks(s, " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}

func EscapeBlock(s string) string {
	s = normalizeLineBreaks(s, "\n")
	s = strings.TrimRight(s, "\n") + "\n"

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = escapeBlockLine(line)
	}
	return strings.Join(lines, "\n")
}

func escapeBlockLine(line string) string {
	if loc := leadingFenceRe.FindStringSubmatchIndex(line); loc != nil {
		insertAt := loc[4]
		return line[:insertAt] + "\\" + line[insertAt:]
	}
	if loc := leadingHeadingRe.FindStringIndex(line); loc != nil {
		insertAt := loc[1] - 1
		return line[:insertAt] + "\\" + line[insertAt:]
	}
	return line
}
