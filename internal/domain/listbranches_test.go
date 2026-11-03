// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"slices"
	"testing"
)

func idsOf(beads []Bead) []string {
	out := make([]string, 0, len(beads))
	for _, b := range beads {
		out = append(out, b.ID)
	}
	slices.Sort(out)
	return out
}

func TestReadyBeadsTypePriorityClaimedByFilters(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	task1 := mustCreate(t, db, CreateInput{Title: "task p1", Priority: 1, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})
	bug2 := mustCreate(t, db, CreateInput{Title: "bug p2", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	task3 := mustCreate(t, db, CreateInput{Title: "task p3", Priority: 3, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n2"})
	if _, err := db.ExecContext(ctx, `UPDATE beads SET type = ? WHERE id = ?`, "bug", bug2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE beads SET claimed_by = ? WHERE id = ?`, "alice", task3); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		f    ReadyFilter
		want []string
	}{
		{"none", ReadyFilter{Priority: -1}, []string{task1, bug2, task3}},
		{"type", ReadyFilter{Priority: -1, Type: "bug"}, []string{bug2}},
		{"priority", ReadyFilter{Priority: 1}, []string{task1}},
		{"claimed_by", ReadyFilter{Priority: -1, ClaimedBy: "alice"}, []string{task3}},
		{"type and priority", ReadyFilter{Type: "task", Priority: 3}, []string{task3}},
		{"type and priority disjoint", ReadyFilter{Type: "bug", Priority: 1}, nil},
		{"claimed_by unknown", ReadyFilter{Priority: -1, ClaimedBy: "nobody"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, total, err := ReadyBeads(ctx, db, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if got := idsOf(rows); !slices.Equal(got, want) {
				t.Fatalf("ids = %v, want %v", got, want)
			}
			if total != len(want) {
				t.Fatalf("total = %d, want %d", total, len(want))
			}
		})
	}
}

func TestApplyReadyFilterAppendsConditionsInOrder(t *testing.T) {
	query, args := applyReadyFilter("Q", []any{"a0"}, ReadyFilter{Type: "bug", Priority: 2, ClaimedBy: "bob"})
	wantQuery := "Q AND type = ? AND priority = ? AND claimed_by = ?"
	if query != wantQuery {
		t.Fatalf("query = %q, want %q", query, wantQuery)
	}
	if want := []any{"a0", "bug", 2, "bob"}; !slices.Equal(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}

	query, args = applyReadyFilter("Q", nil, ReadyFilter{Priority: -1})
	if query != "Q" || len(args) != 0 {
		t.Fatalf("empty filter changed query/args: %q %v", query, args)
	}
}

func TestListBeadsEffectiveSortAndLimit(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	p3 := mustCreate(t, db, CreateInput{Title: "p3", Priority: 3, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	p1 := mustCreate(t, db, CreateInput{Title: "p1", Priority: 1, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-02-01T00:00:00.000Z"})
	p2 := mustCreate(t, db, CreateInput{Title: "p2", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-03-01T00:00:00.000Z"})

	order := func(beads []Bead) []string {
		out := make([]string, 0, len(beads))
		for _, b := range beads {
			out = append(out, b.ID)
		}
		return out
	}

	t.Run("updated sort skips effective priority", func(t *testing.T) {
		rows, eff, total, err := ListBeadsEffective(ctx, db, ListFilter{Priority: -1, Sort: SortUpdated})
		if err != nil {
			t.Fatal(err)
		}
		if eff != nil {
			t.Fatalf("eff = %v, want nil for updated sort", eff)
		}
		if want := []string{p2, p1, p3}; !slices.Equal(order(rows), want) {
			t.Fatalf("order = %v, want %v", order(rows), want)
		}
		if total != 3 {
			t.Fatalf("total = %d, want 3", total)
		}
	})

	t.Run("priority sort", func(t *testing.T) {
		rows, eff, total, err := ListBeadsEffective(ctx, db, ListFilter{Priority: -1, Sort: SortPriority})
		if err != nil {
			t.Fatal(err)
		}
		if eff == nil {
			t.Fatal("eff = nil, want raised-priority map for priority sort")
		}
		if want := []string{p1, p2, p3}; !slices.Equal(order(rows), want) {
			t.Fatalf("order = %v, want %v", order(rows), want)
		}
		if total != 3 {
			t.Fatalf("total = %d, want 3", total)
		}
	})

	t.Run("limit truncates but total stays", func(t *testing.T) {
		rows, _, total, err := ListBeadsEffective(ctx, db, ListFilter{Priority: -1, Sort: SortPriority, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{p1, p2}; !slices.Equal(order(rows), want) {
			t.Fatalf("order = %v, want %v", order(rows), want)
		}
		if total != 3 {
			t.Fatalf("total = %d, want 3", total)
		}
	})

	t.Run("limit at or above total keeps all", func(t *testing.T) {
		rows, _, total, err := ListBeadsEffective(ctx, db, ListFilter{Priority: -1, Sort: SortPriority, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 || total != 3 {
			t.Fatalf("rows=%d total=%d, want 3 and 3", len(rows), total)
		}
	})

	t.Run("label filter with limit", func(t *testing.T) {
		rows, _, total, err := ListBeadsEffective(ctx, db, ListFilter{Priority: -1, Sort: SortPriority, Limit: 1, Label: "nope"})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 || total != 0 {
			t.Fatalf("rows=%d total=%d, want 0 and 0", len(rows), total)
		}
	})
}

func TestRunListQueryLimitAndErrors(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	for i, title := range []string{"a", "b", "c"} {
		mustCreate(t, db, CreateInput{Title: title, Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-0" + string(rune('1'+i)) + "-01T00:00:00.000Z"})
	}

	rows, total, err := runListQuery(ctx, db, `FROM beads WHERE 1=1`, nil, "", 2, SortUpdated)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || total != 3 {
		t.Fatalf("rows=%d total=%d, want 2 and 3", len(rows), total)
	}
	if rows[0].Title != "c" || rows[1].Title != "b" {
		t.Fatalf("updated order = %q,%q, want c,b", rows[0].Title, rows[1].Title)
	}

	if _, _, err := runListQuery(ctx, db, `FROM no_such_table`, nil, "", 2, SortPriority); err == nil {
		t.Fatal("limited query on missing table: want error, got nil")
	}
	if _, _, err := runListQuery(ctx, db, `FROM no_such_table`, nil, "x", 0, SortPriority); err == nil {
		t.Fatal("unlimited query on missing table: want error, got nil")
	}
}
