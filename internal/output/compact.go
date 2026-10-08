// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"io"
	"time"
)

const DefaultRetentionDays = 0

const CompactedLineFormat = "Compacted: description omitted (closed more than retention_days=%d days ago); use `lm export` or `lm show --full` to see the full record"

func Compacted(status string, closedAtValid bool, closedAt string, hasSummary bool, retentionDays int, now time.Time) bool {
	if status != "closed" && status != "cancelled" {
		return false
	}
	if !closedAtValid {
		return false
	}
	if !hasSummary {
		return false
	}
	t, err := time.Parse(time.RFC3339, closedAt)
	if err != nil {
		return false
	}
	return t.Add(time.Duration(retentionDays) * 24 * time.Hour).Before(now)
}

func WriteDescription(w io.Writer, compacted bool, descriptionValid bool, description string, summary string, retentionDays int) {
	fmt.Fprintln(w, "## Description")
	fmt.Fprintln(w)
	switch {
	case compacted:
		fmt.Fprint(w, EscapeBlock(summary))
	case descriptionValid:
		fmt.Fprint(w, EscapeBlock(description))
	default:
		fmt.Fprintln(w, "(none)")
	}
	if compacted {
		fmt.Fprintf(w, CompactedLineFormat+"\n", retentionDays)
	}
	fmt.Fprintln(w)
}
