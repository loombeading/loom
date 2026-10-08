// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"regexp"
	"strings"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiYellow = "\x1b[33m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiRed    = "\x1b[31m"
)

var statusTagRe = regexp.MustCompile(`\[(?:[^/\]]+/)??(?:(open|in_progress|closed|cancelled)(?:/(#\d+))?|(#\d+))(/P\d+)\]`)

func Decorate(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = decorateLine(line)
	}
	return strings.Join(lines, "\n")
}

func decorateLine(line string) string {
	switch {
	case strings.HasPrefix(line, "## "):
		return line
	case strings.HasPrefix(line, "Filter:"),
		strings.HasPrefix(line, "Truncated:"),
		strings.HasPrefix(line, "Rejected:"),
		strings.HasPrefix(line, "Error:"):
		return ansiCyan + line + ansiReset
	default:
		return decorateIDAndStatusTag(line)
	}
}

func decorateIDAndStatusTag(line string) string {
	decorated := statusTagRe.ReplaceAllStringFunc(line, decorateStatusTag)
	loc := statusTagRe.FindStringIndex(line)
	if loc == nil {
		return decorated
	}
	matchStart := loc[0]
	if matchStart == 0 || line[matchStart-1] != ' ' {
		return decorated
	}
	idEnd := matchStart - 1
	idStart := strings.LastIndex(line[:idEnd], " ") + 1
	id := line[idStart:idEnd]
	dash := strings.LastIndex(id, "-")
	if dash == -1 || dash == len(id)-1 {
		return decorated
	}
	namespace := id[:dash+1]
	name := id[dash+1:]
	return line[:idStart] + namespace + ansiBold + name + ansiReset + " " + decorated[matchStart:]
}

func decorateStatusTag(m string) string {
	loc := statusTagRe.FindStringSubmatchIndex(m)
	var sb strings.Builder
	last := 0
	if loc[2] != -1 {
		status := m[loc[2]:loc[3]]
		var color string
		switch status {
		case "in_progress":
			color = ansiYellow
		case "closed", "cancelled":
			color = ansiDim
		}
		sb.WriteString(m[last:loc[2]])
		if color != "" {
			sb.WriteString(color + status + ansiReset)
		} else {
			sb.WriteString(status)
		}
		last = loc[3]
	}
	prStart, prEnd := loc[4], loc[5]
	if prStart == -1 {
		prStart, prEnd = loc[6], loc[7]
	}
	if prStart != -1 {
		sb.WriteString(m[last:prStart])
		sb.WriteString(ansiYellow + m[prStart:prEnd] + ansiReset)
		last = prEnd
	}
	sb.WriteString(m[last:loc[8]])
	priority := m[loc[8]:loc[9]]
	if priority == "/P0" {
		sb.WriteString("/" + ansiRed + ansiBold + "P0" + ansiReset)
	} else {
		sb.WriteString(priority)
	}
	sb.WriteString(m[loc[9]:])
	return sb.String()
}
