// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func mustAddLink(t *testing.T, db *sql.DB, beadID, dependsOnID, depType string) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = AddLink(context.Background(), tx, "u", beadID, dependsOnID, depType, "n", "")
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func newBead(t *testing.T, db *sql.DB, now string) string {
	t.Helper()
	return mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
}

func mustDisplayID(t *testing.T, db *sql.DB, canonical string) string {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	display, err := DisplayIDFor(context.Background(), tx, canonical)
	if err != nil {
		t.Fatal(err)
	}
	return display
}

func TestAddLinkRejectsSelfLink(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")

	if err := mustAddLink(t, db, a, a, BlocksDepType); err == nil {
		t.Fatal("self blocks link = nil error, want an error")
	} else if !errors.As(err, new(*SelfLinkError)) {
		t.Fatalf("self blocks link err = %T, want *SelfLinkError", err)
	}
	if err := mustAddLink(t, db, a, a, ParentChildDepType); err == nil {
		t.Fatal("self parent-child link = nil error, want an error")
	}
}

func TestSelfLinkErrorReportsDisplayID(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")

	err := mustAddLink(t, db, a, a, BlocksDepType)
	var serr *SelfLinkError
	if !errors.As(err, &serr) {
		t.Fatalf("err = %T, want *SelfLinkError", err)
	}
	want := mustDisplayID(t, db, a)
	if serr.ID != want {
		t.Errorf("SelfLinkError.ID = %q, want display ID %q", serr.ID, want)
	}
}

func TestAddLinkRejectsDirectCycle(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")

	if err := mustAddLink(t, db, a, b, BlocksDepType); err != nil {
		t.Fatalf("a -> b: %v", err)
	}
	err := mustAddLink(t, db, b, a, BlocksDepType)
	if err == nil {
		t.Fatal("b -> a (direct cycle) = nil error, want a CycleError")
	}
	var cerr *CycleError
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %T, want *CycleError", err)
	}
	if cerr.DepType != BlocksDepType {
		t.Errorf("cycle DepType = %q, want %q", cerr.DepType, BlocksDepType)
	}

	wantStart, wantEnd := mustDisplayID(t, db, a), mustDisplayID(t, db, b)
	if len(cerr.Cycle) < 2 || cerr.Cycle[0] != wantStart || cerr.Cycle[len(cerr.Cycle)-1] != wantEnd {
		t.Errorf("cycle path = %v, want to start at %q and end at %q", cerr.Cycle, wantStart, wantEnd)
	}
	for _, id := range cerr.Cycle {
		if id == a || id == b {
			t.Errorf("cycle path = %v, contains a canonical ID rather than a resolved display ID", cerr.Cycle)
		}
	}
}

func TestAddLinkRejectsTwoStepCycle(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")
	c := newBead(t, db, "n2")

	mustNoErr(t, mustAddLink(t, db, a, b, BlocksDepType))
	mustNoErr(t, mustAddLink(t, db, b, c, BlocksDepType))
	if err := mustAddLink(t, db, c, a, BlocksDepType); err == nil {
		t.Fatal("c -> a (2-step cycle) = nil error, want an error")
	} else if !errors.As(err, new(*CycleError)) {
		t.Fatalf("err = %T, want *CycleError", err)
	}
}

func TestAddLinkRejectsThreeStepCycle(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")
	c := newBead(t, db, "n2")
	d := newBead(t, db, "n3")

	mustNoErr(t, mustAddLink(t, db, a, b, BlocksDepType))
	mustNoErr(t, mustAddLink(t, db, b, c, BlocksDepType))
	mustNoErr(t, mustAddLink(t, db, c, d, BlocksDepType))
	if err := mustAddLink(t, db, d, a, BlocksDepType); err == nil {
		t.Fatal("d -> a (3-step cycle) = nil error, want an error")
	}
}

func TestAddLinkCycleDetectionIsPerDepType(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")

	mustNoErr(t, mustAddLink(t, db, a, b, BlocksDepType))

	if err := mustAddLink(t, db, b, a, ParentChildDepType); err != nil {
		t.Fatalf("b -> a parent-child (independent of the blocks cycle): %v", err)
	}
}

func TestAddLinkRejectsSecondActiveParent(t *testing.T) {
	db := testDB(t)
	child := newBead(t, db, "n0")
	parent1 := newBead(t, db, "n1")
	parent2 := newBead(t, db, "n2")

	mustNoErr(t, mustAddLink(t, db, child, parent1, ParentChildDepType))
	err := mustAddLink(t, db, child, parent2, ParentChildDepType)
	if err == nil {
		t.Fatal("second active parent = nil error, want an error")
	}
	var derr *DuplicateParentError
	if !errors.As(err, &derr) {
		t.Fatalf("err = %T, want *DuplicateParentError", err)
	}
	if derr.CurrentParentID == "" {
		t.Error("DuplicateParentError.CurrentParentID is empty, want the current parent's display ID")
	}

	wantBeadID := mustDisplayID(t, db, child)
	if derr.BeadID != wantBeadID {
		t.Errorf("DuplicateParentError.BeadID = %q, want display ID %q", derr.BeadID, wantBeadID)
	}
}

func TestAddLinkAllowsReparentAfterRemoval(t *testing.T) {
	db := testDB(t)
	child := newBead(t, db, "n0")
	parent1 := newBead(t, db, "n1")
	parent2 := newBead(t, db, "n2")

	mustNoErr(t, mustAddLink(t, db, child, parent1, ParentChildDepType))

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(context.Background(), tx, "u", child, parent1, ParentChildDepType, "n3", ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := mustAddLink(t, db, child, parent2, ParentChildDepType); err != nil {
		t.Fatalf("re-parent after removing the old link: %v", err)
	}
}

func mustUpsertLink(t *testing.T, db *sql.DB, beadID, dependsOnID, depType, now string) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = UpsertLink(context.Background(), tx, "u", beadID, dependsOnID, depType, now, "")
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func TestUpsertLinkReactivatesRemovedLink(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")

	mustNoErr(t, mustAddLink(t, db, a, b, BlocksDepType))
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(context.Background(), tx, "u", a, b, BlocksDepType, "n2", ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	mustNoErr(t, mustUpsertLink(t, db, a, b, BlocksDepType, "n3"))

	links, err := Links(context.Background(), db, a, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].DependsOnID != b || links[0].Removed {
		t.Errorf("links after reactivation = %+v, want one active link to %q", links, b)
	}
}

func TestUpsertLinkReactivationDetectsCycle(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")

	mustNoErr(t, mustAddLink(t, db, a, b, BlocksDepType))
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(context.Background(), tx, "u", a, b, BlocksDepType, "n2", ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	mustNoErr(t, mustAddLink(t, db, b, a, BlocksDepType))

	err = mustUpsertLink(t, db, a, b, BlocksDepType, "n3")
	if err == nil {
		t.Fatal("reactivating a -> b after b -> a exists = nil error, want a CycleError")
	} else if !errors.As(err, new(*CycleError)) {
		t.Fatalf("err = %T, want *CycleError", err)
	}
}

func TestUpsertLinkAlreadyActiveIsNoOp(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")

	mustNoErr(t, mustAddLink(t, db, a, b, BlocksDepType))
	if err := mustUpsertLink(t, db, a, b, BlocksDepType, "n2"); err != nil {
		t.Fatalf("re-adding an already-active link: %v", err)
	}

	links, err := Links(context.Background(), db, a, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Errorf("links = %v, want exactly one row (no duplicate insert)", links)
	}
}

func TestRemoveLinkNotFound(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	err = RemoveLink(context.Background(), tx, "u", a, b, BlocksDepType, "n1", "")
	if err == nil {
		t.Fatal("removing a nonexistent link = nil error, want an error")
	}
	if !errors.As(err, new(*LinkNotFoundError)) {
		t.Fatalf("err = %T, want *LinkNotFoundError", err)
	}
}

func TestCreateBeadWithParentAndBlockedByEnforcesConstraints(t *testing.T) {
	db := testDB(t)
	parent1 := newBead(t, db, "n0")
	parent2 := newBead(t, db, "n1")

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = CreateBead(context.Background(), tx, CreateInput{
		Title: "child", Priority: 2, BeadType: "task", Namespace: "lm",
		Parent: parent1, Actor: "u", Now: "n2",
	})
	if err != nil {
		t.Fatalf("create with a single parent: %v", err)
	}

	_, err = CreateBead(context.Background(), tx, CreateInput{
		Title: "self-blocker", Priority: 2, BeadType: "task", Namespace: "lm",
		BlockedBy: []string{parent1, parent2}, Actor: "u", Now: "n3",
	})
	if err != nil {
		t.Fatalf("create with two independent blockers: %v", err)
	}
}

func TestCountActiveLinksCountsOnlyMatchingActiveType(t *testing.T) {
	db := testDB(t)
	src := newBead(t, db, "n0")
	a := newBead(t, db, "n1")
	b := newBead(t, db, "n2")
	c := newBead(t, db, "n3")
	other := newBead(t, db, "n4")

	if err := mustAddLink(t, db, a, src, DiscoveredFromDepType); err != nil {
		t.Fatal(err)
	}
	if err := mustAddLink(t, db, b, src, DiscoveredFromDepType); err != nil {
		t.Fatal(err)
	}
	if err := mustAddLink(t, db, c, src, DiscoveredFromDepType); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(context.Background(), tx, "u", c, src, DiscoveredFromDepType, "n5", ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := mustAddLink(t, db, other, src, BlocksDepType); err != nil {
		t.Fatal(err)
	}

	n, err := CountActiveLinks(context.Background(), db, src, DiscoveredFromDepType)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("CountActiveLinks = %d, want 2", n)
	}
}
