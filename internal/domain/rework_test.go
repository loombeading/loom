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

func mustAddRework(t *testing.T, db *sql.DB, in AddReworkInput) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = AddRework(context.Background(), tx, in)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func createForRework(t *testing.T, db *sql.DB) string {
	t.Helper()
	return mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
}

func countReworkAuditRows(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_log WHERE bead_id = ? AND kind = 'field' AND field = 'rework'`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAddReworkRejectsEmptyCause(t *testing.T) {
	db := testDB(t)
	id := createForRework(t, db)

	err := mustAddRework(t, db, AddReworkInput{BeadID: id, Cause: "", Actor: "alice", Now: "n1"})
	if _, ok := errors.AsType[*ValidationError](err); !ok {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if countReworkAuditRows(t, db, id) != 0 {
		t.Errorf("audit rows = %d, want 0 (rejected before insert)", countReworkAuditRows(t, db, id))
	}
}

func TestAddReworkRejectsUnknownCause(t *testing.T) {
	db := testDB(t)
	id := createForRework(t, db)

	err := mustAddRework(t, db, AddReworkInput{BeadID: id, Cause: "typo", Actor: "alice", Now: "n1"})
	if _, ok := errors.AsType[*ValidationError](err); !ok {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if !strings.Contains(err.Error(), "invalid --cause") {
		t.Errorf("err = %q, want it to contain %q", err.Error(), "invalid --cause")
	}
}

func TestAddReworkNonexistentBeadReturnsErrBeadNotFound(t *testing.T) {
	db := testDB(t)

	err := mustAddRework(t, db, AddReworkInput{BeadID: "no-such-id", Cause: "ci", Actor: "alice", Now: "n1"})
	if !errors.Is(err, ErrBeadNotFound) {
		t.Fatalf("err = %v, want ErrBeadNotFound", err)
	}
}

func TestAddReworkWrapsLookupError(t *testing.T) {
	db := testDB(t)
	id := createForRework(t, db)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = AddRework(ctx, tx, AddReworkInput{BeadID: id, Cause: "ci", Actor: "alice", Now: "n1"})
	if err == nil {
		t.Fatal("want an error from a canceled context")
	}
	if errors.Is(err, ErrBeadNotFound) {
		t.Fatalf("err = %v, want the generic lookup error, not ErrBeadNotFound", err)
	}
	if !strings.Contains(err.Error(), "add rework: lookup:") {
		t.Errorf("err = %q, want it to contain %q", err.Error(), "add rework: lookup:")
	}
}

func TestAddReworkRecordsAuditRowPerCall(t *testing.T) {
	db := testDB(t)
	id := createForRework(t, db)

	mustNoErr(t, mustAddRework(t, db, AddReworkInput{BeadID: id, Cause: "ci", Reason: "PR #1", Actor: "alice", Now: "n1"}))

	var kind, field, newValue string
	if err := db.QueryRowContext(t.Context(), `SELECT kind, field, new_value FROM audit_log WHERE bead_id = ? AND field = 'rework'`, id).Scan(&kind, &field, &newValue); err != nil {
		t.Fatal(err)
	}
	if kind != "field" || field != "rework" || newValue != "ci" {
		t.Errorf("row = (%q, %q, %q), want (field, rework, ci)", kind, field, newValue)
	}

	mustNoErr(t, mustAddRework(t, db, AddReworkInput{BeadID: id, Cause: "review", Actor: "bob", Now: "n2"}))

	if got := countReworkAuditRows(t, db, id); got != 2 {
		t.Errorf("audit rows = %d, want 2 (append-only)", got)
	}
}
