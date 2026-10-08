// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/loombeading/loom/internal/domain"
)

func TestImportPolicyKeepsNewerValueAndAuditsTheMerge(t *testing.T) {
	db := testDB(t)
	newer := `{"_type":"coefficient","key":"promote_after","value":"48h","_set_at":{"value":"2026-06-01T00:00:00.000Z"}}` + "\n"
	sum := mustImportLine(t, db, newer)
	if sum.UpdatedCoefficients != 1 {
		t.Fatalf("UpdatedCoefficients = %d, want 1", sum.UpdatedCoefficients)
	}
	older := `{"_type":"coefficient","key":"promote_after","value":"96h","_set_at":{"value":"2026-01-01T00:00:00.000Z"}}` + "\n"
	if sum := mustImportLine(t, db, older); sum.UpdatedCoefficients != 0 {
		t.Fatalf("older row changed the policy: UpdatedCoefficients = %d", sum.UpdatedCoefficients)
	}
	var value string
	if err := db.QueryRowContext(t.Context(), `SELECT value FROM coefficients WHERE key = 'promote_after'`).Scan(&value); err != nil || value != "48h" {
		t.Fatalf("promote_after = %q, %v; want 48h", value, err)
	}
	tie := `{"_type":"coefficient","key":"promote_after","value":"72h","_set_at":{"value":"2026-06-01T00:00:00.000Z"}}` + "\n"
	mustImportLine(t, db, tie)
	if err := db.QueryRowContext(t.Context(), `SELECT value FROM coefficients WHERE key = 'promote_after'`).Scan(&value); err != nil || value != "72h" {
		t.Fatalf("tie on time: promote_after = %q, %v; want the lexicographically larger 72h", value, err)
	}
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_log WHERE kind = 'merge' AND field = 'promote_after'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("merge audit rows for promote_after = %d, %v; want 2", n, err)
	}
}

func TestImportRejectsOutOfRangePolicyUnknownKeyAndPriorityZero(t *testing.T) {
	db := testDB(t)
	for name, line := range map[string]string{
		"out-of-range": `{"_type":"coefficient","key":"expedite_max_open","value":"0","_set_at":{"value":"2026-06-01T00:00:00.000Z"}}`,
		"unknown-key":  `{"_type":"coefficient","key":"nope","value":"1","_set_at":{"value":"2026-06-01T00:00:00.000Z"}}`,
		"priority-0":   `{"_type":"bead","id":"0000000000000000000000000z","namespace":"lm","title":"x","status":"open","priority":0,"type":"task","created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}`,
		"severity-9":   `{"_type":"bead","id":"0000000000000000000000000z","namespace":"lm","title":"x","status":"open","priority":2,"severity":9,"type":"task","created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}`,
	} {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Run(context.Background(), tx, strings.NewReader(line+"\n"), Options{Actor: "test", Now: "n0"})
		_ = tx.Rollback()
		if err == nil {
			t.Errorf("%s: import succeeded, want an error", name)
		}
	}
}

func TestExportImportRoundTripsSeverityExpediteAndPolicy(t *testing.T) {
	src := testDB(t)
	span := time.Hour
	due := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	id := mustCreate(t, src, domain.CreateInput{
		Title: "t", Priority: 2, Severity: 1, BeadType: "task", Namespace: "lm",
		Actor: "u", Now: "2026-01-01T00:00:00.000Z", Due: &due, ExpediteFor: &span, Reason: "why",
	})
	var buf bytes.Buffer
	if err := domain.Export(context.Background(), src, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"_type":"coefficient"`) {
		t.Fatalf("export lacks policy rows:\n%s", buf.String())
	}

	dst := testDB(t)
	mustImportLine(t, dst, buf.String())
	var severity int
	var dueAt, reason string
	if err := dst.QueryRowContext(t.Context(), `SELECT severity, due_at, expedite_reason FROM beads WHERE id = ?`, id).Scan(&severity, &dueAt, &reason); err != nil {
		t.Fatal(err)
	}
	if severity != 1 || dueAt != "2030-01-02T03:04:05.000Z" || reason != "why" {
		t.Fatalf("round trip = severity %d due %q reason %q", severity, dueAt, reason)
	}
}

func importCoefficientLine(t *testing.T, db *sql.DB, line string) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = Run(context.Background(), tx, strings.NewReader(line+"\n"), Options{Actor: "test", Now: "n0"})
	return err
}

func TestImportRejectsMalformedPolicyRows(t *testing.T) {
	db := testDB(t)
	for name, line := range map[string]string{
		"missing-key":   `{"_type":"coefficient","value":"48h","_set_at":{"value":"2026-06-01T00:00:00.000Z"}}`,
		"value-number":  `{"_type":"coefficient","key":"promote_after","value":48,"_set_at":{"value":"2026-06-01T00:00:00.000Z"}}`,
		"bad-at":        `{"_type":"coefficient","key":"promote_after","value":"48h","_set_at":7}`,
		"at-value-type": `{"_type":"coefficient","key":"promote_after","value":"48h","_set_at":{"value":7}}`,
	} {
		if err := importCoefficientLine(t, db, line); err == nil {
			t.Errorf("%s: import succeeded, want an error", name)
		}
	}
}

func TestImportPolicyIdenticalRowIsANoOp(t *testing.T) {
	db := testDB(t)
	line := `{"_type":"coefficient","key":"promote_after","value":"48h","_set_at":{"value":"2026-06-01T00:00:00.000Z"}}` + "\n"
	mustImportLine(t, db, line)
	if sum := mustImportLine(t, db, line); sum.UpdatedCoefficients != 0 {
		t.Fatalf("identical re-import: UpdatedCoefficients = %d, want 0", sum.UpdatedCoefficients)
	}
}

func TestImportPolicyReportsStorageFailures(t *testing.T) {
	line := `{"_type":"coefficient","key":"promote_after","value":"48h","_set_at":{"value":"2026-06-01T00:00:00.000Z"}}`
	for name, ddl := range map[string]string{
		"write": `CREATE TRIGGER deny_coefficients BEFORE INSERT ON coefficients BEGIN SELECT RAISE(ABORT, 'denied'); END`,
		"audit": `CREATE TRIGGER deny_audit BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'denied'); END`,
		"load":  `DROP TABLE coefficients`,
	} {
		db := testDB(t)
		if _, err := db.ExecContext(context.Background(), ddl); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := importCoefficientLine(t, db, line); err == nil {
			t.Errorf("%s: import succeeded, want an error", name)
		}
	}
}
