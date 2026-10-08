// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"testing"
)

func TestAuditFieldRequiresFieldAndNewValue(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a", "lm", "2026-01-01T00:00:00.000Z")
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := Audit(context.Background(), tx, AuditRecord{Kind: AuditKindField, BeadID: "a", Actor: "u"}); err == nil {
		t.Fatal("Audit(kind=field, no field/new_value) = nil error, want an error")
	}
}

func TestAuditNoteRequiresReason(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a", "lm", "2026-01-01T00:00:00.000Z")
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := Audit(context.Background(), tx, AuditRecord{Kind: AuditKindNote, BeadID: "a", Actor: "u"}); err == nil {
		t.Fatal("Audit(kind=note, no reason) = nil error, want an error")
	}
}

func TestAuditWritesRow(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a", "lm", "2026-01-01T00:00:00.000Z")
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Audit(context.Background(), tx, AuditRecord{
		OccurredAt: "2026-01-02T00:00:00.000Z", Actor: "u", BeadID: "a",
		Kind: AuditKindField, Field: "title", NewValue: "new title",
	}); err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var field, newValue string
	var origin string
	err = db.QueryRowContext(t.Context(), `SELECT field, new_value, origin FROM audit_log WHERE bead_id = ?`, "a").Scan(&field, &newValue, &origin)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if field != "title" || newValue != "new title" {
		t.Errorf("field/new_value = %q/%q, want title/new title", field, newValue)
	}
	if origin != OriginLocal {
		t.Errorf("origin = %q, want %q (default)", origin, OriginLocal)
	}
}
