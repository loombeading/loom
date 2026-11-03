// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTruncateValue(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantCut bool
	}{
		{"empty", "", "", false},
		{"exactly 80 runes", strings.Repeat("a", 80), strings.Repeat("a", 80), false},
		{"81 runes", strings.Repeat("a", 81), strings.Repeat("a", 80) + "…", true},
		{"80 multibyte runes", strings.Repeat("あ", 80), strings.Repeat("あ", 80), false},
		{"81 multibyte runes", strings.Repeat("あ", 81), strings.Repeat("あ", 80) + "…", true},
		{"line break becomes space", "a\nb", "a b", false},
		{"leading marker escaped", "- x", "\\- x", false},
		{"pipe escaped", "a|b", "a\\|b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cut := TruncateValue(tt.in)
			if got != tt.want || cut != tt.wantCut {
				t.Errorf("TruncateValue(%q) = (%q, %v), want (%q, %v)", tt.in, got, cut, tt.want, tt.wantCut)
			}
		})
	}
}

func TestLinkLine(t *testing.T) {
	tests := []struct {
		name string
		row  DepRow
		want string
	}{
		{
			name: "received link",
			row:  DepRow{Type: "blocks", Display: "lm-a", Status: "open", BeadType: "task", Priority: 2, Title: "Title"},
			want: "- blocks ← lm-a [open/P2] Title",
		},
		{
			name: "sent link",
			row:  DepRow{Type: "parent-child", Sent: true, Display: "lm-b", Status: "closed", BeadType: "task", Priority: 1, Title: "Child"},
			want: "- parent-child → lm-b [closed/P1] Child",
		},
		{
			name: "dangling stops after marker",
			row:  DepRow{Type: "blocks", Sent: true, Dangling: true, Display: "lm-x", Status: "open", Priority: 2, Title: "ignored"},
			want: "- blocks → lm-x (dangling)",
		},
		{
			name: "removed suffix",
			row:  DepRow{Type: "duplicates", Display: "lm-c", Status: "cancelled", BeadType: "task", Priority: 3, Title: "Gone", Removed: true},
			want: "- duplicates ← lm-c [cancelled/P3] Gone  (removed)",
		},
		{
			name: "non-default type and pull request ref",
			row: DepRow{
				Type: "discovered-from", Display: "lm-d", Status: "open", BeadType: "epic", Priority: 0, Title: "Epic",
				ExternalRefs: []string{"https://github.com/o/r/pull/42"},
			},
			want: "- discovered-from ← lm-d [epic/#42/P0] Epic",
		},
		{
			name: "terminal status with pull request ref",
			row: DepRow{
				Type: "blocks", Display: "lm-e", Status: "closed", BeadType: "task", Priority: 2, Title: "Done",
				ExternalRefs: []string{"https://github.com/o/r/pull/7"},
			},
			want: "- blocks ← lm-e [closed/#7/P2] Done",
		},
		{
			name: "title and display escaped",
			row:  DepRow{Type: "blocks", Display: "a|b", Status: "open", BeadType: "task", Priority: 2, Title: "- item"},
			want: "- blocks ← a\\|b [open/P2] \\- item",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := linkLine(tt.row); got != tt.want {
				t.Errorf("linkLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteDeps(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		WriteDeps(&buf, nil)
		if got, want := buf.String(), "(none)\n"; got != want {
			t.Errorf("WriteDeps(nil) = %q, want %q", got, want)
		}
	})
	t.Run("multiple rows", func(t *testing.T) {
		var buf bytes.Buffer
		WriteDeps(&buf, []DepRow{
			{Type: "blocks", Display: "lm-a", Status: "open", BeadType: "task", Priority: 2, Title: "A"},
			{Type: "parent-child", Sent: true, Dangling: true, Display: "lm-b"},
		})
		want := "- blocks ← lm-a [open/P2] A\n- parent-child → lm-b (dangling)\n"
		if got := buf.String(); got != want {
			t.Errorf("WriteDeps() = %q, want %q", got, want)
		}
	})
}

func TestWriteDerived(t *testing.T) {
	t.Run("empty writes nothing", func(t *testing.T) {
		var buf bytes.Buffer
		WriteDerived(&buf, nil)
		if got := buf.String(); got != "" {
			t.Errorf("WriteDerived(nil) = %q, want empty", got)
		}
	})
	t.Run("multiple rows", func(t *testing.T) {
		var buf bytes.Buffer
		WriteDerived(&buf, []DepRow{
			{Display: "lm-a", Status: "open", BeadType: "task", Priority: 2, Title: "A"},
			{Display: "lm-b", Dangling: true, Title: "ignored"},
			{Display: "lm-c|d", Status: "closed", BeadType: "epic", Priority: 1, Title: "# C", ExternalRefs: []string{"https://github.com/o/r/pull/9"}},
		})
		want := "- lm-a [open/P2] A\n- lm-b (dangling)\n- lm-c\\|d [epic/closed/#9/P1] \\# C\n"
		if got := buf.String(); got != want {
			t.Errorf("WriteDerived() = %q, want %q", got, want)
		}
	})
}

func TestHistoryLine(t *testing.T) {
	tests := []struct {
		name string
		row  HistoryRow
		want string
	}{
		{
			name: "created",
			row:  HistoryRow{Time: "2026-01-02T03:04:05.000Z", Actor: "alice", Created: "title, status"},
			want: "- 2026-01-02T03:04:05.000Z alice created (title, status)",
		},
		{
			name: "created ignores other fields",
			row:  HistoryRow{Time: "t", Actor: "a", Created: "x", Kind: "field", HasField: true, Field: "f", HasValue: true, Value: "v", HasReason: true, Reason: "r"},
			want: "- t a created (x)",
		},
		{
			name: "kind only",
			row:  HistoryRow{Time: "t", Actor: "a", Kind: "note"},
			want: "- t a note",
		},
		{
			name: "field only",
			row:  HistoryRow{Time: "t", Actor: "a", Kind: "field", HasField: true, Field: "status"},
			want: "- t a field status",
		},
		{
			name: "field and value",
			row:  HistoryRow{Time: "t", Actor: "a", Kind: "field", HasField: true, Field: "status", HasValue: true, Value: "closed"},
			want: "- t a field status = closed",
		},
		{
			name: "value only",
			row:  HistoryRow{Time: "t", Actor: "a", Kind: "note", HasValue: true, Value: "v"},
			want: "- t a note = v",
		},
		{
			name: "reason only",
			row:  HistoryRow{Time: "t", Actor: "a", Kind: "note", HasReason: true, Reason: "because"},
			want: "- t a note  (because)",
		},
		{
			name: "field value and reason",
			row:  HistoryRow{Time: "t", Actor: "a", Kind: "field", HasField: true, Field: "priority", HasValue: true, Value: "1", HasReason: true, Reason: "r"},
			want: "- t a field priority = 1  (r)",
		},
		{
			name: "present flag with empty text still prints",
			row:  HistoryRow{Time: "t", Actor: "a", Kind: "field", HasField: true, HasValue: true, HasReason: true},
			want: "- t a field  =   ()",
		},
		{
			name: "absent flags hide text",
			row:  HistoryRow{Time: "t", Actor: "a", Kind: "field", Field: "f", Value: "v", Reason: "r"},
			want: "- t a field",
		},
		{
			name: "actor and time escaped",
			row:  HistoryRow{Time: "a|b", Actor: "- x", Kind: "note"},
			want: "- a\\|b \\- x note",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := historyLine(tt.row); got != tt.want {
				t.Errorf("historyLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteHistory(t *testing.T) {
	const truncated = "Truncated: history values cut at 80 characters; use `lm show --full` to see them in full"
	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		WriteHistory(&buf, nil)
		if got, want := buf.String(), "(none)\n"; got != want {
			t.Errorf("WriteHistory(nil) = %q, want %q", got, want)
		}
	})
	t.Run("no cut row omits notice", func(t *testing.T) {
		var buf bytes.Buffer
		WriteHistory(&buf, []HistoryRow{
			{Time: "t1", Actor: "a", Created: "title"},
			{Time: "t2", Actor: "a", Kind: "note"},
		})
		want := "- t1 a created (title)\n- t2 a note\n"
		if got := buf.String(); got != want {
			t.Errorf("WriteHistory() = %q, want %q", got, want)
		}
	})
	t.Run("cut row appends notice once", func(t *testing.T) {
		var buf bytes.Buffer
		WriteHistory(&buf, []HistoryRow{
			{Time: "t1", Actor: "a", Kind: "note", Cut: true},
			{Time: "t2", Actor: "a", Kind: "note"},
			{Time: "t3", Actor: "a", Kind: "note", Cut: true},
		})
		want := "- t1 a note\n- t2 a note\n- t3 a note\n" + truncated + "\n"
		if got := buf.String(); got != want {
			t.Errorf("WriteHistory() = %q, want %q", got, want)
		}
	})
	t.Run("cut on last row only", func(t *testing.T) {
		var buf bytes.Buffer
		WriteHistory(&buf, []HistoryRow{
			{Time: "t1", Actor: "a", Kind: "note"},
			{Time: "t2", Actor: "a", Kind: "note", Cut: true},
		})
		want := "- t1 a note\n- t2 a note\n" + truncated + "\n"
		if got := buf.String(); got != want {
			t.Errorf("WriteHistory() = %q, want %q", got, want)
		}
	})
}

func TestChildrenLineOmitsZeroCountsInGivenOrder(t *testing.T) {
	got := ChildrenLine([]ChildStatusCount{
		{Status: "open", Count: 5},
		{Status: "in_progress", Count: 0},
		{Status: "closed", Count: 3},
		{Status: "cancelled", Count: 1},
	})
	want := "open 5, closed 3, cancelled 1"
	if got != want {
		t.Errorf("ChildrenLine() = %q, want %q", got, want)
	}
}

func TestChildrenLineSingleCount(t *testing.T) {
	got := ChildrenLine([]ChildStatusCount{
		{Status: "open", Count: 1},
	})
	want := "open 1"
	if got != want {
		t.Errorf("ChildrenLine() = %q, want %q", got, want)
	}
}

func TestChildrenLineAllZeroIsEmpty(t *testing.T) {
	got := ChildrenLine([]ChildStatusCount{
		{Status: "open", Count: 0},
		{Status: "in_progress", Count: 0},
		{Status: "closed", Count: 0},
		{Status: "cancelled", Count: 0},
	})
	if got != "" {
		t.Errorf("ChildrenLine() = %q, want %q for a childless Bead", got, "")
	}
}

func TestChildrenLineEmptyInputIsEmpty(t *testing.T) {
	if got := ChildrenLine(nil); got != "" {
		t.Errorf("ChildrenLine(nil) = %q, want %q", got, "")
	}
}
