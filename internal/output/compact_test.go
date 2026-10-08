// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"testing"
	"time"
)

func TestCompactedJudgment(t *testing.T) {
	now := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		status        string
		closedAtValid bool
		closedAt      string
		hasSummary    bool
		retentionDays int
		want          bool
	}{
		{"open bead never compacts", "open", false, "", true, 30, false},
		{"in_progress bead never compacts", "in_progress", false, "", true, 30, false},
		{"closed within retention", "closed", true, "2026-01-15T00:00:00Z", true, 30, false},
		{"closed exceeding retention", "closed", true, "2025-01-01T00:00:00Z", true, 30, true},
		{"cancelled exceeding retention", "cancelled", true, "2025-01-01T00:00:00Z", true, 30, true},
		{"closed with no closed_at", "closed", false, "", true, 30, false},
		{"closed with unparsable closed_at fails open", "closed", true, "not-a-time", true, 30, false},
		{"closed exceeding retention without a summary never compacts", "closed", true, "2025-01-01T00:00:00Z", false, 30, false},
		{"cancelled exceeding retention without a summary never compacts", "cancelled", true, "2025-01-01T00:00:00Z", false, 30, false},
		{"closed exceeding retention without a summary at retention_days=0", "closed", true, "2025-01-01T00:00:00Z", false, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compacted(tt.status, tt.closedAtValid, tt.closedAt, tt.hasSummary, tt.retentionDays, now)
			if got != tt.want {
				t.Errorf("Compacted(%q, %v, %q, %v, %d, now) = %v, want %v", tt.status, tt.closedAtValid, tt.closedAt, tt.hasSummary, tt.retentionDays, got, tt.want)
			}
		})
	}
}

func TestWriteDescriptionNotCompacted(t *testing.T) {
	var buf bytes.Buffer
	WriteDescription(&buf, false, true, "the body", "", 30)
	got := buf.String()
	want := "## Description\n\nthe body\n\n"
	if got != want {
		t.Errorf("WriteDescription = %q, want %q", got, want)
	}
}

func TestWriteDescriptionNotCompactedNoDescription(t *testing.T) {
	var buf bytes.Buffer
	WriteDescription(&buf, false, false, "", "", 30)
	got := buf.String()
	want := "## Description\n\n(none)\n\n"
	if got != want {
		t.Errorf("WriteDescription = %q, want %q", got, want)
	}
}

func TestWriteDescriptionCompactedWithSummary(t *testing.T) {
	var buf bytes.Buffer
	WriteDescription(&buf, true, true, "the body", "the summary", 30)
	got := buf.String()
	want := "## Description\n\nthe summary\n" + "Compacted: description omitted (closed more than retention_days=30 days ago); use `lm export` or `lm show --full` to see the full record\n\n"
	if got != want {
		t.Errorf("WriteDescription = %q, want %q", got, want)
	}
}

func TestWriteDescriptionNotCompactedMultilineBody(t *testing.T) {
	var buf bytes.Buffer
	WriteDescription(&buf, false, true, "line one\n#not a heading\nline three", "", 30)
	got := buf.String()
	want := "## Description\n\nline one\n\\#not a heading\nline three\n\n"
	if got != want {
		t.Errorf("WriteDescription = %q, want %q", got, want)
	}
}

func TestWriteDescriptionCompactedMultilineSummary(t *testing.T) {
	var buf bytes.Buffer
	WriteDescription(&buf, true, true, "the body", "line one\nline two", 30)
	got := buf.String()
	want := "## Description\n\nline one\nline two\n" + "Compacted: description omitted (closed more than retention_days=30 days ago); use `lm export` or `lm show --full` to see the full record\n\n"
	if got != want {
		t.Errorf("WriteDescription = %q, want %q", got, want)
	}
}
