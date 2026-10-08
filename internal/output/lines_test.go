// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteFilter(t *testing.T) {
	t.Run("empty writes nothing", func(t *testing.T) {
		var buf bytes.Buffer
		WriteFilter(&buf, nil)
		if buf.String() != "" {
			t.Errorf("WriteFilter(nil) = %q, want empty", buf.String())
		}
	})

	t.Run("single condition", func(t *testing.T) {
		var buf bytes.Buffer
		WriteFilter(&buf, []Cond{{Key: "namespace", Value: "web"}})
		want := `Filter: namespace="web"` + "\n"
		if buf.String() != want {
			t.Errorf("WriteFilter() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("multiple conditions comma joined", func(t *testing.T) {
		var buf bytes.Buffer
		WriteFilter(&buf, []Cond{
			{Key: "namespace", Value: "web"},
			{Key: "model", Value: "deep"},
		})
		want := `Filter: namespace="web", model="deep"` + "\n"
		if buf.String() != want {
			t.Errorf("WriteFilter() = %q, want %q", buf.String(), want)
		}
	})
}

func TestWriteTruncated(t *testing.T) {
	t.Run("shown equals total writes nothing", func(t *testing.T) {
		var buf bytes.Buffer
		WriteTruncated(&buf, 10, 10)
		if buf.String() != "" {
			t.Errorf("WriteTruncated(10,10) = %q, want empty", buf.String())
		}
	})

	t.Run("shown less than total writes line", func(t *testing.T) {
		var buf bytes.Buffer
		WriteTruncated(&buf, 50, 120)
		want := "Truncated: showing 50 of 120\n"
		if buf.String() != want {
			t.Errorf("WriteTruncated(50,120) = %q, want %q", buf.String(), want)
		}
	})
}

func TestWriteNewlyBlockedOmitsEmpty(t *testing.T) {
	var buf bytes.Buffer
	WriteNewlyBlocked(&buf, []Cond{{Key: "namespace", Value: "web"}}, nil)
	if buf.Len() != 0 {
		t.Errorf("WriteNewlyBlocked() = %q, want empty", buf.String())
	}
}

func TestWriteNewlyReady(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		WriteNewlyReady(&buf, nil, nil)
		want := "## Newly ready\n(none)\n"
		if buf.String() != want {
			t.Errorf("WriteNewlyReady() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("with rows", func(t *testing.T) {
		var buf bytes.Buffer
		WriteNewlyReady(&buf, nil, []BeadRow{
			{ID: "bd-1", Title: "Fix bug", Status: "open", Type: "task", Priority: 1},
		})
		want := "" +
			"## Newly ready\n" +
			"- bd-1 [open/P1] Fix bug\n"
		if buf.String() != want {
			t.Errorf("WriteNewlyReady() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("with filter", func(t *testing.T) {
		var buf bytes.Buffer
		WriteNewlyReady(&buf, []Cond{{Key: "namespace", Value: "web"}}, nil)
		want := "## Newly ready\nFilter: namespace=\"web\"\n(none)\n"
		if buf.String() != want {
			t.Errorf("WriteNewlyReady() = %q, want %q", buf.String(), want)
		}
	})
}

func TestWriteRejected(t *testing.T) {
	var buf bytes.Buffer
	WriteRejected(&buf, "bd-a3f8", "not found")
	want := "Rejected: bd-a3f8 (not found)\n"
	if buf.String() != want {
		t.Errorf("WriteRejected() = %q, want %q", buf.String(), want)
	}
}

func TestWriteError(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, "bead not found")
	want := "Error: bead not found\n"
	if buf.String() != want {
		t.Errorf("WriteError() = %q, want %q", buf.String(), want)
	}
}

func TestWriteErrorFixedMessagesNotEscaped(t *testing.T) {
	cases := []string{
		`--status only accepts "cancelled"`,
		"--title is required",
		"--priority must be between 0 and 4",
		"--subject is required",
	}
	for _, msg := range cases {
		var buf bytes.Buffer
		WriteError(&buf, msg)
		want := "Error: " + msg + "\n"
		if buf.String() != want {
			t.Errorf("WriteError(%q) = %q, want %q", msg, buf.String(), want)
		}
		if strings.Contains(buf.String(), `\-`) {
			t.Errorf("WriteError(%q) = %q, contains escaped \\-", msg, buf.String())
		}
	}
}
