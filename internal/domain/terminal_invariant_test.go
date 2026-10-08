// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
)

func wantInvariant(t *testing.T, err error, reason string) {
	t.Helper()
	var inv *TerminalInvariantError
	if !errors.As(err, &inv) || inv.Reason != reason {
		t.Fatalf("err = %v, want TerminalInvariantError %q", err, reason)
	}
}

func aliasOf(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	r, err := NewAliasResolverFor(context.Background(), db, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	alias, hasParent, err := r.Alias(id)
	if err != nil || !hasParent {
		t.Fatalf("Alias(%q) = %q, %v, %v; want a parent alias", id, alias, hasParent, err)
	}
	return alias
}

func wantStatus(t *testing.T, db *sql.DB, id, status string) {
	t.Helper()
	b, err := GetBead(context.Background(), db, id)
	if err != nil || b.Status != status {
		t.Fatalf("GetBead(%q) = %q, %v; want %q", id, b.Status, err, status)
	}
}

func TestCloseRefusesUnfinishedChildren(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	low := mustCreate(t, db, CreateInput{Title: "low", Priority: 3, BeadType: "task", Namespace: "lm", Parent: parent, Actor: "u", Now: "n1"})
	high := mustCreate(t, db, CreateInput{Title: "high", Priority: 1, BeadType: "task", Namespace: "lm", Parent: parent, Actor: "u", Now: "n2"})
	done := mustCreate(t, db, CreateInput{Title: "done", Priority: 2, BeadType: "task", Namespace: "lm", Parent: parent, Actor: "u", Now: "n3"})
	mustNoErr(t, mustCloseBead(t, db, done, "n4"))

	wantInvariant(t, mustCloseBead(t, db, parent, "n5"), "unfinished children: "+aliasOf(t, db, high)+", "+aliasOf(t, db, low))
	wantStatus(t, db, parent, StatusOpen)

	mustNoErr(t, mustCloseBead(t, db, high, "n6"))
	mustNoErr(t, mustRemoveLink(t, db, low, parent, ParentChildDepType))
	mustNoErr(t, mustCloseBead(t, db, parent, "n7"))
	wantStatus(t, db, parent, StatusClosed)
}

func TestCancelRefusesUnfinishedDependents(t *testing.T) {
	db := testDB(t)
	prereq := newBead(t, db, "n0")
	downstream := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, downstream, prereq, BlocksDepType))

	err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: prereq, Cancel: true, Actor: "u", Now: "n2"})
	wantInvariant(t, err, "unfinished dependents: "+displayIDOf(t, db, downstream))
	wantStatus(t, db, prereq, StatusOpen)

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: downstream, Cancel: true, Actor: "u", Now: "n3"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: prereq, Cancel: true, Actor: "u", Now: "n4"}))
	wantStatus(t, db, prereq, StatusCancelled)
}

func TestParentChildRefusesTerminalParent(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := newBead(t, db, "n1")
	mustNoErr(t, mustCloseBead(t, db, parent, "n2"))

	wantInvariant(t, mustAddLink(t, db, child, parent, ParentChildDepType), "terminal parent: "+displayIDOf(t, db, parent))
	if n, err := CountActiveLinks(context.Background(), db, parent, ParentChildDepType); err != nil || n != 0 {
		t.Fatalf("active parent-child links = %d, %v; want 0", n, err)
	}

	mustNoErr(t, mustCloseBead(t, db, child, "n3"))
	mustNoErr(t, mustAddLink(t, db, child, parent, ParentChildDepType))
}

func TestReopenRefusesTerminalParent(t *testing.T) {
	db := testDB(t)
	parent := newBead(t, db, "n0")
	child := mustCreate(t, db, CreateInput{Title: "c", Priority: 2, BeadType: "task", Namespace: "lm", Parent: parent, Actor: "u", Now: "n1"})
	mustNoErr(t, mustCloseBead(t, db, child, "n2"))
	mustNoErr(t, mustCloseBead(t, db, parent, "n3"))

	err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: child, Reopen: true, Actor: "u", Now: "n4"})
	wantInvariant(t, err, "terminal parent: "+displayIDOf(t, db, parent))
	wantStatus(t, db, child, StatusClosed)

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: parent, Reopen: true, Actor: "u", Now: "n5"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: child, Reopen: true, Actor: "u", Now: "n6"}))
	wantStatus(t, db, child, StatusOpen)
}

func TestReopenRefusesCancelledPrerequisites(t *testing.T) {
	db := testDB(t)
	prereq := newBead(t, db, "n0")
	done := newBead(t, db, "n0")
	downstream := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, downstream, prereq, BlocksDepType))
	mustNoErr(t, mustAddLink(t, db, downstream, done, BlocksDepType))
	mustNoErr(t, mustCloseBead(t, db, done, "n2"))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: downstream, Cancel: true, Actor: "u", Now: "n2"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: prereq, Cancel: true, Actor: "u", Now: "n3"}))

	err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: downstream, Reopen: true, Actor: "u", Now: "n4"})
	wantInvariant(t, err, "cancelled prerequisites: "+displayIDOf(t, db, prereq))
	wantStatus(t, db, downstream, StatusCancelled)

	mustNoErr(t, mustRemoveLink(t, db, downstream, prereq, BlocksDepType))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: downstream, Reopen: true, Actor: "u", Now: "n5"}))
	wantStatus(t, db, downstream, StatusOpen)
}

func TestGateRejectCancelsDependentsBlockedByTheSameGate(t *testing.T) {
	db := testDB(t)
	target := newBead(t, db, "n0")
	downstream := newBead(t, db, "n0")
	mustNoErr(t, mustAddLink(t, db, downstream, target, BlocksDepType))
	gateID := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target, downstream}, Fields: GateFields{Subject: "x"}, Namespace: "lm", Actor: "u", Now: "n1"})

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GateReject(context.Background(), tx, GateRejectInput{ID: gateID, Reason: "won't do", Actor: "u", Now: "n2"}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	mustNoErr(t, tx.Commit())
	wantStatus(t, db, target, StatusCancelled)
	wantStatus(t, db, downstream, StatusCancelled)
}

func TestTerminalInvariantQueryError(t *testing.T) {
	db := testDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if err := checkNoUnfinishedChildren(context.Background(), tx, "x"); err == nil {
		t.Fatal("checkNoUnfinishedChildren on a closed tx = nil, want an error")
	}
	if err := checkParentChildTarget(context.Background(), tx, "x", "y"); err == nil {
		t.Fatal("checkParentChildTarget on a closed tx = nil, want an error")
	}
	if err := checkReopenTarget(context.Background(), tx, "x"); err == nil {
		t.Fatal("checkReopenTarget on a closed tx = nil, want an error")
	}
}

func TestCreateDiscoveredFrom(t *testing.T) {
	db := testDB(t)
	milestone := mustCreate(t, db, CreateInput{Title: "m", Priority: 1, BeadType: "task", Labels: []string{"milestone:x"}, Namespace: "lm", Actor: "u", Now: "n0"})
	affiliated := mustCreate(t, db, CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Parent: milestone, Actor: "u", Now: "n1"})
	unaffiliated := newBead(t, db, "n2")

	id := mustCreate(t, db, CreateInput{Title: "d", Priority: 2, BeadType: "task", Namespace: "lm", DiscoveredFrom: affiliated, RequireMilestone: true, Actor: "u", Now: "n3"})
	n, err := CountActiveLinks(context.Background(), db, affiliated, DiscoveredFromDepType)
	if err != nil || n != 1 {
		t.Fatalf("discovered-from links to the source = %d, %v; want 1", n, err)
	}
	b, err := GetBead(context.Background(), db, id)
	if err != nil || len(b.Labels) != 0 {
		t.Fatalf("labels of the derived Bead = %v, %v; want none", b.Labels, err)
	}

	exempt := mustCreate(t, db, CreateInput{Title: "e", Priority: 2, BeadType: "task", Namespace: "lm", NoMilestone: "r", Actor: "u", Now: "n1"})
	exemptChild := mustCreate(t, db, CreateInput{Title: "ec", Priority: 2, BeadType: "task", Namespace: "lm", Parent: exempt, Actor: "u", Now: "n1"})
	for _, tc := range []struct {
		name, source string
		require      bool
	}{
		{"exempt source", exempt, true},
		{"exempt ancestor", exemptChild, true},
		{"unaffiliated source without require", unaffiliated, false},
	} {
		t.Run("inherit "+tc.name, func(t *testing.T) {
			id := mustCreate(t, db, CreateInput{Title: "d", Priority: 2, BeadType: "task", Namespace: "lm", DiscoveredFrom: tc.source, RequireMilestone: tc.require, Actor: "u", Now: "n3"})
			b, err := GetBead(context.Background(), db, id)
			if err != nil || !slices.Equal(b.Labels, []string{ExemptMilestoneLabel}) {
				t.Fatalf("labels of the derived Bead = %v, %v; want [%s]", b.Labels, err, ExemptMilestoneLabel)
			}
			var n int
			want := "discovered-from " + tc.source + ": 派生元がマイルストーンに属さない"
			if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_log WHERE bead_id = ? AND kind = 'note' AND reason = ?`, id, want).Scan(&n); err != nil || n != 1 {
				t.Fatalf("inherit notes = %d, %v; want 1 with %q", n, err, want)
			}
		})
	}

	withParent := mustCreate(t, db, CreateInput{Title: "p", Priority: 2, BeadType: "task", Namespace: "lm", Parent: exempt, DiscoveredFrom: unaffiliated, Actor: "u", Now: "n3"})
	if b, err := GetBead(context.Background(), db, withParent); err != nil || len(b.Labels) != 0 {
		t.Fatalf("labels of a Bead created with --parent = %v, %v; want none", b.Labels, err)
	}

	for _, tc := range []struct {
		name, source, want string
	}{
		{"unaffiliated source", unaffiliated, errDiscoveredFromUnaffiliatedMsg},
		{"missing source", "lm-zzzzzz", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			_, err = CreateBead(context.Background(), tx, CreateInput{Title: "d", Priority: 2, BeadType: "task", Namespace: "lm", DiscoveredFrom: tc.source, RequireMilestone: true, Actor: "u", Now: "n4"})
			if err == nil || (tc.want != "" && err.Error() != tc.want) {
				t.Fatalf("CreateBead err = %v, want %q", err, tc.want)
			}
		})
	}
}
