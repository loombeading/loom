// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import "testing"

func TestCollectRowsScanErrorClosesRows(t *testing.T) {
	db := testDB(t)
	rows, err := db.QueryContext(t.Context(), `SELECT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collectRows(rows, scanString); err == nil {
		t.Fatal("collectRows: want scan error for NULL into string, got nil")
	}
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
}
