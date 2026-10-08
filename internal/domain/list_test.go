// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"testing"
)

func TestListBeadsDefaultExcludesTerminal(t *testing.T) {
	db := testDB(t)
	open := mustCreate(t, db, CreateInput{Title: "open", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})
	closed := mustCreate(t, db, CreateInput{Title: "closed", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseBead(context.Background(), tx, CloseInput{ID: closed, Actor: "u", Now: "n2"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	rows, _, err := ListBeads(context.Background(), db, ListFilter{Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != open {
		t.Fatalf("default list = %v, want just %q", rows, open)
	}

	all, _, err := ListBeads(context.Background(), db, ListFilter{All: true, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("--all list = %v, want 2 rows", all)
	}
}

func TestListBeadsFilters(t *testing.T) {
	db := testDB(t)
	_ = mustCreate(t, db, CreateInput{Title: "a", Priority: 1, BeadType: "task", Labels: []string{"x"}, Namespace: "lm", Actor: "u", Now: "n0"})
	b := mustCreate(t, db, CreateInput{Title: "b", Priority: 3, BeadType: "task", Labels: []string{"y"}, Namespace: "web", Actor: "u", Now: "n1"})

	rows, _, err := ListBeads(context.Background(), db, ListFilter{Namespace: "web", Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != b {
		t.Fatalf("namespace filter = %v, want just %q", rows, b)
	}

	rows, _, err = ListBeads(context.Background(), db, ListFilter{Label: "y", Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != b {
		t.Fatalf("label filter = %v, want just %q", rows, b)
	}

	rows, _, err = ListBeads(context.Background(), db, ListFilter{Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "a" {
		t.Fatalf("priority filter = %v, want just title=a", rows)
	}
}
