// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

const defaultBeadType = "task"

var pullRequestRefRe = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/pull/(\d+)$`)

func WriteBeadList(w io.Writer, rows []BeadRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, noneLine)
		return
	}
	for _, r := range rows {
		fmt.Fprintln(w, beadLine(r))
	}
}

func statusPriorityTypeBracket(status string, priority int, typ string, refs []string) string {
	parts := make([]string, 0, 4)
	if typ != "" && typ != defaultBeadType {
		parts = append(parts, typ)
	}
	n := prNumber(refs)
	switch {
	case n != "" && isTerminalStatus(status):
		parts = append(parts, status, "#"+n)
	case n != "":
		parts = append(parts, "#"+n)
	default:
		parts = append(parts, status)
	}
	parts = append(parts, fmt.Sprintf("P%d", priority))
	return "[" + strings.Join(parts, "/") + "]"
}

func isTerminalStatus(status string) bool {
	return status == "closed" || status == "cancelled"
}

func BeadLine(r BeadRow) string {
	return beadLine(r)
}

func beadLine(r BeadRow) string {
	id := r.ID
	if r.Alias != "" {
		id = r.Alias
	}
	var sb strings.Builder
	sb.WriteString(strings.Repeat("  ", r.Depth))
	fmt.Fprintf(&sb, "- %s %s %s", id, statusPriorityTypeBracket(r.Status, r.Priority, r.Type, r.ExternalRefs), Escape(r.Title))
	if r.ClaimedBy != "" {
		fmt.Fprintf(&sb, "  @%s", r.ClaimedBy)
	}
	if len(r.BlockedBy) > 0 {
		fmt.Fprintf(&sb, "  ← blocked by: %s", strings.Join(r.BlockedBy, ", "))
	}
	if r.EffSource != "" {
		fmt.Fprintf(&sb, "  eff=P%d(%s)", r.EffPriority, r.EffSource)
	}
	if r.ExpediteExpired {
		sb.WriteString(" expedite-expired")
	}
	return sb.String()
}

func WriteCreated(w io.Writer, r BeadRow) {
	id := r.ID
	if r.Alias != "" {
		id = r.Alias
	}
	fmt.Fprintf(w, "- Created: %s %s %s\n", id, statusPriorityTypeBracket(r.Status, r.Priority, r.Type, r.ExternalRefs), Escape(r.Title))
}

func prNumber(refs []string) string {
	for _, ref := range slices.Backward(refs) {
		if m := pullRequestRefRe.FindStringSubmatch(ref); m != nil {
			return m[1]
		}
	}
	return ""
}
