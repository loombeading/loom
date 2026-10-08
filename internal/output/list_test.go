// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteBeadList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, nil)
		want := "(none)\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList(nil) = %q, want %q", buf.String(), want)
		}
	})

	t.Run("plain row", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-95d7", Status: "open", Type: "task", Priority: 2, Title: "Fix bug"},
		})
		want := "- bd-95d7 [open/P2] Fix bug\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("row with alias shows alias only, no parenthesized short ID", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-95d7", Alias: "bd-b9d4.1", Status: "open", Type: "task", Priority: 2, Title: "Fix bug"},
		})
		want := "- bd-b9d4.1 [open/P2] Fix bug\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("row with depth is indented 2 spaces per level", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "root"},
			{ID: "bd-2", Alias: "bd-1.1", Status: "open", Type: "task", Priority: 2, Title: "child", Depth: 1},
			{ID: "bd-3", Alias: "bd-1.1.1", Status: "open", Type: "task", Priority: 2, Title: "grandchild", Depth: 2},
		})
		want := "- bd-1 [open/P2] root\n" +
			"  - bd-1.1 [open/P2] child\n" +
			"    - bd-1.1.1 [open/P2] grandchild\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("row with claimed_by appends @claimed_by", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t", ClaimedBy: "alice"},
		})
		want := "- bd-1 [open/P2] t  @alice\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("row with blocked by appends trailing item", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t", BlockedBy: []string{"bd-2", "gate:bd-3"}},
		})
		want := "- bd-1 [open/P2] t  ← blocked by: bd-2, gate:bd-3\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("title is escaped", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "a|b"},
		})
		want := `- bd-1 [open/P2] a\|b` + "\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("multiple rows", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t1"},
			{ID: "bd-2", Status: "in_progress", Type: "bug", Priority: 0, Title: "t2"},
		})
		want := "- bd-1 [open/P2] t1\n- bd-2 [bug/in_progress/P0] t2\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("task type is omitted from the bracket", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t"},
		})
		if strings.Contains(buf.String(), "/task/") {
			t.Errorf("WriteBeadList() = %q, want no \"/task/\" in the bracket", buf.String())
		}
	})

	t.Run("non-task types keep the type in the bracket", func(t *testing.T) {
		for _, typ := range []string{"epic", "bug", "feature", "gate"} {
			var buf bytes.Buffer
			WriteBeadList(&buf, []BeadRow{
				{ID: "bd-1", Status: "open", Type: typ, Priority: 2, Title: "t"},
			})
			want := "- bd-1 [" + typ + "/open/P2] t\n"
			if buf.String() != want {
				t.Errorf("WriteBeadList() type=%s = %q, want %q", typ, buf.String(), want)
			}
		}
	})

	t.Run("external_refs pull request URL shows #<n> in place of status", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t", ExternalRefs: []string{"https://github.com/example/repo/pull/105"}},
		})
		want := "- bd-1 [#105/P2] t\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("external_refs pull request URL on a closed Bead keeps status alongside #<n>", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "closed", Type: "task", Priority: 2, Title: "t", ExternalRefs: []string{"https://github.com/example/repo/pull/105"}},
		})
		want := "- bd-1 [closed/#105/P2] t\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("external_refs pull request URL on a cancelled Bead keeps status alongside #<n>", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "cancelled", Type: "task", Priority: 2, Title: "t", ExternalRefs: []string{"https://github.com/example/repo/pull/105"}},
		})
		want := "- bd-1 [cancelled/#105/P2] t\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("external_refs pull request URL on a closed Bead with non-task type keeps type, status, and #<n>", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "closed", Type: "epic", Priority: 1, Title: "t", ExternalRefs: []string{"https://github.com/example/repo/pull/12"}},
		})
		want := "- bd-1 [epic/closed/#12/P1] t\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("external_refs pull request URL on an in_progress Bead still shows #<n> only (non-terminal)", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "in_progress", Type: "task", Priority: 2, Title: "t", ExternalRefs: []string{"https://github.com/example/repo/pull/105"}},
		})
		want := "- bd-1 [#105/P2] t\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("external_refs that is not a pull request URL renders nothing", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t", ExternalRefs: []string{"JIRA-123"}},
		})
		want := "- bd-1 [open/P2] t\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("empty external_refs renders nothing", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t", ExternalRefs: nil},
		})
		want := "- bd-1 [open/P2] t\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("external_refs containing | and a newline is dropped, not escaped", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t", ExternalRefs: []string{"a|b\nc"}},
		})
		want := "- bd-1 [open/P2] t\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("external_refs uses the last matching GitHub PR URL from the end", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{
			{ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t", ExternalRefs: []string{
				"https://github.com/example/repo/pull/1",
				"jira:PROJ-1",
				"https://github.com/example/repo/pull/2",
			}},
		})
		want := "- bd-1 [#2/P2] t\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})

	t.Run("item order: claimed_by, blocked by", func(t *testing.T) {
		var buf bytes.Buffer
		WriteBeadList(&buf, []BeadRow{{
			ID: "bd-1", Status: "open", Type: "task", Priority: 2, Title: "t",
			ClaimedBy: "alice", BlockedBy: []string{"bd-2"},
			ExternalRefs: []string{"https://github.com/example/repo/pull/1"},
		}})
		want := "- bd-1 [#1/P2] t  @alice  ← blocked by: bd-2\n"
		if buf.String() != want {
			t.Errorf("WriteBeadList() = %q, want %q", buf.String(), want)
		}
	})
}
