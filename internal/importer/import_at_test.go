// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/domain"
)

func mustImportLine(t *testing.T, db *sql.DB, jsonl string) *Summary {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	sum, err := Run(ctx, tx, strings.NewReader(jsonl), Options{Actor: "test", Now: "n0"})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("Import: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return sum
}

func TestImportAtNestedFieldLevelLWW(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, domain.CreateInput{
		Title: "before", Priority: 2, BeadType: "task", Namespace: "lm",
		Actor: "u", Now: "2050-01-01T00:00:00.000Z",
	})

	jsonl := `{"_type":"bead","id":"` + id + `","namespace":"lm","title":"after","status":"in_progress","priority":2,"type":"task","labels":[],"created_at":"2000-01-01T00:00:00.000Z","updated_at":"2000-06-01T00:00:00.000Z","_set_at":{"title":"2000-01-01T00:00:00.000Z","status":"2099-01-01T00:00:00.000Z"}}` + "\n"
	sum := mustImportLine(t, db, jsonl)
	if sum.UpdatedBeads != 1 {
		t.Fatalf("UpdatedBeads = %d, want 1", sum.UpdatedBeads)
	}

	var title, status string
	if err := db.QueryRowContext(t.Context(), `SELECT title, status FROM beads WHERE id = ?`, id).Scan(&title, &status); err != nil {
		t.Fatal(err)
	}
	if title != "before" {
		t.Errorf("title = %q, want %q (its _at.title assertion is older than the local write and must lose)", title, "before")
	}
	if status != "in_progress" {
		t.Errorf("status = %q, want %q (its _at.status assertion is newer than the local write and must win)", status, "in_progress")
	}
}

func TestImportNoAtDegradesToRowUpdatedAt(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, domain.CreateInput{
		Title: "before", Priority: 2, BeadType: "task", Namespace: "lm",
		Actor: "u", Now: "2000-01-01T00:00:00.000Z",
	})

	jsonl := `{"_type":"bead","id":"` + id + `","namespace":"lm","title":"after","status":"open","priority":2,"type":"task","labels":[],"created_at":"2000-01-01T00:00:00.000Z","updated_at":"2099-01-01T00:00:00.000Z"}` + "\n"
	mustImportLine(t, db, jsonl)

	var title string
	if err := db.QueryRowContext(t.Context(), `SELECT title FROM beads WHERE id = ?`, id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "after" {
		t.Errorf("title = %q, want %q (no _at object degrades to the row's own updated_at for every field)", title, "after")
	}
}

func TestImportDependencyAtRemovedLWW(t *testing.T) {
	db := testDB(t)
	now := "2000-01-01T00:00:00.000Z"
	a := mustCreate(t, db, domain.CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	b := mustCreate(t, db, domain.CreateInput{Title: "b", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: now})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.AddLink(context.Background(), tx, "u", a, b, domain.BlocksDepType, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	jsonl := `{"_type":"dependency","bead_id":"` + a + `","depends_on_id":"` + b + `","type":"blocks","created_at":"2000-01-01T00:00:00.000Z","removed":true,"_set_at":{"removed":"2099-01-01T00:00:00.000Z"}}` + "\n"
	mustImportLine(t, db, jsonl)

	var removed bool
	if err := db.QueryRowContext(t.Context(), `SELECT removed FROM dependencies WHERE bead_id = ? AND depends_on_id = ? AND type = 'blocks'`, a, b).Scan(&removed); err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Errorf("removed = %v, want true (a newer _at.removed assertion must win)", removed)
	}
}

func TestImportRejectsRowsOutsideExportFormat(t *testing.T) {
	const bead = `{"_type":"bead","id":"rejecttest0000000001","namespace":"lm","title":"t","status":"open","priority":2,"type":"task","labels":[],"created_at":"2000-01-01T00:00:00.000Z","updated_at":"2000-01-01T00:00:00.000Z"`
	cases := []struct {
		name, jsonl, want string
	}{
		{"unknown key", bead + `,"owner":"alice"}`, `unknown key "owner"`},
		{"old key issue_type", bead + `,"issue_type":"task"}`, `unknown key "issue_type"`},
		{"old key assignee", bead + `,"assignee":"alice"}`, `unknown key "assignee"`},
		{"old key lease_expires_at", bead + `,"lease_expires_at":"2000-01-01T00:00:00.000Z"}`, `unknown key "lease_expires_at"`},
		{"old key issue_id", bead + "}\n" + `{"_type":"dependency","issue_id":"rejecttest0000000001","bead_id":"rejecttest0000000001","depends_on_id":"rejecttest0000000001","type":"blocks","created_at":"2000-01-01T00:00:00.000Z","removed":false}`, `unknown key "issue_id"`},
		{"singular external_ref", bead + `,"external_ref":"https://x"}`, `unknown key "external_ref"`},
		{"renamed dedupe_key", bead + `,"dedupe_key":"k"}`, `import: line 1: bead key "dedupe_key" was renamed to "redetect_key"`},
		{"renamed last_seen_at", bead + `,"last_seen_at":"2000-01-01T00:00:00.000Z"}`, `import: line 1: bead key "last_seen_at" was renamed to "last_redetected_at"`},
		{"renamed depth", bead + `,"depth":2}`, `import: line 1: bead key "depth" was renamed to "reasoning_depth"`},
		{"renamed _at", bead + `,"_at":{}}`, `import: line 1: key "_at" was renamed to "_set_at"`},
		{"renamed removed_at", bead + "}\n\n" + `{"_type":"dependency","bead_id":"rejecttest0000000001","depends_on_id":"rejecttest0000000001","type":"blocks","created_at":"2000-01-01T00:00:00.000Z","removed":false,"removed_at":"2000-01-01T00:00:00.000Z"}`, `import: line 3: dependency key "removed_at" was renamed to "removed_set_at"`},
		{"renamed value_at", bead + "}\n" + `{"_type":"coefficient","key":"promote_after","value":"48h","value_at":"2000-01-01T00:00:00.000Z"}`, `import: line 2: coefficient key "value_at" was renamed to "value_set_at"`},
		{"renamed _type policy", bead + "}\n" + `{"_type":"policy","key":"promote_after","value":"48h","_set_at":{"value":"2000-01-01T00:00:00.000Z"}}`, `import: line 2: _type "policy" was renamed to "coefficient"`},
		{"status", strings.Replace(bead, `"status":"open"`, `"status":"blocked"`, 1) + `}`, `out-of-vocabulary status "blocked"`},
		{"type", strings.Replace(bead, `"type":"task"`, `"type":"bug"`, 1) + `}`, `out-of-vocabulary type "bug"`},
		{"link type", bead + "}\n" + `{"_type":"dependency","bead_id":"rejecttest0000000001","depends_on_id":"rejecttest0000000001","type":"related","created_at":"2000-01-01T00:00:00.000Z","removed":false}`, `out-of-vocabulary type "related"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Run(context.Background(), tx, strings.NewReader(tc.jsonl+"\n"), Options{Actor: "test", Now: "n0"})
			_ = tx.Rollback()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want it to contain %q", err, tc.want)
			}
			var n int
			if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM beads`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("beads rows = %d, want 0 after a rejected batch", n)
			}
		})
	}
}

func TestExportImportAtRoundTripByteIdentical(t *testing.T) {
	db1 := testDB(t)
	mustCreate(t, db1, domain.CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})

	var first bytes.Buffer
	if err := domain.Export(context.Background(), db1, &first); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), `"_set_at":{`) {
		t.Fatalf("export line has no nested _at object: %s", first.String())
	}

	db2 := testDB(t)
	tx, err := db2.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), tx, strings.NewReader(first.String()), Options{Actor: "u", Now: "n1"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var second bytes.Buffer
	if err := domain.Export(context.Background(), db2, &second); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatalf("export -> import -> export not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", first.String(), second.String())
	}
}
