// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func createMilestoneTree(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	milestone := mustCreate(t, db, CreateInput{Title: "m", Priority: 2, BeadType: "task", Labels: []string{"milestone:x"}, Namespace: "lm", Actor: "u", Now: "n0"})
	child := mustCreate(t, db, CreateInput{Title: "c", Priority: 2, BeadType: "task", Namespace: "lm", Parent: milestone, Actor: "u", Now: "n1"})
	return milestone, child
}

func priorityOf(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	return b.Priority
}

func wantMilestoneOrderErr(t *testing.T, err error, msDisplay string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if !strings.Contains(ve.Msg, msDisplay+" の P2") {
		t.Fatalf("msg = %q, want milestone %s and its priority P2", ve.Msg, msDisplay)
	}
}

func displayOf(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	d, err := DisplayIDFor(context.Background(), tx, id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMilestoneOrderUpdatePriority(t *testing.T) {
	db := testDB(t)
	milestone, child := createMilestoneTree(t, db)
	grandchild := mustCreate(t, db, CreateInput{Title: "g", Priority: 3, BeadType: "task", Namespace: "lm", Parent: child, Actor: "u", Now: "n2"})

	err := mustUpdate(t, db, UpdateInput{ID: grandchild, Priority: 1, Actor: "u", Now: "n3"})
	wantMilestoneOrderErr(t, err, displayOf(t, db, milestone))
	if got := priorityOf(t, db, grandchild); got != 3 {
		t.Fatalf("priority after rejected update = %d, want 3", got)
	}

	mustNoErr(t, mustUpdate(t, db, UpdateInput{ID: grandchild, Priority: 2, Actor: "u", Now: "n4"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{ID: grandchild, Priority: 4, Actor: "u", Now: "n5"}))

	err = mustUpdate(t, db, UpdateInput{ID: milestone, Priority: 3, Actor: "u", Now: "n6"})
	if err == nil {
		t.Fatal("lowering the milestone below its child = nil, want rejection")
	}
	mustNoErr(t, mustUpdate(t, db, UpdateInput{ID: milestone, Priority: 1, Actor: "u", Now: "n7"}))
}

func TestMilestoneOrderNearestAncestor(t *testing.T) {
	db := testDB(t)
	outer := mustCreate(t, db, CreateInput{Title: "o", Priority: 1, BeadType: "task", Labels: []string{"milestone:o"}, Namespace: "lm", Actor: "u", Now: "n0"})
	inner := mustCreate(t, db, CreateInput{Title: "i", Priority: 2, BeadType: "task", Labels: []string{"milestone:i"}, Namespace: "lm", Parent: outer, Actor: "u", Now: "n1"})
	child := mustCreate(t, db, CreateInput{Title: "c", Priority: 2, BeadType: "task", Namespace: "lm", Parent: inner, Actor: "u", Now: "n2"})

	err := mustUpdate(t, db, UpdateInput{ID: child, Priority: 1, Actor: "u", Now: "n3"})
	wantMilestoneOrderErr(t, err, displayOf(t, db, inner))
}

func TestMilestoneOrderExemptions(t *testing.T) {
	db := testDB(t)
	free := newBead(t, db, "n0")
	mustNoErr(t, mustUpdate(t, db, UpdateInput{ID: free, Priority: 1, Actor: "u", Now: "n1"}))

	milestone, child := createMilestoneTree(t, db)
	mustNoErr(t, mustUpdate(t, db, UpdateInput{ID: child, Priority: -1, Cancel: true, Reason: "r", Actor: "u", Now: "n2"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{ID: milestone, Priority: 3, Actor: "u", Now: "n3"}))
}

func TestMilestoneOrderCreateParent(t *testing.T) {
	db := testDB(t)
	milestone, child := createMilestoneTree(t, db)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateBead(context.Background(), tx, CreateInput{Title: "x", Priority: 1, BeadType: "task", Namespace: "lm", Parent: child, Actor: "u", Now: "n2"})
	_ = tx.Rollback()
	wantMilestoneOrderErr(t, err, displayOf(t, db, milestone))
}

func parentOf(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var parent string
	err := db.QueryRowContext(context.Background(), `SELECT depends_on_id FROM dependencies WHERE bead_id = ? AND type = ? AND removed = 0`, id, ParentChildDepType).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return parent
}

func TestCreateDiscoveredFromAttachesMilestoneParent(t *testing.T) {
	db := testDB(t)
	milestone, child := createMilestoneTree(t, db)
	grandchild := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "task", Namespace: "lm", Parent: child, Actor: "u", Now: "n2"})

	for _, tc := range []struct {
		name, source, wantParent string
	}{
		{"source is the milestone", milestone, milestone},
		{"source is a milestone child", child, milestone},
		{"source is deeper", grandchild, child},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := mustCreate(t, db, CreateInput{Title: "d", Priority: 3, BeadType: "task", Namespace: "lm", DiscoveredFrom: tc.source, RequireMilestone: true, Actor: "u", Now: "n3"})
			if got := parentOf(t, db, id); got != tc.wantParent {
				t.Fatalf("parent = %q, want %q", got, tc.wantParent)
			}
			if b, err := GetBead(context.Background(), db, id); err != nil || len(b.Labels) != 0 {
				t.Fatalf("labels = %v, %v; want none", b.Labels, err)
			}
		})
	}

	explicit := mustCreate(t, db, CreateInput{Title: "e", Priority: 3, BeadType: "task", Namespace: "lm", Parent: grandchild, DiscoveredFrom: child, Actor: "u", Now: "n4"})
	if got := parentOf(t, db, explicit); got != grandchild {
		t.Fatalf("parent with --parent = %q, want %q", got, grandchild)
	}

	free := newBead(t, db, "n5")
	derived := mustCreate(t, db, CreateInput{Title: "f", Priority: 2, BeadType: "task", Namespace: "lm", DiscoveredFrom: free, Actor: "u", Now: "n6"})
	if got := parentOf(t, db, derived); got != "" {
		t.Fatalf("parent of a Bead derived from an unaffiliated source = %q, want none", got)
	}

	var before int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM beads`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateBead(context.Background(), tx, CreateInput{Title: "x", Priority: 1, BeadType: "task", Namespace: "lm", DiscoveredFrom: child, Actor: "u", Now: "n7"})
	_ = tx.Rollback()
	wantMilestoneOrderErr(t, err, displayOf(t, db, milestone))
	var after int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM beads`).Scan(&after); err != nil || after != before {
		t.Fatalf("beads after rejected create = %d, %v; want %d", after, err, before)
	}
}

func TestDiscoveredFromParentQueryError(t *testing.T) {
	db := testDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if _, err := discoveredFromParent(context.Background(), tx, "x"); err == nil {
		t.Fatal("discoveredFromParent on a closed tx = nil, want an error")
	}
}

func TestMilestoneOrderUpsertParentChild(t *testing.T) {
	db := testDB(t)
	milestone, _ := createMilestoneTree(t, db)
	urgent := mustCreate(t, db, CreateInput{Title: "u", Priority: 1, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n2"})
	sub := mustCreate(t, db, CreateInput{Title: "s", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n3"})
	mustNoErr(t, mustAddLink(t, db, urgent, sub, ParentChildDepType))
	sub2 := mustCreate(t, db, CreateInput{Title: "s2", Priority: 3, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n4"})

	upsert := func(beadID, dependsOnID, depType string) error {
		t.Helper()
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := UpsertLink(context.Background(), tx, "u", beadID, dependsOnID, depType, "n5", ""); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}

	wantMilestoneOrderErr(t, upsert(sub, milestone, ParentChildDepType), displayOf(t, db, milestone))
	if n, err := CountActiveLinks(context.Background(), db, milestone, ParentChildDepType); err != nil || n != 1 {
		t.Fatalf("active parent-child links to the milestone = %d, %v; want 1 (rejected link not written)", n, err)
	}
	mustNoErr(t, upsert(sub2, milestone, ParentChildDepType))
	mustNoErr(t, upsert(urgent, milestone, BlocksDepType))
}
