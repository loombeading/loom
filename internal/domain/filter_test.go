// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"testing"
)

func TestParseFilterTime(t *testing.T) {
	got, err := ParseFilterTime("2026-09-01")
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-09-01T00:00:00.000Z"; got != want {
		t.Fatalf("ParseFilterTime(date-only) = %q, want %q", got, want)
	}

	got, err = ParseFilterTime("2026-09-01T12:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-09-01T12:30:00.000Z"; got != want {
		t.Fatalf("ParseFilterTime(RFC3339) = %q, want %q", got, want)
	}

	if _, err := ParseFilterTime("not a time"); err == nil {
		t.Fatal("ParseFilterTime(garbage): want error, got nil")
	}
	if _, err := ParseFilterTime("7d"); err == nil {
		t.Fatal("ParseFilterTime(relative spec): want error (no relative support), got nil")
	}
}

func TestResolveParentFilter(t *testing.T) {
	db := testDB(t)
	parent := mustCreate(t, db, CreateInput{Title: "parent", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})

	id, display, err := ResolveParentFilter(context.Background(), db, parent, ShortIDs{})
	if err != nil {
		t.Fatal(err)
	}
	if id != parent {
		t.Fatalf("ResolveParentFilter id = %q, want %q", id, parent)
	}
	if display == "" {
		t.Fatal("ResolveParentFilter display = \"\", want a display ID")
	}

	if _, _, err := ResolveParentFilter(context.Background(), db, "no-such-id", ShortIDs{}); err == nil {
		t.Fatal("ResolveParentFilter(unknown): want error, got nil")
	}
}

func TestListBeadsParentFilter(t *testing.T) {
	db := testDB(t)
	parent := mustCreate(t, db, CreateInput{Title: "parent", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})
	child := mustCreate(t, db, CreateInput{Title: "child", Priority: 2, BeadType: "task", Namespace: "lm", Parent: parent, Actor: "u", Now: "n1"})
	grandchild := mustCreate(t, db, CreateInput{Title: "grandchild", Priority: 2, BeadType: "task", Namespace: "lm", Parent: child, Actor: "u", Now: "n2"})
	_ = mustCreate(t, db, CreateInput{Title: "unrelated", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n3"})

	rows, _, err := ListBeads(context.Background(), db, ListFilter{ParentID: parent, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != child {
		t.Fatalf("--parent filter = %v, want just the direct child %q (not the grandchild %q)", rows, child, grandchild)
	}
}

func TestListBeadsUpdatedFilter(t *testing.T) {
	db := testDB(t)
	early := mustCreate(t, db, CreateInput{Title: "early", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	late := mustCreate(t, db, CreateInput{Title: "late", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-06-01T00:00:00.000Z"})

	mid, err := ParseFilterTime("2026-03-01")
	if err != nil {
		t.Fatal(err)
	}

	before, _, err := ListBeads(context.Background(), db, ListFilter{UpdatedBefore: mid, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].ID != early {
		t.Fatalf("--updated-before filter = %v, want just %q", before, early)
	}

	after, _, err := ListBeads(context.Background(), db, ListFilter{UpdatedAfter: mid, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].ID != late {
		t.Fatalf("--updated-after filter = %v, want just %q", after, late)
	}
}

func TestReadyBlockedParentAndUpdatedFilters(t *testing.T) {
	db := testDB(t)
	parent := mustCreate(t, db, CreateInput{Title: "parent", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})
	readyChild := mustCreate(t, db, CreateInput{Title: "ready child", Priority: 2, BeadType: "task", Namespace: "lm", Parent: parent, Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	blocker := mustCreate(t, db, CreateInput{Title: "blocker", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-06-01T00:00:00.000Z"})
	blockedChild := mustCreate(t, db, CreateInput{Title: "blocked child", Priority: 2, BeadType: "task", Namespace: "lm", Parent: parent, BlockedBy: []string{blocker}, Actor: "u", Now: "2026-06-01T00:00:00.000Z"})
	_ = mustCreate(t, db, CreateInput{Title: "unrelated ready", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-06-01T00:00:00.000Z"})

	readyRows, _, err := ReadyBeads(context.Background(), db, ReadyFilter{ParentID: parent, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(readyRows) != 1 || readyRows[0].ID != readyChild {
		t.Fatalf("ready --parent filter = %v, want just %q", readyRows, readyChild)
	}

	blockedRows, _, _, err := BlockedBeadsEffective(context.Background(), db, ReadyFilter{ParentID: parent, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(blockedRows) != 1 || blockedRows[0].ID != blockedChild {
		t.Fatalf("blocked --parent filter = %v, want just %q", blockedRows, blockedChild)
	}

	mid, err := ParseFilterTime("2026-03-01")
	if err != nil {
		t.Fatal(err)
	}
	beforeRows, _, err := ReadyBeads(context.Background(), db, ReadyFilter{UpdatedBefore: mid, Priority: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeRows) != 1 || beforeRows[0].ID != readyChild {
		t.Fatalf("ready --updated-before filter = %v, want just %q", beforeRows, readyChild)
	}
}
