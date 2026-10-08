// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseSpanAcceptsDaysAndDurationsAndRejectsGarbage(t *testing.T) {
	for in, want := range map[string]time.Duration{"2d": 48 * time.Hour, "90m": 90 * time.Minute, "1.5d": 36 * time.Hour} {
		got, err := ParseSpan(in)
		if err != nil || got != want {
			t.Errorf("ParseSpan(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"xd", "zzz", ""} {
		if _, err := ParseSpan(in); err == nil {
			t.Errorf("ParseSpan(%q) succeeded, want an error", in)
		}
	}
}

func TestValidateCoefficientValueRejectsBadDurationsAndIntegers(t *testing.T) {
	for _, c := range []struct{ key, value string }{
		{"promote_after", "soon"},
		{"promote_after", "-1h"},
		{"cutoff_priority", "two"},
		{"cutoff_priority", "9"},
		{"no_such_key", "1"},
	} {
		var ve *ValidationError
		if err := ValidateCoefficientValue(c.key, c.value); !errors.As(err, &ve) {
			t.Errorf("ValidateCoefficientValue(%q, %q) = %v, want *ValidationError", c.key, c.value, err)
		}
	}
	if err := ValidateCoefficientValue("expedite_max_open", "5"); err != nil {
		t.Errorf("expedite_max_open 5: %v", err)
	}
}

func TestLoadCoefficientsRejectsCeilingAboveFloor(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE coefficients SET value = ? WHERE key = ?`, "4", "severity_2_ceiling"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCoefficients(ctx, db); err == nil || !strings.Contains(err.Error(), "severity_2_ceiling") {
		t.Errorf("ceiling above floor: err = %v, want an error naming severity_2_ceiling", err)
	}
}

func TestCreateBeadRejectsSeverityOutOfRange(t *testing.T) {
	db := testDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = CreateBead(context.Background(), tx, CreateInput{Title: "t", Priority: 2, Severity: 9, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n"})
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Msg, "severity must be between 1 and 4") {
		t.Fatalf("severity 9: err = %v, want *ValidationError naming 1 and 4", err)
	}
}

func updateScheduled(t *testing.T, db *sql.DB, in UpdateInput) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	in.Actor, in.Now = "u", "2026-01-02T00:00:00.000Z"
	if _, err = UpdateBead(context.Background(), tx, in); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func TestUpdateBeadSeverityDueExpediteAndClear(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	id := mustCreate(t, db, CreateInput{Title: "t", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	read := func() (int, sql.NullString, sql.NullString) {
		t.Helper()
		var severity int
		var due, until sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT severity, due_at, expedite_until FROM beads WHERE id = ?`, id).Scan(&severity, &due, &until); err != nil {
			t.Fatal(err)
		}
		return severity, due, until
	}

	bad, good := 7, 1
	var ve *ValidationError
	if err := updateScheduled(t, db, UpdateInput{ID: id, Priority: -1, Severity: &bad}); !errors.As(err, &ve) {
		t.Fatalf("severity 7: err = %v, want *ValidationError", err)
	}
	due := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	span := time.Hour
	if err := updateScheduled(t, db, UpdateInput{ID: id, Priority: -1, Severity: &good, Due: &due, ExpediteFor: &span, Reason: "r"}); err != nil {
		t.Fatal(err)
	}
	if s, d, u := read(); s != 1 || d.String != "2030-01-02T03:04:05.000Z" || !u.Valid {
		t.Fatalf("after update: severity %d due %v expedite_until %v", s, d, u)
	}

	if err := updateScheduled(t, db, UpdateInput{ID: id, Priority: -1, ExpediteFor: &span}); !errors.As(err, &ve) || !strings.Contains(ve.Msg, "--reason") {
		t.Errorf("expedite without reason: err = %v, want *ValidationError naming --reason", err)
	}
	zero := time.Duration(0)
	if err := updateScheduled(t, db, UpdateInput{ID: id, Priority: -1, ExpediteFor: &zero, Reason: "r"}); !errors.As(err, &ve) || !strings.Contains(ve.Msg, "positive") {
		t.Errorf("zero expedite: err = %v, want *ValidationError naming positive", err)
	}

	if err := updateScheduled(t, db, UpdateInput{ID: id, Priority: -1, Clear: []string{"due", "expedite"}}); err != nil {
		t.Fatal(err)
	}
	if _, d, u := read(); d.Valid || u.Valid {
		t.Fatalf("after clear: due %v expedite_until %v, want both NULL", d, u)
	}
}

func TestExpediteFailsWhenPolicyRowIsMissing(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	id := mustCreate(t, db, CreateInput{Title: "t", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	if _, err := db.ExecContext(ctx, `DELETE FROM coefficients WHERE key = 'expedite_max_age'`); err != nil {
		t.Fatal(err)
	}
	span := time.Hour
	if err := updateScheduled(t, db, UpdateInput{ID: id, Priority: -1, ExpediteFor: &span, Reason: "r"}); err == nil || !strings.Contains(err.Error(), "expedite_max_age") {
		t.Fatalf("expedite with missing policy: err = %v, want an error naming expedite_max_age", err)
	}
}

func TestEffectivePriorityOfUnknownIDIsAnError(t *testing.T) {
	db := testDB(t)
	if _, _, err := EffectivePriorityOf(context.Background(), db, "", "no-such-id"); err == nil {
		t.Fatal("EffectivePriorityOf of an unknown id succeeded, want an error")
	}
}

func TestExportCoefficientsPropagatesQueryAndEmitErrors(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	want := errors.New("emit failed")
	if err := ExportCoefficients(ctx, db, func(ExportRow) error { return want }); !errors.Is(err, want) {
		t.Errorf("emit error: err = %v, want %v", err, want)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ExportCoefficients(ctx, db, func(ExportRow) error { return nil }); err == nil {
		t.Error("export on a closed DB succeeded, want an error")
	}
}
