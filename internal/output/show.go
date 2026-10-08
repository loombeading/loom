// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"io"
	"strings"
)

const showValueMax = 80

func TruncateValue(s string) (string, bool) {
	s = Escape(s)
	r := []rune(s)
	if len(r) <= showValueMax {
		return s, false
	}
	return string(r[:showValueMax]) + "…", true
}

type DepRow struct {
	Type string

	Sent bool

	Dangling bool

	Display  string
	Status   string
	BeadType string
	Priority int
	Title    string

	ExternalRefs []string

	Removed bool
}

func WriteDeps(w io.Writer, rows []DepRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, noneLine)
		return
	}
	for _, r := range rows {
		fmt.Fprintln(w, linkLine(r))
	}
}

func linkLine(r DepRow) string {
	arrow := "←"
	if r.Sent {
		arrow = "→"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "- %s %s %s", Escape(r.Type), arrow, r.Display)
	if r.Dangling {
		sb.WriteString(" (dangling)")
		return sb.String()
	}
	fmt.Fprintf(&sb, " %s %s", statusPriorityTypeBracket(r.Status, r.Priority, r.BeadType, r.ExternalRefs), Escape(r.Title))
	if r.Removed {
		sb.WriteString("  (removed)")
	}
	return sb.String()
}

func WriteDerived(w io.Writer, rows []DepRow) {
	for _, r := range rows {
		fmt.Fprintln(w, derivedLine(r))
	}
}

func derivedLine(r DepRow) string {
	if r.Dangling {
		return fmt.Sprintf("- %s (dangling)", r.Display)
	}
	return fmt.Sprintf("- %s %s %s", r.Display, statusPriorityTypeBracket(r.Status, r.Priority, r.BeadType, r.ExternalRefs), Escape(r.Title))
}

type ChildStatusCount struct {
	Status string
	Count  int
}

func ChildrenLine(counts []ChildStatusCount) string {
	parts := make([]string, 0, len(counts))
	for _, c := range counts {
		if c.Count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d", c.Status, c.Count))
	}
	return strings.Join(parts, ", ")
}

type HistoryRow struct {
	Time  string
	Actor string

	Created string

	Kind      string
	HasField  bool
	Field     string
	HasValue  bool
	Value     string
	HasReason bool
	Reason    string

	Cut bool
}

func WriteHistory(w io.Writer, rows []HistoryRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, noneLine)
		return
	}
	cut := false
	for _, r := range rows {
		fmt.Fprintln(w, historyLine(r))
		cut = cut || r.Cut
	}
	if cut {
		fmt.Fprintln(w, HistoryTruncatedLine)
	}
}

func historyLine(r HistoryRow) string {
	if r.Created != "" {
		return fmt.Sprintf("- %s %s created (%s)", r.Time, Escape(r.Actor), Escape(r.Created))
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "- %s %s %s", r.Time, Escape(r.Actor), Escape(r.Kind))
	if r.HasField {
		fmt.Fprintf(&sb, " %s", Escape(r.Field))
	}
	if r.HasValue {
		fmt.Fprintf(&sb, " = %s", r.Value)
	}
	if r.HasReason {
		fmt.Fprintf(&sb, "  (%s)", r.Reason)
	}
	return sb.String()
}
