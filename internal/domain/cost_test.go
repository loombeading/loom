// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"testing"
)

func mustAddTokenCost(t *testing.T, db *sql.DB, in AddTokenCostInput) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = AddTokenCost(context.Background(), tx, in)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func createForCost(t *testing.T, db *sql.DB) string {
	t.Helper()
	return mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
}

func TestAddTokenCostRecordsRowAndAudit(t *testing.T) {
	db := testDB(t)
	id := createForCost(t, db)

	if err := mustAddTokenCost(t, db, AddTokenCostInput{BeadID: id, TokensIn: 100, TokensOut: 50, Actor: "alice", Now: "n1"}); err != nil {
		t.Fatalf("add token cost: %v", err)
	}

	totals, err := GetTokenCostTotals(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if totals.TokensIn != 100 || totals.TokensOut != 50 || totals.Records != 1 {
		t.Errorf("totals = %+v, want in=100 out=50 records=1", totals)
	}

	history, err := AuditHistory(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range history {
		if h.Kind == AuditKindField && h.Field.String == "token_cost" && h.NewValue.String == "100/50" {
			found = true
		}
	}
	if !found {
		t.Errorf("history = %+v, want a kind=field field=token_cost new_value=100/50 record", history)
	}
}

func TestAddTokenCostAggregatesMultipleRecords(t *testing.T) {
	db := testDB(t)
	id := createForCost(t, db)

	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: id, TokensIn: 10, TokensOut: 5, Actor: "alice", Now: "n1"}))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: id, TokensIn: 20, TokensOut: 0, Actor: "bob", Now: "n2"}))

	totals, err := GetTokenCostTotals(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if totals.TokensIn != 30 || totals.TokensOut != 5 || totals.Records != 2 {
		t.Errorf("totals = %+v, want in=30 out=5 records=2", totals)
	}
	if totals.Total() != 35 {
		t.Errorf("Total() = %d, want 35", totals.Total())
	}
}

func TestGetTokenCostTotalsZeroWhenNoRecords(t *testing.T) {
	db := testDB(t)
	id := createForCost(t, db)

	totals, err := GetTokenCostTotals(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if totals != (TokenCostTotals{}) {
		t.Errorf("totals = %+v, want zero value", totals)
	}
}

func TestAddTokenCostRejectsNegative(t *testing.T) {
	db := testDB(t)
	id := createForCost(t, db)

	if err := mustAddTokenCost(t, db, AddTokenCostInput{BeadID: id, TokensIn: -1, TokensOut: 5, Actor: "alice", Now: "n1"}); err == nil {
		t.Fatal("negative tokens-in = nil error, want an error")
	}
	if err := mustAddTokenCost(t, db, AddTokenCostInput{BeadID: id, TokensIn: 5, TokensOut: -1, Actor: "alice", Now: "n1"}); err == nil {
		t.Fatal("negative tokens-out = nil error, want an error")
	}
}

func TestAddTokenCostRejectsBothZero(t *testing.T) {
	db := testDB(t)
	id := createForCost(t, db)

	if err := mustAddTokenCost(t, db, AddTokenCostInput{BeadID: id, TokensIn: 0, TokensOut: 0, Actor: "alice", Now: "n1"}); err == nil {
		t.Fatal("both zero = nil error, want an error")
	}
}

func TestAddTokenCostRejectsUnknownBead(t *testing.T) {
	db := testDB(t)

	if err := mustAddTokenCost(t, db, AddTokenCostInput{BeadID: NewCanonicalID(), TokensIn: 1, TokensOut: 1, Actor: "alice", Now: "n1"}); err == nil {
		t.Fatal("unknown bead = nil error, want an error")
	}
}

func TestGetTokenCostTotalNoChildrenMatchesTokenCost(t *testing.T) {
	db := testDB(t)
	id := createForCost(t, db)
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: id, TokensIn: 10, TokensOut: 5, Actor: "alice", Now: "n1"}))

	total, err := GetTokenCostTotal(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if total != 15 {
		t.Errorf("GetTokenCostTotal = %d, want 15", total)
	}
}

func TestGetTokenCostTotalSumsDescendants(t *testing.T) {
	db := testDB(t)
	epic := createForCost(t, db)
	child := createForCost(t, db)
	grandchild := createForCost(t, db)
	mustNoErr(t, mustAddLink(t, db, child, epic, ParentChildDepType))
	mustNoErr(t, mustAddLink(t, db, grandchild, child, ParentChildDepType))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: epic, TokensIn: 1, TokensOut: 1, Actor: "alice", Now: "n1"}))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: child, TokensIn: 100, TokensOut: 50, Actor: "alice", Now: "n2"}))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: grandchild, TokensIn: 1000, TokensOut: 500, Actor: "alice", Now: "n3"}))

	total, err := GetTokenCostTotal(context.Background(), db, epic)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1652 {
		t.Errorf("GetTokenCostTotal(epic) = %d, want 1652 (2 + 150 + 1500)", total)
	}

	childTotal, err := GetTokenCostTotal(context.Background(), db, child)
	if err != nil {
		t.Fatal(err)
	}
	if childTotal != 1650 {
		t.Errorf("GetTokenCostTotal(child) = %d, want 1650 (150 + 1500)", childTotal)
	}
}

func TestGetTokenCostTotalExcludesRemovedLink(t *testing.T) {
	db := testDB(t)
	epic := createForCost(t, db)
	child := createForCost(t, db)
	mustNoErr(t, mustAddLink(t, db, child, epic, ParentChildDepType))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: child, TokensIn: 100, TokensOut: 50, Actor: "alice", Now: "n1"}))

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(context.Background(), tx, "alice", child, epic, ParentChildDepType, "n2", ""); err != nil {
		t.Fatal(err)
	}
	mustNoErr(t, tx.Commit())

	total, err := GetTokenCostTotal(context.Background(), db, epic)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Errorf("GetTokenCostTotal(epic) = %d, want 0 once the parent-child link is removed", total)
	}
}
