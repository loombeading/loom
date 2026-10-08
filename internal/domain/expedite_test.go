// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStoredPriorityZeroIsAValidationError(t *testing.T) {
	db := testDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = CreateBead(context.Background(), tx, CreateInput{Title: "t", Priority: 0, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n"})
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Msg, "priority must be between 1 and 4") {
		t.Fatalf("CreateBead priority 0: err = %v, want *ValidationError naming 1 and 4", err)
	}
}

func TestExpediteLimitsAreValidationErrors(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	create := func(span time.Duration) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = CreateBead(ctx, tx, CreateInput{Title: "t", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n", ExpediteFor: &span, Reason: "r"})
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	var ve *ValidationError
	if err := create(25 * time.Hour); !errors.As(err, &ve) || !strings.Contains(ve.Msg, "expedite_max_age") {
		t.Errorf("25h expedite: err = %v, want *ValidationError naming expedite_max_age", err)
	}
	for i := range 2 {
		if err := create(time.Hour); err != nil {
			t.Fatalf("expedite %d: %v", i, err)
		}
	}
	if err := create(time.Hour); !errors.As(err, &ve) || !strings.Contains(ve.Msg, "expedite_max_open") {
		t.Errorf("third expedite: err = %v, want *ValidationError naming expedite_max_open", err)
	}
}

func TestCoefficientKeysMatchSchemaTable(t *testing.T) {
	db := testDB(t)
	rows, err := db.QueryContext(context.Background(), `SELECT key FROM coefficients`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	inTable := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		inTable[k] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	known := CoefficientKeys()
	if len(known) != 17 || len(inTable) != 17 {
		t.Fatalf("known keys = %d, table keys = %d, want 17 each", len(known), len(inTable))
	}
	for _, k := range known {
		if !inTable[k] {
			t.Errorf("policy key %q known to the code is missing from the table", k)
		}
	}
	if _, err := LoadCoefficients(context.Background(), db); err != nil {
		t.Errorf("LoadCoefficients on the initial table: %v", err)
	}
}

func TestLoadCoefficientsRejectsMissingAndOutOfRange(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE coefficients SET value = '0h' WHERE key = 'due_lead'`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCoefficients(ctx, db); err == nil || !strings.Contains(err.Error(), "due_lead") {
		t.Errorf("non-positive duration: err = %v, want an error naming due_lead", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE coefficients SET value = '72h' WHERE key = 'due_lead'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE coefficients SET value = '5' WHERE key = 'severity_3_floor'`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCoefficients(ctx, db); err == nil || !strings.Contains(err.Error(), "severity_3_floor") {
		t.Errorf("floor out of range: err = %v, want an error naming severity_3_floor", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE coefficients SET value = '4' WHERE key = 'severity_3_floor'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM coefficients WHERE key = 'cancel_after'`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCoefficients(ctx, db); err == nil || !strings.Contains(err.Error(), "cancel_after") {
		t.Errorf("missing key: err = %v, want an error naming cancel_after", err)
	}
}
