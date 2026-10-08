// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/domain"
)

func exportString(t *testing.T, db *sql.DB) string {
	t.Helper()
	var buf bytes.Buffer
	if err := domain.Export(context.Background(), db, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func importWithNow(t *testing.T, db *sql.DB, jsonl, now string) *Summary {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := Run(ctx, tx, strings.NewReader(jsonl), Options{Actor: "test", Now: now})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("Import: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return sum
}

func inTx(t *testing.T, db *sql.DB, fn func(tx *sql.Tx) error) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func auditCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func twoEnvsWithLink(t *testing.T) (*sql.DB, *sql.DB, string, string) {
	t.Helper()
	now := "2000-01-01T00:00:00.000Z"
	dbA := testDB(t)
	a := mustCreate(t, dbA, domain.CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	b := mustCreate(t, dbA, domain.CreateInput{Title: "b", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	inTx(t, dbA, func(tx *sql.Tx) error {
		return domain.AddLink(context.Background(), tx, "u", a, b, domain.BlocksDepType, now, "")
	})
	dbB := testDB(t)
	importWithNow(t, dbB, exportString(t, dbA), "n0")
	return dbA, dbB, a, b
}

func TestImportLinkReactivationConvergesAcrossEnvironments(t *testing.T) {
	ctx := context.Background()
	dbA, dbB, a, b := twoEnvsWithLink(t)
	inTx(t, dbA, func(tx *sql.Tx) error {
		if err := domain.RemoveLink(ctx, tx, "u", a, b, domain.BlocksDepType, "2000-02-01T00:00:00.000Z", ""); err != nil {
			return err
		}
		return domain.UpsertLink(ctx, tx, "u", a, b, domain.BlocksDepType, "2000-03-01T00:00:00.000Z", "")
	})
	inTx(t, dbB, func(tx *sql.Tx) error {
		return domain.RemoveLink(ctx, tx, "u", a, b, domain.BlocksDepType, "2000-02-01T00:00:00.000Z", "")
	})

	exA, exB := exportString(t, dbA), exportString(t, dbB)
	importWithNow(t, dbA, exB, "n1")
	importWithNow(t, dbB, exA, "n2")

	gotA, gotB := exportString(t, dbA), exportString(t, dbB)
	if gotA != gotB {
		t.Fatalf("exports differ after mutual import:\n--- A ---\n%s\n--- B ---\n%s", gotA, gotB)
	}
	if !strings.Contains(gotA, `"removed":false`) || !strings.Contains(gotA, `"_set_at":{"removed":"2000-03-01T00:00:00.000Z"}`) {
		t.Errorf("want the reactivation (2000-03-01) to win, got:\n%s", gotA)
	}
}

func TestImportLinkCreatedIndependentlyConverges(t *testing.T) {
	ctx := context.Background()
	now := "2000-01-01T00:00:00.000Z"
	dbA := testDB(t)
	a := mustCreate(t, dbA, domain.CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	b := mustCreate(t, dbA, domain.CreateInput{Title: "b", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	dbB := testDB(t)
	importWithNow(t, dbB, exportString(t, dbA), "n0")
	inTx(t, dbA, func(tx *sql.Tx) error {
		return domain.AddLink(ctx, tx, "u", a, b, domain.BlocksDepType, "2000-02-01T00:00:00.000Z", "")
	})
	inTx(t, dbB, func(tx *sql.Tx) error {
		return domain.AddLink(ctx, tx, "u", a, b, domain.BlocksDepType, "2000-03-01T00:00:00.000Z", "")
	})

	exA, exB := exportString(t, dbA), exportString(t, dbB)
	importWithNow(t, dbA, exB, "n1")
	importWithNow(t, dbB, exA, "n2")
	if gotA, gotB := exportString(t, dbA), exportString(t, dbB); gotA != gotB {
		t.Fatalf("exports differ after mutual import:\n--- A ---\n%s\n--- B ---\n%s", gotA, gotB)
	}
}

func TestImportSameJSONLTwiceAddsNoAuditRows(t *testing.T) {
	ctx := context.Background()
	dbA, _, a, b := twoEnvsWithLink(t)
	inTx(t, dbA, func(tx *sql.Tx) error {
		if err := domain.AddTokenCost(ctx, tx, domain.AddTokenCostInput{BeadID: a, TokensIn: 1, TokensOut: 2, Actor: "u", Now: "2000-01-02T00:00:00.000Z"}); err != nil {
			return err
		}
		return domain.RemoveLink(ctx, tx, "u", a, b, domain.BlocksDepType, "2000-02-01T00:00:00.000Z", "")
	})
	jsonl := exportString(t, dbA) + `{"_type":"dependency","bead_id":"` + a + `","depends_on_id":"missing0000000000000000000","type":"blocks","created_at":"2000-01-01T00:00:00.000Z","removed":false}` + "\n"

	db := testDB(t)
	importWithNow(t, db, jsonl, "n0")
	for _, want := range []string{"token_cost", "dangling", domain.FieldDependency} {
		var n int
		if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_log WHERE kind = 'merge' AND origin = 'import' AND field = ?`, want).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Errorf("no merge audit row for field %q", want)
		}
	}

	before := auditCount(t, db)
	importWithNow(t, db, jsonl, "n1")
	if after := auditCount(t, db); after != before {
		t.Errorf("audit rows %d -> %d on re-import of the same JSONL, want unchanged", before, after)
	}
}

func TestImportKeepsTokenCostActor(t *testing.T) {
	src := testDB(t)
	a := mustCreate(t, src, domain.CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2000-01-01T00:00:00.000Z"})
	inTx(t, src, func(tx *sql.Tx) error {
		return domain.AddTokenCost(context.Background(), tx, domain.AddTokenCostInput{BeadID: a, TokensIn: 1, TokensOut: 2, Actor: "reporter", Now: "2000-01-02T00:00:00.000Z"})
	})

	dst := testDB(t)
	importWithNow(t, dst, exportString(t, src), "n0")
	var actor string
	if err := dst.QueryRowContext(t.Context(), `SELECT actor FROM token_costs WHERE bead_id = ?`, a).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if actor != "reporter" {
		t.Errorf("token_costs.actor after import = %q, want %q", actor, "reporter")
	}
}

func TestImportCycleResolutionIgnoresImportTime(t *testing.T) {
	now := "2000-01-01T00:00:00.000Z"
	src := testDB(t)
	a := mustCreate(t, src, domain.CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	b := mustCreate(t, src, domain.CreateInput{Title: "b", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	jsonl := exportString(t, src) +
		`{"_type":"dependency","bead_id":"` + a + `","depends_on_id":"` + b + `","type":"blocks","created_at":"2000-01-01T00:00:00.000Z","removed":false}` + "\n" +
		`{"_type":"dependency","bead_id":"` + b + `","depends_on_id":"` + a + `","type":"blocks","created_at":"2000-02-01T00:00:00.000Z","removed":false}` + "\n"

	db1, db2 := testDB(t), testDB(t)
	if sum := importWithNow(t, db1, jsonl, "2090-01-01T00:00:00.000Z"); len(sum.CycleLines) != 1 {
		t.Fatalf("CycleLines = %v, want 1", sum.CycleLines)
	}
	importWithNow(t, db2, jsonl, "2091-01-01T00:00:00.000Z")
	got1, got2 := exportString(t, db1), exportString(t, db2)
	if got1 != got2 {
		t.Fatalf("exports differ by import time:\n--- 1 ---\n%s\n--- 2 ---\n%s", got1, got2)
	}
	if strings.Contains(got1, "2090") {
		t.Errorf("removed_set_at took the import time:\n%s", got1)
	}
}

func TestImportDuplicateParentResolutionIgnoresImportTime(t *testing.T) {
	now := "2000-01-01T00:00:00.000Z"
	src := testDB(t)
	c := mustCreate(t, src, domain.CreateInput{Title: "c", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	p1 := mustCreate(t, src, domain.CreateInput{Title: "p1", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	p2 := mustCreate(t, src, domain.CreateInput{Title: "p2", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	jsonl := exportString(t, src) +
		`{"_type":"dependency","bead_id":"` + c + `","depends_on_id":"` + p1 + `","type":"parent-child","created_at":"2000-01-01T00:00:00.000Z","removed":false}` + "\n" +
		`{"_type":"dependency","bead_id":"` + c + `","depends_on_id":"` + p2 + `","type":"parent-child","created_at":"2000-02-01T00:00:00.000Z","removed":false}` + "\n"

	db := testDB(t)
	if sum := importWithNow(t, db, jsonl, "2090-01-01T00:00:00.000Z"); len(sum.ParentLines) != 1 {
		t.Fatalf("ParentLines = %v, want 1", sum.ParentLines)
	}
	if got := exportString(t, db); strings.Contains(got, "2090") {
		t.Errorf("removed_set_at took the import time:\n%s", got)
	}
}

func TestImportRejectsDepthOutOfRange(t *testing.T) {
	for _, depth := range []string{"0", "6", "-1"} {
		db := testDB(t)
		jsonl := `{"_type":"bead","id":"x0000000000000000000000000","namespace":"lm","title":"x","status":"open","priority":2,"type":"task","reasoning_depth":` + depth + `,"created_at":"2000-01-01T00:00:00.000Z","updated_at":"2000-01-01T00:00:00.000Z"}` + "\n"
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Run(context.Background(), tx, strings.NewReader(jsonl), Options{Actor: "test", Now: "n0"})
		_ = tx.Rollback()
		if depth == "0" {
			if err != nil {
				t.Errorf("depth 0 (unset): unexpected error %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "reasoning_depth must be between") {
			t.Errorf("depth %s: err = %v, want range error", depth, err)
		}
	}
}

func TestParseDependencyRow(t *testing.T) {
	for _, tc := range []struct {
		row     string
		want    linkState
		wantErr string
	}{
		{`{"created_at":"c","removed":false}`, linkState{ts: "c"}, ""},
		{`{"created_at":"c","removed":true}`, linkState{removed: true, ts: "c", explicit: true}, ""},
		{`{"created_at":"c","removed":false,"_set_at":{"removed":"r"}}`, linkState{ts: "r", explicit: true}, ""},
		{`{"created_at":1}`, linkState{}, "created_at"},
		{`{"created_at":"c","removed":"x"}`, linkState{}, "removed"},
		{`{"created_at":"c","removed":false,"_set_at":1}`, linkState{}, "_set_at"},
		{`{"created_at":"c","removed":false,"_set_at":{"removed":1}}`, linkState{}, "_at.removed"},
	} {
		var row importRow
		if err := json.Unmarshal([]byte(tc.row), &row); err != nil {
			t.Fatal(err)
		}
		_, got, err := parseDependencyRow(row)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want %q", tc.row, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %+v, %v, want %+v", tc.row, got, err, tc.want)
		}
	}
}

func TestMergeDependencyPropagatesErrors(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	k := depKey{beadID: "a", dependsOnID: "b", typ: "blocks"}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if _, err := mergeDependency(ctx, tx, k, nil, Options{}, &Summary{}); err == nil || !strings.Contains(err.Error(), "load dependency") {
		t.Errorf("done tx: err = %v, want load dependency", err)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	bad := []importRow{{"created_at": json.RawMessage(`1`)}}
	if _, err := mergeDependency(ctx, tx, k, bad, Options{}, &Summary{}); err == nil || !strings.Contains(err.Error(), "created_at") {
		t.Errorf("bad row: err = %v, want created_at", err)
	}
}

func TestLinkStateNewerThanTieBreaks(t *testing.T) {
	for _, tc := range []struct {
		a, b linkState
		want bool
	}{
		{linkState{ts: "2"}, linkState{ts: "1", removed: true}, true},
		{linkState{ts: "1", removed: true}, linkState{ts: "1"}, true},
		{linkState{ts: "1"}, linkState{ts: "1", removed: true}, false},
		{linkState{ts: "1", explicit: true}, linkState{ts: "1"}, true},
		{linkState{ts: "1"}, linkState{ts: "1", explicit: true}, false},
	} {
		if got := tc.a.newerThan(tc.b); got != tc.want {
			t.Errorf("%+v.newerThan(%+v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
