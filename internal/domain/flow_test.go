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

const (
	flowT1 = "2026-10-10T01:00:00.000Z"
	flowT2 = "2026-10-10T02:00:00.000Z"
	flowT3 = "2026-10-10T03:00:00.000Z"
	flowT4 = "2026-10-10T04:00:00.000Z"
	flowT5 = "2026-10-10T05:00:00.000Z"
	flowT6 = "2026-10-10T06:00:00.000Z"
)

func flowTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func flowWindow(t *testing.T) (time.Time, time.Time) {
	t.Helper()
	return flowTime(t, "2026-10-09T00:00:00.000Z"), flowTime(t, "2026-10-11T00:00:00.000Z")
}

func flowByID(recs []FlowRecord) map[string]FlowRecord {
	m := map[string]FlowRecord{}
	for _, r := range recs {
		m[r.ID] = r
	}
	return m
}

func TestFlowRecordsRemovedBlockerLinkIsReadyEntry(t *testing.T) {
	db := testDB(t)
	blocker := mustCreate(t, db, CreateInput{Title: "blocker", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	target := mustCreate(t, db, CreateInput{Title: "target", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	mustNoErr(t, mustAddLinkAt(t, db, target, blocker, BlocksDepType, flowT1))
	if _, err := db.ExecContext(t.Context(), `UPDATE dependencies SET removed = 1, removed_set_at = ? WHERE bead_id = ?`, flowT3, target); err != nil {
		t.Fatal(err)
	}
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Actor: "alice", Now: flowT4}))
	mustNoErr(t, mustCloseBead(t, db, target, flowT5))
	mustNoErr(t, mustCloseBead(t, db, blocker, flowT6))

	from, to := flowWindow(t)
	got, err := FlowRecords(context.Background(), db, from, to, "lm")
	if err != nil {
		t.Fatal(err)
	}
	r := flowByID(got)[target]
	if !r.ReadyAt.Equal(flowTime(t, flowT3)) {
		t.Errorf("ReadyAt = %v, want the link removal time (the later blocker close is after the claim)", r.ReadyAt)
	}
	if !r.ClaimedAt.Equal(flowTime(t, flowT4)) {
		t.Errorf("ClaimedAt = %v, want t4", r.ClaimedAt)
	}
}

func TestFlowRecordsIgnoresDanglingBlocker(t *testing.T) {
	db := testDB(t)
	target := mustCreate(t, db, CreateInput{Title: "target", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	if _, err := db.ExecContext(t.Context(), `INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed) VALUES (?, ?, ?, ?, 0)`,
		target, "ghost", BlocksDepType, flowT1); err != nil {
		t.Fatal(err)
	}
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Actor: "alice", Now: flowT2}))
	mustNoErr(t, mustCloseBead(t, db, target, flowT3))

	from, to := flowWindow(t)
	got, err := FlowRecords(context.Background(), db, from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	r := flowByID(got)[target]
	if !r.ReadyAt.Equal(flowTime(t, flowT1)) {
		t.Errorf("ReadyAt = %v, want created_at (a missing blocker is ignored)", r.ReadyAt)
	}
}

func TestFlowRecordsExcludesCancelled(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET status = 'cancelled', closed_at = ? WHERE id = ?`, flowT2, id); err != nil {
		t.Fatal(err)
	}
	from, to := flowWindow(t)
	got, err := FlowRecords(context.Background(), db, from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want none (cancelled is not closed)", got)
	}
}

func TestFlowRecordsWindowBounds(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	mustNoErr(t, mustCloseBead(t, db, id, flowT2))

	at := flowTime(t, flowT2)
	if got, err := FlowRecords(context.Background(), db, at, at.Add(time.Hour), ""); err != nil || len(got) != 0 {
		t.Errorf("from == closed_at: got %+v, err %v, want none", got, err)
	}
	if got, err := FlowRecords(context.Background(), db, at.Add(-time.Hour), at, ""); err != nil || len(got) != 1 {
		t.Errorf("to == closed_at: got %+v, err %v, want one", got, err)
	}
}

type failingFlowQuerier struct {
	Querier

	failOn int
	calls  int
}

func (q *failingFlowQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.calls++
	if q.calls == q.failOn {
		return nil, errors.New("forced")
	}
	return q.Querier.QueryContext(ctx, query, args...)
}

func TestFlowRecordsQueryErrorsAreWrapped(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	mustNoErr(t, mustCloseBead(t, db, id, flowT2))
	from, to := flowWindow(t)

	for failOn, want := range map[int]string{1: "flow records:", 2: "flow records: ready candidates:"} {
		_, err := FlowRecords(context.Background(), &failingFlowQuerier{Querier: db, failOn: failOn}, from, to, "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("failOn %d: err = %v, want it to contain %q", failOn, err, want)
		}
	}
}

func TestFlowRecordsRejectsUnparseableTimes(t *testing.T) {
	from, to := flowWindow(t)
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	mustNoErr(t, mustCloseBead(t, db, id, flowT2))
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET created_at = 'bad' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := FlowRecords(context.Background(), db, from, to, ""); err == nil {
		t.Error("created_at = bad: want an error")
	}

	db = testDB(t)
	id = mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: flowT2}))
	mustNoErr(t, mustCloseBead(t, db, id, flowT3))
	if _, err := db.ExecContext(t.Context(), `UPDATE audit_log SET occurred_at = 'bad' WHERE bead_id = ? AND field = 'claim'`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := FlowRecords(context.Background(), db, from, to, ""); err == nil {
		t.Error("claim time = bad: want an error")
	}

	db = testDB(t)
	blocker := mustCreate(t, db, CreateInput{Title: "b", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	id = mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: flowT1})
	mustNoErr(t, mustAddLinkAt(t, db, id, blocker, BlocksDepType, flowT1))
	mustNoErr(t, mustCloseBead(t, db, blocker, flowT2))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: flowT3}))
	mustNoErr(t, mustCloseBead(t, db, id, flowT4))
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET closed_at = 'bad' WHERE id = ?`, blocker); err != nil {
		t.Fatal(err)
	}
	if _, err := FlowRecords(context.Background(), db, from, to, ""); err == nil {
		t.Error("blocker closed_at = bad: want an error")
	}
}
