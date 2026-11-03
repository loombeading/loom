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

func setCoefficientIn(t *testing.T, db *sql.DB, key, value, reason string) (string, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	prev, serr := SetCoefficient(ctx, tx, key, value, reason, "tester", "2026-10-20T00:00:00.000Z")
	if serr != nil {
		_ = tx.Rollback()
		return "", serr
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return prev, nil
}

func TestSetCoefficientWritesValueAndAudit(t *testing.T) {
	db := testDB(t)
	prev, err := setCoefficientIn(t, db, "due_lead", "48h", "tune")
	if err != nil {
		t.Fatalf("SetCoefficient: %v", err)
	}
	if prev != "72h" {
		t.Errorf("previous value = %q, want 72h", prev)
	}
	var value, setAt string
	if err := db.QueryRowContext(t.Context(), `SELECT value, value_set_at FROM coefficients WHERE key = 'due_lead'`).Scan(&value, &setAt); err != nil {
		t.Fatal(err)
	}
	if value != "48h" || setAt != "2026-10-20T00:00:00.000Z" {
		t.Errorf("coefficient = (%q, %q), want (48h, 2026-10-20T00:00:00.000Z)", value, setAt)
	}
	var actor, field, newValue, reason string
	var beadID sql.NullString
	if err := db.QueryRowContext(t.Context(), `SELECT actor, bead_id, field, new_value, reason FROM audit_log WHERE kind = 'coefficient'`).
		Scan(&actor, &beadID, &field, &newValue, &reason); err != nil {
		t.Fatal(err)
	}
	if actor != "tester" || beadID.Valid || field != "due_lead" || newValue != "48h" || reason != "tune" {
		t.Errorf("audit = (%q, %v, %q, %q, %q), want a bead-less due_lead=48h row by tester", actor, beadID, field, newValue, reason)
	}
}

func TestSetCoefficientRejectionsAreValidationErrors(t *testing.T) {
	db := testDB(t)
	cases := []struct {
		name, key, value, reason, want string
	}{
		{"blank reason", "due_lead", "48h", "  ", "--reason"},
		{"unknown key", "nope", "1", "r", "known keys: cancel_after, cutoff_priority, decay_after, due_lead"},
		{"out of range", "cutoff_priority", "5", "r", "between 1 and 4"},
		{"ceiling above floor", "severity_2_ceiling", "3", "r", "must not exceed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := setCoefficientIn(t, db, tc.key, tc.value, tc.reason)
			var ve *ValidationError
			if !errors.As(err, &ve) || !strings.Contains(ve.Msg, tc.want) {
				t.Fatalf("err = %v, want *ValidationError containing %q", err, tc.want)
			}
		})
	}
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_log WHERE kind = 'coefficient'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("audit rows = %d (err %v), want 0", n, err)
	}
}

func TestSetCoefficientLoadErrorIsReturned(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `DROP TABLE coefficients`); err != nil {
		t.Fatal(err)
	}
	if _, err := setCoefficientIn(t, db, "due_lead", "48h", "r"); err == nil || !strings.Contains(err.Error(), "load coefficients") {
		t.Errorf("err = %v, want a load coefficients error", err)
	}
}
