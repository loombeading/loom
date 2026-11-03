// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"strings"
	"testing"
)

func TestHoldCancelRunRejectsUnparsableNow(t *testing.T) {
	db := testDB(t)
	execSQL(t, db, `UPDATE coefficients SET value = 'on' WHERE key = 'time_adjust'`)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := HoldCancelRun(t.Context(), tx, HoldCancelInput{At: "not-a-time"}); err == nil || !strings.Contains(err.Error(), "hold cancel: now") {
		t.Errorf("err = %v, want a hold cancel: now error", err)
	}
}

func TestHoldCancelRunPropagatesCoefficientLoadError(t *testing.T) {
	db := testDB(t)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := HoldCancelRun(t.Context(), tx, HoldCancelInput{}); err == nil {
		t.Error("HoldCancelRun on a finished transaction must fail")
	}
}
