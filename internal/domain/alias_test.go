// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"testing"
)

func TestAliasNoParentIsPlainDisplayID(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")

	got, err := Alias(context.Background(), db, root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "lm-a3f" {
		t.Fatalf("got %q, want %q", got, "lm-a3f")
	}
}

func TestAliasResolverForNoIDsResolvesNothing(t *testing.T) {
	r, err := NewAliasResolverFor(t.Context(), testDB(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if alias, ok, err := r.Alias("a3f80000000000000000000000000000"); err != nil || ok || alias != "" {
		t.Fatalf("Alias = %q, %v, %v; want no alias", alias, ok, err)
	}
}

func TestAliasUsesStoredShortIDOfAncestor(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	child := "c1110000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child, "lm", "2026-01-01T00:01:00.000Z")
	insertParentChild(t, db, child, root, "2026-01-01T00:01:00.000Z", false)
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET short_id = 'a3f80' WHERE id = ?`, root); err != nil {
		t.Fatal(err)
	}

	alias, err := Alias(t.Context(), db, child)
	if err != nil {
		t.Fatal(err)
	}
	if alias != "lm-a3f80.1" {
		t.Fatalf("alias = %q, want %q", alias, "lm-a3f80.1")
	}
}

func TestAliasSkipsRemovedSiblingNumbers(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	removedChild := "b1110000000000000000000000000000"
	child := "c1110000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, removedChild, "lm", "2026-01-01T00:01:00.000Z")
	insertBead(t, db, child, "lm", "2026-01-01T00:02:00.000Z")

	insertParentChild(t, db, removedChild, root, "2026-01-01T00:01:00.000Z", true)
	insertParentChild(t, db, child, root, "2026-01-01T00:02:00.000Z", false)

	alias, err := Alias(context.Background(), db, child)
	if err != nil {
		t.Fatal(err)
	}
	if alias != "lm-a3f.2" {
		t.Fatalf("alias = %q, want %q (欠番 for removed link #1)", alias, "lm-a3f.2")
	}
}

func TestAliasSameTimestampTiebreaksOnChildID(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	childHigh := "c1110000000000000000000000000000"
	childLow := "b1110000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, childHigh, "lm", "2026-01-01T00:01:00.000Z")
	insertBead(t, db, childLow, "lm", "2026-01-01T00:01:00.000Z")

	insertParentChild(t, db, childHigh, root, "2026-01-01T00:01:00.000Z", false)
	insertParentChild(t, db, childLow, root, "2026-01-01T00:01:00.000Z", false)

	aliasLow, err := Alias(context.Background(), db, childLow)
	if err != nil {
		t.Fatal(err)
	}
	if aliasLow != "lm-a3f.1" {
		t.Fatalf("aliasLow = %q, want %q", aliasLow, "lm-a3f.1")
	}

	aliasHigh, err := Alias(context.Background(), db, childHigh)
	if err != nil {
		t.Fatal(err)
	}
	if aliasHigh != "lm-a3f.2" {
		t.Fatalf("aliasHigh = %q, want %q", aliasHigh, "lm-a3f.2")
	}
}

func TestAliasMultipleActiveParentsPicksOldestLink(t *testing.T) {
	db := testDB(t)
	p1 := "a3f80000000000000000000000000000"
	p2 := "b3f80000000000000000000000000000"
	child := "c1110000000000000000000000000000"
	insertBead(t, db, p1, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, p2, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child, "lm", "2026-01-01T00:03:00.000Z")
	insertParentChild(t, db, child, p2, "2026-01-01T00:01:00.000Z", false)
	insertParentChild(t, db, child, p1, "2026-01-01T00:02:00.000Z", false)

	alias, err := Alias(context.Background(), db, child)
	if err != nil {
		t.Fatal(err)
	}
	if alias != "lm-b3f.1" {
		t.Fatalf("alias = %q, want %q", alias, "lm-b3f.1")
	}
}

func TestAliasMultipleActiveParentsSameTimeTiebreaksOnParentID(t *testing.T) {
	db := testDB(t)
	p1 := "a3f80000000000000000000000000000"
	p2 := "b3f80000000000000000000000000000"
	child := "c1110000000000000000000000000000"
	insertBead(t, db, p1, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, p2, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child, "lm", "2026-01-01T00:03:00.000Z")
	insertParentChild(t, db, child, p2, "2026-01-01T00:01:00.000Z", false)
	insertParentChild(t, db, child, p1, "2026-01-01T00:01:00.000Z", false)

	alias, err := Alias(context.Background(), db, child)
	if err != nil {
		t.Fatal(err)
	}
	if alias != "lm-a3f.1" {
		t.Fatalf("alias = %q, want %q", alias, "lm-a3f.1")
	}
}

func TestAliasDoesNotTraverseRemovedParentLink(t *testing.T) {
	db := testDB(t)
	parent := "a3f80000000000000000000000000000"
	child := "c1110000000000000000000000000000"
	insertBead(t, db, parent, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child, "lm", "2026-01-01T00:01:00.000Z")
	insertParentChild(t, db, child, parent, "2026-01-01T00:01:00.000Z", true)

	alias, err := Alias(context.Background(), db, child)
	if err != nil {
		t.Fatal(err)
	}
	if alias != "lm-c11" {
		t.Fatalf("alias = %q, want %q", alias, "lm-c11")
	}

	r, err := NewAliasResolverFor(t.Context(), db, []string{child})
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := r.Parent(child); ok {
		t.Fatalf("Parent(child) = %q, want no parent", p)
	}
}

func TestAliasTwoLevelHierarchy(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	child := "b1110000000000000000000000000000"
	grandchild := "d1110000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child, "lm", "2026-01-01T00:01:00.000Z")
	insertBead(t, db, grandchild, "lm", "2026-01-01T00:02:00.000Z")
	insertParentChild(t, db, child, root, "2026-01-01T00:01:00.000Z", false)
	insertParentChild(t, db, grandchild, child, "2026-01-01T00:02:00.000Z", false)

	alias, err := Alias(context.Background(), db, grandchild)
	if err != nil {
		t.Fatal(err)
	}
	if alias != "lm-a3f.1.1" {
		t.Fatalf("alias = %q, want %q", alias, "lm-a3f.1.1")
	}

	got, err := ResolveID(context.Background(), db, alias)
	if err != nil {
		t.Fatal(err)
	}
	if got != grandchild {
		t.Fatalf("resolved %q, want %q", got, grandchild)
	}
}
