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

func mustUpdate(t *testing.T, db *sql.DB, in UpdateInput) error {
	t.Helper()
	_, err := mustUpdateWithWarnings(t, db, in)
	return err
}

func mustUpdateWithWarnings(t *testing.T, db *sql.DB, in UpdateInput) ([]string, error) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	warnings, err := UpdateBead(context.Background(), tx, in)
	if err != nil {
		_ = tx.Rollback()
		return warnings, err
	}
	return warnings, tx.Commit()
}

func createForUpdate(t *testing.T, db *sql.DB) string {
	t.Helper()
	return mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
}

func TestUpdateClaim(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)

	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "2026-01-01T00:01:00.000Z"}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusInProgress || b.ClaimedBy.String != "alice" {
		t.Errorf("bead = %+v, want status=in_progress claimed_by=alice", b)
	}
}

func TestUpdateClaimExclusiveToOtherActor(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "n1"}); err != nil {
		t.Fatal(err)
	}
	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "bob", Now: "n2"}); err == nil {
		t.Fatal("second claim by a different actor = nil error, want an error")
	}
}

func TestUpdateReleaseByOwnerSucceeds(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "n1"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Release: true, Actor: "alice", Now: "n2"}))

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusOpen || b.ClaimedBy.Valid {
		t.Errorf("bead = %+v, want status=open claimed_by=NULL", b)
	}
}

func TestUpdateReleaseByOtherActorRejectedWithoutForce(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "n1"}))
	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Release: true, Actor: "bob", Now: "n2"}); err == nil {
		t.Fatal("release by a different actor without --force = nil error, want an error")
	}
}

func TestUpdateForceReleaseRecordsForceRelease(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "n1"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Release: true, Force: true, Actor: "bob", Now: "n2"}))

	rows, err := AuditHistory(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Field.Valid && r.Field.String == FieldForceRelease {
			found = true
			if r.Actor != "bob" {
				t.Errorf("force_release actor = %q, want bob", r.Actor)
			}
		}
		if r.Field.Valid && r.Field.String == FieldRelease {
			t.Errorf("plain release field recorded for a forced release %+v", r)
		}
	}
	if !found {
		t.Error("no force_release audit record found")
	}
}

func TestUpdateReopenClearsClaimedByAndClosedAt(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "n1"}))
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseBead(context.Background(), tx, CloseInput{ID: id, Actor: "alice", Now: "n2"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Reopen: true, Actor: "carol", Now: "n3"}))

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusOpen {
		t.Errorf("status = %q, want open", b.Status)
	}
	if b.ClaimedBy.Valid {
		t.Errorf("claimed_by = %+v, want NULL", b.ClaimedBy)
	}
	if b.ClosedAt.Valid {
		t.Errorf("closed_at = %+v, want NULL", b.ClosedAt)
	}
}

func TestUpdateReasonOnlyDoesNotChangeUpdatedAt(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	before, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Reason: "handoff to carol", Actor: "alice", Now: "2026-01-01T00:05:00.000Z"}))

	after, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Errorf("updated_at changed from %q to %q for a reason-only update", before.UpdatedAt, after.UpdatedAt)
	}

	rows, err := AuditHistory(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	last := rows[len(rows)-1]
	if last.Kind != AuditKindNote || !last.Reason.Valid || last.Reason.String != "handoff to carol" {
		t.Errorf("last audit row = %+v, want kind=note reason=%q", last, "handoff to carol")
	}
}

func TestUpdateReasonOnlyWithoutReasonIsRejected(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Actor: "alice", Now: "n"}); err == nil {
		t.Fatal("update with no flags and no reason = nil error, want an error")
	}
}

func TestUpdateFieldEdits(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	title := "new title"
	priority := 1
	mustNoErr(t, mustUpdate(t, db, UpdateInput{ID: id, Title: title, Priority: priority, Actor: "alice", Now: "n2"}))

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != title || b.Priority != priority {
		t.Errorf("bead = %+v, want title=%q priority=%d", b, title, priority)
	}
	if b.UpdatedAt != "n2" {
		t.Errorf("updated_at = %q, want n2", b.UpdatedAt)
	}
}

func TestUpdateLabelsAddRemove(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, AddLabels: []string{"b", "a"}, Actor: "u", Now: "n1"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, AddLabels: []string{"c"}, RemoveLabels: []string{"a"}, Actor: "u", Now: "n2"}))

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Labels) != 2 || b.Labels[0] != "b" || b.Labels[1] != "c" {
		t.Errorf("labels = %v, want [b c]", b.Labels)
	}
}

func TestUpdateRejectsReleaseLabelOnAdd(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)

	var ve *ValidationError
	err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, AddLabels: []string{"release:v1"}, Title: "should not stick", Actor: "u", Now: "n1"})
	if !errors.As(err, &ve) || !strings.Contains(ve.Msg, "milestone:v1") {
		t.Fatalf("err = %v, want *ValidationError naming milestone:v1", err)
	}
	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "x" || len(b.Labels) != 0 {
		t.Errorf("bead = %+v, want unchanged (title=x, no labels)", b)
	}

	if _, err := db.ExecContext(context.Background(), `UPDATE beads SET labels = '["release:v1"]' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, RemoveLabels: []string{"release:v1"}, Actor: "u", Now: "n2"}))
	b, err = GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Labels) != 0 {
		t.Errorf("labels = %v, want none after --remove-label release:v1", b.Labels)
	}
}

func TestUpdateWaitDateOnTimedGateWarning(t *testing.T) {
	db := testDB(t)
	gateTimedTitle := mustCreate(t, db, CreateInput{Title: "23:00 に開く", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})
	gateTimedDesc := mustCreate(t, db, CreateInput{Title: "g", Description: "窓の終わりは 07:40", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})
	gateNoTime := mustCreate(t, db, CreateInput{Title: "g", Description: "日付だけ", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})
	nonGateTimed := mustCreate(t, db, CreateInput{Title: "23:00 に開く", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0"})

	for _, c := range []struct {
		name   string
		id     string
		labels []string
		want   []string
	}{
		{name: "件名に時刻・wait:date:", id: gateTimedTitle, labels: []string{"wait:date:2026-10-01"}, want: []string{"wait:date は日単位に丸める。窓の終わりの時刻を wait:after:<RFC3339> で付ける"}},
		{name: "本文に時刻・wait:date:", id: gateTimedDesc, labels: []string{"wait:date:2026-10-01"}, want: []string{"wait:date は日単位に丸める。窓の終わりの時刻を wait:after:<RFC3339> で付ける"}},
		{name: "時刻なし Gate・wait:date:", id: gateNoTime, labels: []string{"wait:date:2026-10-01"}, want: nil},
		{name: "非 Gate・時刻あり・wait:date:", id: nonGateTimed, labels: []string{"wait:date:2026-10-01"}, want: nil},
		{name: "時刻あり Gate・wait:date: 以外", id: gateTimedTitle, labels: []string{"wait:file:/tmp/x"}, want: nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			warnings, err := mustUpdateWithWarnings(t, db, UpdateInput{Priority: -1, ID: c.id, AddLabels: c.labels, Actor: "u", Now: "n1"})
			if err != nil {
				t.Fatalf("mustUpdateWithWarnings: %v", err)
			}
			if len(warnings) != len(c.want) {
				t.Fatalf("warnings=%v want=%v", warnings, c.want)
			}
			for i, w := range c.want {
				if warnings[i] != w {
					t.Errorf("warnings[%d]=%q want %q", i, warnings[i], w)
				}
			}
			b, err := GetBead(context.Background(), db, c.id)
			if err != nil {
				t.Fatal(err)
			}
			for _, l := range c.labels {
				found := false
				for _, bl := range b.Labels {
					if bl == l {
						found = true
					}
				}
				if !found {
					t.Errorf("label %q was not applied despite a warning-only path: labels=%v", l, b.Labels)
				}
			}
			mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: c.id, RemoveLabels: c.labels, Actor: "u", Now: "n2"}))
		})
	}
}

func TestUpdateOtherFieldEditsAndClear(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Description: "d", Namespace: "lm", Summary: "s", Depth: MinReasoningDepth, AddLabels: []string{"a"}, Actor: "u", Now: "n1"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Clear: []string{"description"}, RemoveLabels: []string{"a"}, Actor: "u", Now: "n2"}))

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Description.Valid || !b.Summary.Valid || b.Summary.String != "s" || len(b.Labels) != 0 {
		t.Errorf("bead = %+v, want description=NULL summary=s labels=[]", b)
	}
}

func TestUpdateRejectsInvalidInput(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	cases := map[string]UpdateInput{
		"not found":       {Priority: -1, ID: "lm-nosuch", Title: "t"},
		"priority":        {Priority: MaxPriority + 1, ID: id},
		"type":            {Priority: -1, ID: id, Type: "nosuch"},
		"namespace":       {Priority: -1, ID: id, Namespace: "Bad NS!"},
		"reasoning_depth": {Priority: -1, ID: id, Depth: MaxReasoningDepth + 1},
		"clear":           {Priority: -1, ID: id, Clear: []string{"title"}},
		"release open":    {Priority: -1, ID: id, Release: true},
		"reopen open":     {Priority: -1, ID: id, Reopen: true},
		"two transitions": {Priority: -1, ID: id, Claim: true, Release: true},
		"claim non-open":  {Priority: -1, ID: id, Claim: true},
	}
	for _, name := range []string{"not found", "priority", "type", "namespace", "reasoning_depth", "clear", "release open", "reopen open", "two transitions"} {
		in := cases[name]
		in.Actor, in.Now = "u", "n1"
		if err := mustUpdate(t, db, in); err == nil {
			t.Errorf("%s: err = nil, want an error", name)
		}
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseBead(context.Background(), tx, CloseInput{ID: id, Actor: "u", Now: "n2"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	in := cases["claim non-open"]
	in.Actor, in.Now = "u", "n3"
	var verr *ValidationError
	if err := mustUpdate(t, db, in); !errors.As(err, &verr) {
		t.Errorf("claim of a closed Bead: err = %v, want *ValidationError", err)
	}
}

func TestUpdateRejectsInvalidTransition(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CloseBead(context.Background(), tx, CloseInput{ID: id, Actor: "u", Now: "n1"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Cancel: true, Actor: "u", Now: "n2"}); err == nil {
		t.Fatal("cancelling a closed Bead = nil error, want an error")
	}
}

func TestCloseRejectsGate(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := CloseBead(context.Background(), tx, CloseInput{ID: id, Actor: "u", Now: "n1"}); err == nil {
		t.Fatal("closing a gate Bead = nil error, want an error")
	}
}

func TestUpdateTypeRejectsGateConversion(t *testing.T) {
	db := testDB(t)
	gate := mustCreate(t, db, CreateInput{Title: "g", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: "n0"})
	open := createForUpdate(t, db)
	claimed := createForUpdate(t, db)
	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: claimed, Claim: true, Actor: "alice", Now: "n1"}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, id, typ string
	}{
		{"gate to task", gate, "task"},
		{"open task to gate", open, "gate"},
		{"in_progress task to gate", claimed, "gate"},
	} {
		err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: tc.id, Type: tc.typ, Actor: "u", Now: "n2"})
		if _, ok := errors.AsType[*ValidationError](err); !ok {
			t.Errorf("%s: err = %v, want *ValidationError", tc.name, err)
		}
		b, err := GetBead(context.Background(), db, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if b.BeadType == tc.typ {
			t.Errorf("%s: type changed to %q", tc.name, b.BeadType)
		}
	}

	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: gate, Type: "gate", Actor: "u", Now: "n3"}); err != nil {
		t.Errorf("gate to gate: %v", err)
	}
	if err := mustUpdate(t, db, UpdateInput{Priority: -1, ID: open, Type: "task", Actor: "u", Now: "n3"}); err != nil {
		t.Errorf("task to task: %v", err)
	}
}

func TestUpdateExternalRefsAddRemove(t *testing.T) {
	db := testDB(t)
	id := createForUpdate(t, db)

	ref1 := "https://github.com/example/repo/pull/1"
	ref2 := "https://github.com/example/repo/pull/2"
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, AddExternalRefs: []string{ref1}, Actor: "u", Now: "n1"}))

	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.ExternalRefs) != 1 || b.ExternalRefs[0] != ref1 {
		t.Errorf("ExternalRefs = %+v, want [%q]", b.ExternalRefs, ref1)
	}
	var externalRefsAt sql.NullString
	if err := db.QueryRowContext(t.Context(), `SELECT external_refs_set_at FROM beads WHERE id = ?`, id).Scan(&externalRefsAt); err != nil {
		t.Fatal(err)
	}
	if externalRefsAt.String != "n1" {
		t.Errorf("external_refs_set_at = %+v, want n1", externalRefsAt)
	}

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, AddExternalRefs: []string{ref2}, Actor: "u", Now: "n2"}))
	b, err = GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.ExternalRefs) != 2 || b.ExternalRefs[0] != ref1 || b.ExternalRefs[1] != ref2 {
		t.Errorf("ExternalRefs after add = %+v, want [%q %q]", b.ExternalRefs, ref1, ref2)
	}

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, RemoveExternalRefs: []string{ref1}, Actor: "u", Now: "n3"}))
	b, err = GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.ExternalRefs) != 1 || b.ExternalRefs[0] != ref2 {
		t.Errorf("ExternalRefs after remove = %+v, want [%q]", b.ExternalRefs, ref2)
	}

	rows, err := AuditHistory(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range rows {
		if r.Field.Valid && r.Field.String == "external_refs" {
			count++
		}
	}
	if count != 3 {
		t.Errorf("external_refs audit records = %d, want 3 (add + add + remove)", count)
	}
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
