// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

type edd32DepState struct {
	count        int
	removed      bool
	removedSetAt sql.NullString
}

func edd32DepRows(t *testing.T, db *sql.DB, beadID string) edd32DepState {
	t.Helper()
	var s edd32DepState
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM dependencies WHERE bead_id = ?`, beadID).Scan(&s.count); err != nil {
		t.Fatal(err)
	}
	if s.count == 0 {
		return s
	}
	if err := db.QueryRowContext(t.Context(), `SELECT removed, removed_set_at FROM dependencies WHERE bead_id = ?`, beadID).Scan(&s.removed, &s.removedSetAt); err != nil {
		t.Fatal(err)
	}
	return s
}

func edd32Tx(t *testing.T, db *sql.DB, fn func(tx *sql.Tx) error) {
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

func TestEDD32RuleReactivationUpdatesRemovedSetAt(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")

	edd32Tx(t, db, func(tx *sql.Tx) error {
		return AddLink(context.Background(), tx, "u", a, b, BlocksDepType, "n2", "")
	})
	s := edd32DepRows(t, db, a)
	if s.count != 1 || s.removed || s.removedSetAt.Valid {
		t.Fatalf("after add: %+v, want 1 row, removed=false, removed_set_at NULL", s)
	}

	edd32Tx(t, db, func(tx *sql.Tx) error {
		return RemoveLink(context.Background(), tx, "u", a, b, BlocksDepType, "n3", "")
	})
	s = edd32DepRows(t, db, a)
	if s.count != 1 || !s.removed || s.removedSetAt.String != "n3" {
		t.Fatalf("after remove: %+v, want 1 row, removed=true, removed_set_at n3", s)
	}

	edd32Tx(t, db, func(tx *sql.Tx) error {
		return UpsertLink(context.Background(), tx, "u", a, b, BlocksDepType, "n5", "")
	})
	s = edd32DepRows(t, db, a)
	if s.count != 1 {
		t.Errorf("after reactivation: %d rows for (a, b, blocks), want the same single row", s.count)
	}
	if s.removed {
		t.Error("after reactivation: removed = true, want false")
	}
	if !s.removedSetAt.Valid || s.removedSetAt.String != "n5" {
		t.Errorf("after reactivation: removed_set_at = %+v, want n5 (the reactivation time)", s.removedSetAt)
	}
}

func TestEDD32RuleLinkAuditSerializesDependency(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")

	last := func() AuditRow {
		t.Helper()
		rows, err := AuditHistory(context.Background(), db, a)
		if err != nil {
			t.Fatal(err)
		}
		return rows[len(rows)-1]
	}
	decode := func(r AuditRow) map[string]any {
		t.Helper()
		if r.Kind != AuditKindField || !r.Field.Valid || r.Field.String != "dependency" {
			t.Fatalf("last audit row = %+v, want kind=field field=dependency", r)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(r.NewValue.String), &got); err != nil {
			t.Fatalf("new_value %q is not JSON: %v", r.NewValue.String, err)
		}
		return got
	}

	edd32Tx(t, db, func(tx *sql.Tx) error {
		return AddLink(context.Background(), tx, "u", a, b, BlocksDepType, "n2", "")
	})
	want := map[string]any{"depends_on_id": b, "type": BlocksDepType, "removed": false}
	if got := decode(last()); !reflect.DeepEqual(got, want) {
		t.Errorf("add new_value = %v, want %v", got, want)
	}

	edd32Tx(t, db, func(tx *sql.Tx) error {
		return RemoveLink(context.Background(), tx, "u", a, b, BlocksDepType, "n3", "")
	})
	want = map[string]any{"depends_on_id": b, "type": BlocksDepType, "removed": true}
	if got := decode(last()); !reflect.DeepEqual(got, want) {
		t.Errorf("remove new_value = %v, want %v", got, want)
	}
}

func TestEDD32RuleNoteHasNullFieldAndValue(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Reason: "handoff to carol", Actor: "alice", Now: "2026-01-01T00:05:00.000Z"}))

	var fieldNull, valueNull bool
	var reason sql.NullString
	err := db.QueryRowContext(t.Context(), `
		SELECT field IS NULL, new_value IS NULL, reason FROM audit_log
		WHERE bead_id = ? AND kind = ?`, id, AuditKindNote).Scan(&fieldNull, &valueNull, &reason)
	if err != nil {
		t.Fatalf("query note row: %v", err)
	}
	if !fieldNull {
		t.Error("note row field is not NULL, want NULL")
	}
	if !valueNull {
		t.Error("note row new_value is not NULL, want NULL")
	}
	if !reason.Valid || reason.String != "handoff to carol" {
		t.Errorf("note row reason = %+v, want handoff to carol", reason)
	}
}

func TestEDD32RuleClaimAndReleaseAuditValues(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "2026-01-01T00:01:00.000Z"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Release: true, Actor: "alice", Now: "2026-01-01T00:02:00.000Z"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "2026-01-01T00:03:00.000Z"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Release: true, Force: true, Actor: "bob", Now: "2026-01-01T00:04:00.000Z"}))

	rows, err := AuditHistory(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	type rec struct{ field, actor, newValue string }
	var got []rec
	for _, r := range rows {
		switch r.Field.String {
		case FieldClaim, FieldRelease, FieldForceRelease:
			got = append(got, rec{r.Field.String, r.Actor, r.NewValue.String})
		}
	}
	want := []rec{
		{"claim", "alice", "alice"},
		{"release", "alice", "alice"},
		{"claim", "alice", "alice"},
		{"force_release", "bob", "bob"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("claim/release audit rows = %v, want %v", got, want)
	}
}

func TestEDD32RuleCreateRecordsEveryNonNullField(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{
		Title: "hello", Priority: 3, BeadType: "task", Namespace: "lm",
		Summary: "short", Depth: 4, ExternalRefs: []string{"https://example.com/pr/1"},
		Actor: "alice", Now: "2026-01-01T00:00:00.000Z",
	})

	rows, err := AuditHistory(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	values := map[string]string{}
	for _, r := range rows {
		if r.Kind != AuditKindField {
			t.Errorf("creation audit row kind = %q, want field", r.Kind)
			continue
		}
		fields = append(fields, r.Field.String)
		values[r.Field.String] = r.NewValue.String
	}
	sort.Strings(fields)
	wantFields := []string{"external_refs", "namespace", "priority", "reasoning_depth", "status", "summary", "title", "type"}
	if !reflect.DeepEqual(fields, wantFields) {
		t.Errorf("creation audit fields = %v, want %v (description and labels are NULL and must not appear)", fields, wantFields)
	}
	wantValues := map[string]string{
		"summary": "short", "reasoning_depth": "4", "priority": "3", "title": "hello",
		"external_refs": `["https://example.com/pr/1"]`,
	}
	for field, want := range wantValues {
		if values[field] != want {
			t.Errorf("creation audit %q = %q, want %q", field, values[field], want)
		}
	}
}
