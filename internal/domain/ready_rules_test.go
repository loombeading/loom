// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
)

type sqlLogQuerier struct {
	db      *sql.DB
	queries []string
}

func (r *sqlLogQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.queries = append(r.queries, query)
	return r.db.QueryContext(ctx, query, args...)
}

func (r *sqlLogQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	r.queries = append(r.queries, query)
	return r.db.QueryRowContext(ctx, query, args...)
}

func (r *sqlLogQuerier) count(pred func(string) bool) int {
	n := 0
	for _, q := range r.queries {
		if pred(q) {
			n++
		}
	}
	return n
}

func sortedIDs(beads []Bead) []string {
	ids := make([]string, len(beads))
	for i, b := range beads {
		ids[i] = b.ID
	}
	slices.Sort(ids)
	return ids
}

func TestReadyNamespaceFilterKeepsOutsideNamespaceBlocker(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	blocker := mustCreate(t, db, CreateInput{Title: "blocker", Priority: 2, BeadType: "task", Namespace: "web", Actor: "u", Now: "n0"})
	blocked := mustCreate(t, db, CreateInput{Title: "blocked", Priority: 2, BeadType: "task", Namespace: "lm", BlockedBy: []string{blocker}, Actor: "u", Now: "n1"})
	free := mustCreate(t, db, CreateInput{Title: "free", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n2"})

	f := ReadyFilter{Namespace: "lm", Priority: -1}
	ready, _, err := ReadyBeads(ctx, db, f)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedIDs(ready); !slices.Equal(got, []string{free}) {
		t.Fatalf("ready --namespace lm = %v, want only %q (blocker in another namespace still blocks %q)", got, free, blocked)
	}

	blockedRows, _, _, err := BlockedBeadsEffective(ctx, db, f)
	if err != nil {
		t.Fatal(err)
	}
	var found *BlockedBead
	for i := range blockedRows {
		if blockedRows[i].ID == blocked {
			found = &blockedRows[i]
		}
	}
	if found == nil {
		t.Fatalf("blocked --namespace lm = %v, want %q included", blockedRows, blocked)
	}
	if len(found.Blockers) != 1 || found.Blockers[0].ID != blocker {
		t.Fatalf("blockers of %q = %v, want just %q", blocked, found.Blockers, blocker)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseBead(ctx, tx, CloseInput{ID: blocker, Actor: "u", Now: "n3"}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	ready, _, err = ReadyBeads(ctx, db, f)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{blocked, free}
	slices.Sort(want)
	if got := sortedIDs(ready); !slices.Equal(got, want) {
		t.Fatalf("ready --namespace lm after closing the blocker = %v, want %v", got, want)
	}
}

func TestListLimitPushedDownToSQLWithCountTotal(t *testing.T) {
	db := testDB(t)
	for _, title := range []string{"a", "b", "c", "d", "e"} {
		mustCreate(t, db, CreateInput{Title: title, Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})
	}
	rec := &sqlLogQuerier{db: db}

	beads, total, err := ListBeads(context.Background(), rec, ListFilter{Priority: -1, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(beads) != 2 || total != 5 {
		t.Fatalf("ListBeads limit=2 = %d rows, total %d; want 2 rows, total 5", len(beads), total)
	}
	if n := rec.count(func(q string) bool { return strings.Contains(q, "LIMIT ?") }); n != 1 {
		t.Errorf("queries with LIMIT ? = %d, want 1; queries: %q", n, rec.queries)
	}
	if n := rec.count(func(q string) bool { return strings.HasPrefix(strings.TrimSpace(q), "SELECT COUNT(*)") }); n != 1 {
		t.Errorf("SELECT COUNT(*) queries = %d, want 1; queries: %q", n, rec.queries)
	}
}

func TestListLabelFilterReadsAllThenTruncates(t *testing.T) {
	db := testDB(t)
	for _, title := range []string{"n1", "n2", "n3", "n4"} {
		mustCreate(t, db, CreateInput{Title: title, Priority: 1, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})
	}
	for _, title := range []string{"x1", "x2", "x3"} {
		mustCreate(t, db, CreateInput{Title: title, Priority: 3, BeadType: "task", Labels: []string{"x"}, Namespace: "lm", Actor: "u", Now: "n0"})
	}
	rec := &sqlLogQuerier{db: db}

	beads, total, err := ListBeads(context.Background(), rec, ListFilter{Priority: -1, Label: "x", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(beads) != 2 || total != 3 {
		t.Fatalf("ListBeads label=x limit=2 = %d rows, total %d; want 2 rows, total 3", len(beads), total)
	}
	for _, b := range beads {
		if !slices.Contains(b.Labels, "x") {
			t.Errorf("row %q has labels %v, want label x", b.ID, b.Labels)
		}
	}
	if n := rec.count(func(q string) bool { return strings.Contains(q, "LIMIT ?") }); n != 0 {
		t.Errorf("queries with LIMIT ? = %d, want 0 under --label; queries: %q", n, rec.queries)
	}
	if n := rec.count(func(q string) bool { return strings.Contains(q, "COUNT(*)") }); n != 0 {
		t.Errorf("COUNT(*) queries = %d, want 0 under --label; queries: %q", n, rec.queries)
	}
}
