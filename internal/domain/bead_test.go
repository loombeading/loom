// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func mustCreate(t *testing.T, db *sql.DB, in CreateInput) string {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := CreateBead(context.Background(), tx, in)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateBead: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCreateBeadMinimal(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{
		Title: "hello", Priority: DefaultPriority, BeadType: DefaultBeadType,
		Namespace: "lm", Actor: "alice", Now: "2026-01-01T00:00:00.000Z",
	})

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatalf("GetBead: %v", err)
	}
	if b.Title != "hello" || b.Status != StatusOpen || b.Priority != DefaultPriority || b.BeadType != DefaultBeadType {
		t.Errorf("bead = %+v, want title=hello status=open priority=%d type=%s", b, DefaultPriority, DefaultBeadType)
	}
	if b.Description.Valid {
		t.Errorf("description = %+v, want unset", b.Description)
	}
}

func TestCreateBeadRecordsAllSetFields(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{
		Title: "hello", Description: "body", Priority: 1, BeadType: "task",
		Labels: []string{"z", "a", "a"}, Namespace: "lm", Actor: "alice", Now: "2026-01-01T00:00:00.000Z",
	})

	rows, err := AuditHistory(context.Background(), db, id)
	if err != nil {
		t.Fatalf("AuditHistory: %v", err)
	}
	want := map[string]string{
		"namespace": "lm", "title": "hello", "status": "open", "priority": "1",
		"type": "task", "description": "body", "labels": `["a","z"]`,
	}
	got := map[string]string{}
	for _, r := range rows {
		if r.Kind != AuditKindField {
			t.Errorf("unexpected kind %q in creation audit trail", r.Kind)
			continue
		}
		got[r.Field.String] = r.NewValue.String
	}
	for field, wantVal := range want {
		if got[field] != wantVal {
			t.Errorf("audit field %q = %q, want %q", field, got[field], wantVal)
		}
	}
	if len(got) != len(want) {
		t.Errorf("audit trail has %d fields, want %d (got %v)", len(got), len(want), got)
	}

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Labels) != 2 || b.Labels[0] != "a" || b.Labels[1] != "z" {
		t.Errorf("labels = %v, want [a z] (deduped, sorted)", b.Labels)
	}
}

func TestCreateBeadWithParentAndBlockedBy(t *testing.T) {
	db := testDB(t)
	parent := mustCreate(t, db, CreateInput{Title: "epic", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	blocker := mustCreate(t, db, CreateInput{Title: "blocker", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.001Z"})

	child := mustCreate(t, db, CreateInput{
		Title: "child", Priority: 2, BeadType: "task", Namespace: "lm",
		Parent: parent, BlockedBy: []string{blocker},
		Actor: "u", Now: "2026-01-01T00:00:00.002Z",
	})

	links, err := Links(context.Background(), db, child, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("links = %v, want 2 rows", links)
	}
	byType := map[string]string{}
	for _, l := range links {
		byType[l.Type] = l.DependsOnID
	}
	if byType[ParentChildDepType] != parent {
		t.Errorf("parent-child depends_on_id = %q, want %q", byType[ParentChildDepType], parent)
	}
	if byType[BlocksDepType] != blocker {
		t.Errorf("blocks depends_on_id = %q, want %q", byType[BlocksDepType], blocker)
	}
}

func TestCreateBeadBadParentCreatesNothing(t *testing.T) {
	db := testDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateBead(context.Background(), tx, CreateInput{
		Title: "child", Priority: 2, BeadType: "task", Namespace: "lm",
		Parent: "deadbeef", Actor: "u", Now: "2026-01-01T00:00:00.000Z",
	})
	if err == nil {
		t.Fatal("CreateBead with an unresolvable --parent = nil error, want an error")
	}
	_ = tx.Rollback()

	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM beads`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("beads count = %d, want 0 (nothing committed)", n)
	}
}

func TestCreateBeadRejectsBadPriority(t *testing.T) {
	db := testDB(t)
	tx, _ := db.BeginTx(context.Background(), nil)
	defer func() { _ = tx.Rollback() }()
	_, err := CreateBead(context.Background(), tx, CreateInput{Title: "x", Priority: 5, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n"})
	if err == nil {
		t.Fatal("CreateBead with priority=5 = nil error, want an error")
	}
}

func TestCreateBeadRejectsBadType(t *testing.T) {
	db := testDB(t)
	tx, _ := db.BeginTx(context.Background(), nil)
	defer func() { _ = tx.Rollback() }()
	_, err := CreateBead(context.Background(), tx, CreateInput{Title: "x", Priority: 2, BeadType: "nonsense", Namespace: "lm", Actor: "u", Now: "n"})
	if err == nil {
		t.Fatal("CreateBead with an unknown type = nil error, want an error")
	}
}

func TestCreateBeadWithExternalRef(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{
		Title: "hello", Priority: 2, BeadType: "task", Namespace: "lm",
		ExternalRefs: []string{"https://github.com/example/repo/pull/1"},
		Actor:        "alice", Now: "2026-01-01T00:00:00.000Z",
	})

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatalf("GetBead: %v", err)
	}
	if len(b.ExternalRefs) != 1 || b.ExternalRefs[0] != "https://github.com/example/repo/pull/1" {
		t.Errorf("ExternalRefs = %+v, want the given URL", b.ExternalRefs)
	}

	var externalRefsAt sql.NullString
	if err := db.QueryRowContext(t.Context(), `SELECT external_refs_set_at FROM beads WHERE id = ?`, id).Scan(&externalRefsAt); err != nil {
		t.Fatal(err)
	}
	if externalRefsAt.String != "2026-01-01T00:00:00.000Z" {
		t.Errorf("external_refs_set_at = %+v, want the creation time", externalRefsAt)
	}

	rows, err := AuditHistory(context.Background(), db, id)
	if err != nil {
		t.Fatalf("AuditHistory: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Field.Valid && r.Field.String == "external_refs" && r.NewValue.String == `["https://github.com/example/repo/pull/1"]` {
			found = true
		}
	}
	if !found {
		t.Errorf("AuditHistory = %+v, want an external_refs field record", rows)
	}
}

func TestCreateBeadWithoutExternalRefIsUnset(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "hello", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "alice", Now: "2026-01-01T00:00:00.000Z"})

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatalf("GetBead: %v", err)
	}
	if len(b.ExternalRefs) != 0 {
		t.Errorf("ExternalRefs = %+v, want unset", b.ExternalRefs)
	}
}

func TestCreateBeadRejectsInvalidInput(t *testing.T) {
	base := CreateInput{Title: "t", Priority: DefaultPriority, BeadType: DefaultBeadType, Namespace: "lm", Now: "2026-01-01T00:00:00.000Z"}
	cases := map[string]func(*CreateInput){
		"empty title":   func(in *CreateInput) { in.Title = "" },
		"bad namespace": func(in *CreateInput) { in.Namespace = "Bad NS" },
		"bad depth":     func(in *CreateInput) { in.Depth = MaxReasoningDepth + 1 },
		"no milestone":  func(in *CreateInput) { in.RequireMilestone = true },
		"release label": func(in *CreateInput) { in.Labels = []string{"release:v1"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := base
			mutate(&in)
			var ve *ValidationError
			if err := validateCreateInput(in); !errors.As(err, &ve) {
				t.Fatalf("validateCreateInput = %v, want *ValidationError", err)
			}
		})
	}
}

func TestCreateBeadPropagatesWriteErrors(t *testing.T) {
	db := testDB(t)
	parent := mustCreate(t, db, CreateInput{Title: "p", Priority: DefaultPriority, BeadType: DefaultBeadType, Namespace: "lm", Now: "2026-01-01T00:00:00.000Z"})
	cases := []struct {
		name    string
		trigger string
		in      func(*CreateInput)
	}{
		{"unresolved blocked-by", "", func(in *CreateInput) { in.BlockedBy = []string{"lm-nosuch"} }},
		{"field audit", "BEFORE INSERT ON audit_log WHEN NEW.kind = '" + AuditKindField + "'", func(*CreateInput) {}},
		{"parent link", "BEFORE INSERT ON dependencies WHEN NEW.type = '" + ParentChildDepType + "'", func(in *CreateInput) { in.Parent = parent }},
		{"blocks link", "BEFORE INSERT ON dependencies WHEN NEW.type = '" + BlocksDepType + "'", func(in *CreateInput) { in.BlockedBy = []string{parent} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if c.trigger != "" {
				if _, err := tx.ExecContext(ctx, "CREATE TRIGGER fail_create "+c.trigger+" BEGIN SELECT RAISE(ABORT, 'injected'); END"); err != nil {
					t.Fatal(err)
				}
			}
			in := CreateInput{Title: "c", Priority: DefaultPriority, BeadType: DefaultBeadType, Namespace: "lm", Now: "2026-01-01T00:00:00.000Z"}
			c.in(&in)
			if _, err := CreateBead(ctx, tx, in); err == nil {
				t.Fatal("CreateBead succeeded, want error")
			}
		})
	}
}

func TestCheckParentAffiliatedQueryError(t *testing.T) {
	db := testDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	in := CreateInput{RequireMilestone: true}
	if err := checkCreateAffiliated(context.Background(), tx, in, "x", ""); err == nil {
		t.Fatal("checkCreateAffiliated on a closed tx = nil, want an error")
	}
}
