// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func beadAt(t *testing.T, db *sql.DB, priority int, now string) string {
	t.Helper()
	in := CreateInput{Title: "x", Priority: priority, BeadType: "task", Namespace: "lm", Actor: "u", Now: now}
	if priority == 0 {
		span := time.Hour
		in.Priority, in.ExpediteFor, in.Reason = 1, &span, "test interrupt"
	}
	return mustCreate(t, db, in)
}

func mustLink(t *testing.T, db *sql.DB, beadID, dependsOnID, depType string) {
	t.Helper()
	if err := mustAddLink(t, db, beadID, dependsOnID, depType); err != nil {
		t.Fatal(err)
	}
}

func effOf(t *testing.T, db *sql.DB, id string) EffectivePriority {
	t.Helper()
	raised, err := RaisedPriorities(context.Background(), db, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := FillSources(context.Background(), db, "", raised, []string{id}); err != nil {
		t.Fatal(err)
	}
	if e, ok := raised[id]; ok {
		return e
	}
	b, err := GetBead(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	return EffectivePriority{Priority: b.Priority}
}

func TestReadyOrdersBlockerOfP0BeforeP1(t *testing.T) {
	db := testDB(t)
	p1 := beadAt(t, db, 1, "n0")
	blocker := beadAt(t, db, 2, "n1")
	p0 := beadAt(t, db, 0, "n2")
	mustLink(t, db, p0, blocker, BlocksDepType)

	got := readyIDs(t, db)
	if want := []string{blocker, p1}; !slices.Equal(got, want) {
		t.Fatalf("ready = %v, want %v", got, want)
	}
	if e := effOf(t, db, blocker); e.Priority != 0 || e.Source != p0 {
		t.Errorf("eff(blocker) = %+v, want P0 from %s", e, p0)
	}
	if e := effOf(t, db, p1); e.Priority != 1 || e.Source != "" {
		t.Errorf("eff(p1) = %+v, want its stored P1 without source", e)
	}
}

func TestReadyLimitAppliesAfterEffectiveOrder(t *testing.T) {
	db := testDB(t)
	_ = beadAt(t, db, 1, "n0")
	blocker := beadAt(t, db, 3, "n1")
	p0 := beadAt(t, db, 0, "n2")
	mustLink(t, db, p0, blocker, BlocksDepType)

	beads, total, err := ReadyBeads(context.Background(), db, ReadyFilter{Priority: -1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(beads) != 1 || beads[0].ID != blocker {
		t.Fatalf("ready(limit 1) = %v total %d, want [%s] total 2", beads, total, blocker)
	}
}

func TestEffectivePriorityRevertsWhenDownstreamEnds(t *testing.T) {
	cases := map[string]func(t *testing.T, db *sql.DB, down, blocker string){
		"closed": func(t *testing.T, db *sql.DB, down, _ string) {
			t.Helper()
			if err := mustCloseBead(t, db, down, "n9"); err != nil {
				t.Fatal(err)
			}
		},
		"cancelled": func(t *testing.T, db *sql.DB, down, _ string) {
			t.Helper()
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := UpdateBead(context.Background(), tx, UpdateInput{ID: down, Cancel: true, Priority: -1, Actor: "u", Now: "n9"}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		},
		"removed": func(t *testing.T, db *sql.DB, down, blocker string) {
			t.Helper()
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := RemoveLink(context.Background(), tx, "u", down, blocker, BlocksDepType, "n9", ""); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, end := range cases {
		t.Run(name, func(t *testing.T) {
			db := testDB(t)
			blocker := beadAt(t, db, 2, "n0")
			down := beadAt(t, db, 0, "n1")
			mustLink(t, db, down, blocker, BlocksDepType)
			if e := effOf(t, db, blocker); e.Priority != 0 {
				t.Fatalf("eff before = %+v, want P0", e)
			}
			end(t, db, down, blocker)
			if e := effOf(t, db, blocker); e.Priority != 2 || e.Source != "" {
				t.Errorf("eff after = %+v, want stored P2 without source", e)
			}
		})
	}
}

func TestEffectivePriorityLeavesStoredPriority(t *testing.T) {
	db := testDB(t)
	blocker := beadAt(t, db, 2, "n0")
	down := beadAt(t, db, 0, "n1")
	mustLink(t, db, down, blocker, BlocksDepType)
	_ = readyIDs(t, db)

	b, err := GetBead(context.Background(), db, blocker)
	if err != nil {
		t.Fatal(err)
	}
	if b.Priority != 2 {
		t.Errorf("stored priority = %d, want 2", b.Priority)
	}
}

func TestEffectivePriorityTransitive(t *testing.T) {
	db := testDB(t)
	b := beadAt(t, db, 3, "n0")
	a := beadAt(t, db, 2, "n1")
	p0 := beadAt(t, db, 0, "n2")
	mustLink(t, db, a, b, BlocksDepType)
	mustLink(t, db, p0, a, BlocksDepType)
	if e := effOf(t, db, b); e.Priority != 0 || e.Source != p0 {
		t.Errorf("eff(B) = %+v, want P0 from %s", e, p0)
	}

	parent := beadAt(t, db, 1, "n3")
	child := beadAt(t, db, 4, "n4")
	pre := beadAt(t, db, 4, "n5")
	mustLink(t, db, child, parent, ParentChildDepType)
	mustLink(t, db, child, pre, BlocksDepType)
	if e := effOf(t, db, child); e.Priority != 1 || e.Source != parent {
		t.Errorf("eff(child) = %+v, want P1 from %s", e, parent)
	}
	if e := effOf(t, db, pre); e.Priority != 1 || e.Source != parent {
		t.Errorf("eff(pre) = %+v, want P1 from %s (blocks then parent-child)", e, parent)
	}
}

func TestEffectivePrioritySourceIsEarliestAndCyclesTerminate(t *testing.T) {
	db := testDB(t)
	blocker := beadAt(t, db, 3, "n0")
	late := beadAt(t, db, 0, "n2")
	early := beadAt(t, db, 0, "n1")
	mid := beadAt(t, db, 1, "n3")
	mustLink(t, db, late, blocker, BlocksDepType)
	mustLink(t, db, mid, blocker, BlocksDepType)
	mustLink(t, db, early, mid, BlocksDepType)
	if e := effOf(t, db, blocker); e.Priority != 0 || e.Source != early {
		t.Errorf("eff = %+v, want P0 from the earliest %s", e, early)
	}

	x := beadAt(t, db, 2, "n4")
	y := beadAt(t, db, 2, "n5")
	if _, err := db.ExecContext(t.Context(), `INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed) VALUES (?, ?, ?, 'n6', 0), (?, ?, ?, 'n6', 0)`,
		x, y, BlocksDepType, y, x, BlocksDepType); err != nil {
		t.Fatal(err)
	}
	if e := effOf(t, db, x); e.Priority != 2 || e.Source != "" {
		t.Errorf("eff(cycle) = %+v, want stored P2", e)
	}
}

func TestRaisedPrioritiesSkipsTerminalAndPropagatesErrors(t *testing.T) {
	db := testDB(t)
	blocker := beadAt(t, db, 2, "n0")
	down := beadAt(t, db, 0, "n1")
	mustLink(t, db, down, blocker, BlocksDepType)
	if err := mustCloseBead(t, db, blocker, "n2"); err != nil {
		t.Fatal(err)
	}
	raised, err := RaisedPriorities(context.Background(), db, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raised[blocker]; ok || len(raised) != 1 || raised[down].Source != SourceExpedite {
		t.Errorf("raised = %+v, want only the expedited Bead itself once the blocker is closed", raised)
	}

	q := &nthCallErrQuerier{Querier: db, failOn: 1}
	if _, err := RaisedPriorities(context.Background(), q, ""); !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
	sub := substituteQuerier{DB: db, match: "SELECT id, MIN(p) FROM lend", sub: "SELECT 'a', 'not a number'"}
	if _, err := RaisedPriorities(context.Background(), sub, ""); err == nil {
		t.Error("err = nil, want a scan error")
	}
	missing := map[string]EffectivePriority{"none": {Priority: 0}}
	if err := FillSources(context.Background(), db, "", missing, []string{"none"}); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("FillSources on an unknown Bead: err = %v, want sql.ErrNoRows", err)
	}

	fresh := testDB(t)
	b2 := beadAt(t, fresh, 2, "n0")
	d2 := beadAt(t, fresh, 0, "n1")
	mustLink(t, fresh, d2, b2, BlocksDepType)
	for failOn := 2; failOn <= 3; failOn++ {
		q := &nthCallErrQuerier{Querier: fresh, failOn: failOn}
		if _, _, err := ReadyBeads(context.Background(), q, ReadyFilter{Priority: -1}); !errors.Is(err, errBoom) {
			t.Errorf("ReadyBeads failOn=%d: err = %v, want errBoom", failOn, err)
		}
	}
}

type substituteQuerier struct {
	*sql.DB

	match, sub string
}

func (q substituteQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, q.match) {
		return q.DB.QueryContext(ctx, q.sub)
	}
	return q.DB.QueryContext(ctx, query, args...)
}

func TestEffectivePrioritySourceTieBreaksOnID(t *testing.T) {
	db := testDB(t)
	blocker := beadAt(t, db, 3, "n0")
	a := beadAt(t, db, 0, "n1")
	b := beadAt(t, db, 0, "n1")
	mustLink(t, db, a, blocker, BlocksDepType)
	mustLink(t, db, b, blocker, BlocksDepType)
	want := min(a, b)
	if e := effOf(t, db, blocker); e.Source != want {
		t.Errorf("eff = %+v, want source %s (smaller canonical ID)", e, want)
	}
}

type rowSubstituteQuerier struct {
	*sql.DB

	match, sub string
}

func (q rowSubstituteQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, q.match) {
		return q.DB.QueryRowContext(ctx, q.sub)
	}
	return q.DB.QueryRowContext(ctx, query, args...)
}

func TestEffectivePriorityOf(t *testing.T) {
	db := testDB(t)
	blocker := beadAt(t, db, 2, "n0")
	down := beadAt(t, db, 0, "n1")
	mustLink(t, db, down, blocker, BlocksDepType)
	ctx := context.Background()

	e, raised, err := EffectivePriorityOf(ctx, db, "", blocker)
	if err != nil || !raised || e != (EffectivePriority{Priority: 0, Source: down}) {
		t.Errorf("EffectivePriorityOf(blocker) = %+v, %v, %v; want P0 from %s", e, raised, err, down)
	}
	e, raised, err = EffectivePriorityOf(ctx, db, "", down)
	if err != nil || !raised || e != (EffectivePriority{Priority: 0, Source: SourceExpedite}) {
		t.Errorf("EffectivePriorityOf(down) = %+v, %v, %v; want its own P0 from expedite", e, raised, err)
	}
	if _, _, err := EffectivePriorityOf(ctx, db, "", "none"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown Bead: err = %v, want sql.ErrNoRows", err)
	}
	sub := rowSubstituteQuerier{DB: db, match: "ORDER BY i.priority ASC", sub: "SELECT 'x'"}
	if _, _, err := EffectivePriorityOf(ctx, sub, "", blocker); err == nil {
		t.Error("err = nil, want a scan error from the reach query")
	}
}

func TestEffectiveListingsPropagateErrors(t *testing.T) {
	db := testDB(t)
	blocker := beadAt(t, db, 2, "n0")
	down := beadAt(t, db, 0, "n1")
	mustLink(t, db, down, blocker, BlocksDepType)
	ctx := context.Background()

	for failOn := 1; failOn <= 2; failOn++ {
		q := &nthCallErrQuerier{Querier: db, failOn: failOn}
		if _, _, _, err := ListBeadsEffective(ctx, q, ListFilter{Priority: -1, Sort: SortPriority}); !errors.Is(err, errBoom) {
			t.Errorf("ListBeadsEffective failOn=%d: err = %v, want errBoom", failOn, err)
		}
	}
	if _, _, err := GateListEffective(ctx, &nthCallErrQuerier{Querier: db, failOn: 1}, ""); !errors.Is(err, errBoom) {
		t.Errorf("GateListEffective: err = %v, want errBoom", err)
	}
	sawErr := false
	for failOn := 1; ; failOn++ {
		q := &nthCallErrQuerier{Querier: db, failOn: failOn}
		_, _, _, err := BlockedBeadsEffective(ctx, q, ReadyFilter{Priority: -1})
		if err == nil {
			break
		}
		sawErr = true
		if !errors.Is(err, errBoom) {
			t.Errorf("BlockedBeadsEffective failOn=%d: err = %v, want errBoom", failOn, err)
		}
	}
	if !sawErr {
		t.Error("BlockedBeadsEffective: no failOn value produced errBoom")
	}
}
