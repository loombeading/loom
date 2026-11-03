// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestDemandConditionsOrderSamePriority(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	now := stamp(t0, 100*time.Hour)
	due := t0.Add(400 * time.Hour)
	for _, mode := range []string{"off", "shadow"} {
		db := testDB(t)
		execSQL(t, db, `UPDATE coefficients SET value = ? WHERE key = 'time_adjust'`, mode)
		mk := func(name string, d time.Duration, in CreateInput) string {
			in.Title, in.Priority, in.BeadType, in.Namespace, in.Actor, in.Now = name, 2, "task", "lm", "u", stamp(t0, d)
			return mustCreate(t, db, in)
		}
		plain := mk("plain", 0, CreateInput{})
		epic := mk("epic", time.Minute, CreateInput{Labels: []string{"milestone:x"}})
		child := mk("child", 2*time.Minute, CreateInput{Parent: epic})
		keystone := mk("keystone", 3*time.Minute, CreateInput{})
		mk("down", 4*time.Minute, CreateInput{BlockedBy: []string{keystone}})
		dueBead := mk("due", 5*time.Minute, CreateInput{Due: &due, Clock: t0})
		redetected := mk("redetected", 6*time.Minute, CreateInput{})
		execSQL(t, db, `UPDATE beads SET last_redetected_at = ? WHERE id = ?`, stamp(t0, 99*time.Hour), redetected)

		res, err := ReadyList(t.Context(), db, ReadyFilter{Priority: -1, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		listed, _, _, err := ListBeadsEffective(t.Context(), db, ListFilter{Priority: -1, Sort: SortPriority, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		for name, beads := range map[string][]Bead{"ready": res.Beads, "list": listed} {
			var order []string
			for _, b := range beads {
				order = append(order, b.ID)
			}
			for label, id := range map[string]string{"milestone descendant": child, "blocks open downstream": keystone, "due_at": dueBead, "redetect window": redetected} {
				if i, j := slices.Index(order, id), slices.Index(order, plain); i < 0 || j < 0 || i > j {
					t.Errorf("time_adjust=%s %s: %s at %d, plain at %d, want demand ahead of the older plain", mode, name, label, i, j)
				}
			}
		}
	}
}

func TestDemandQueryFailuresSurface(t *testing.T) {
	db := testDB(t)
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i, typ := range []string{"task", "gate"} {
		mustCreate(t, db, CreateInput{Title: "b", Priority: 2, BeadType: typ, Namespace: "lm", Actor: "u", Now: stamp(t0, time.Duration(i)*time.Minute)})
	}
	now := stamp(t0, time.Hour)
	if _, err := CandidateDemand(t.Context(), db, "not-a-time", Coefficients{}, []Bead{{ID: "x"}}); err == nil {
		t.Error("CandidateDemand with an invalid now returned nil error")
	}
	calls := map[string]func(q Querier) error{
		"ready": func(q Querier) error {
			_, err := ReadyList(t.Context(), q, ReadyFilter{Priority: -1, Now: now})
			return err
		},
		"list": func(q Querier) error {
			_, _, _, err := ListBeadsEffective(t.Context(), q, ListFilter{Priority: -1, Sort: SortPriority, Now: now})
			return err
		},
		"gate": func(q Querier) error {
			_, _, err := GateListEffective(t.Context(), q, now)
			return err
		},
	}
	for name, call := range calls {
		failed := 0
		for n := 1; n <= 40; n++ {
			err := call(&nthCallErrQuerier{Querier: db, failOn: n})
			if err == nil {
				continue
			}
			if !errors.Is(err, errBoom) {
				t.Errorf("%s: failOn %d: err %v does not wrap the query failure", name, n, err)
			}
			failed++
		}
		if failed == 0 {
			t.Errorf("%s: no injected query failure surfaced", name)
		}
	}
}

func TestWithinLimitTiers(t *testing.T) {
	keys := []Bead{{ID: "a", Priority: 3}, {ID: "b", Priority: 2}, {ID: "c", Priority: 2}, {ID: "d", Priority: 4}}
	raised := map[string]EffectivePriority{"a": {Priority: 0}}
	ids := func(bs []Bead) []string {
		out := make([]string, 0, len(bs))
		for _, b := range bs {
			out = append(out, b.ID)
		}
		return out
	}
	for _, tc := range []struct {
		limit int
		want  []string
	}{
		{0, []string{"a", "b", "c", "d"}},
		{4, []string{"a", "b", "c", "d"}},
		{1, []string{"a"}},
		{2, []string{"a", "b", "c"}},
		{3, []string{"a", "b", "c"}},
	} {
		if got := ids(withinLimitTiers(raised, keys, tc.limit)); !slices.Equal(got, tc.want) {
			t.Errorf("limit %d: got %v, want %v", tc.limit, got, tc.want)
		}
	}
}

func TestGateListDemandFirstWithoutTimeAdjust(t *testing.T) {
	db := testDB(t)
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	mk := func(name, typ string, d time.Duration, in CreateInput) string {
		in.Title, in.Priority, in.BeadType, in.Namespace, in.Actor, in.Now = name, 2, typ, "lm", "u", stamp(t0, d)
		return mustCreate(t, db, in)
	}
	idle := mk("idle", "gate", 0, CreateInput{})
	busy := mk("busy", "gate", time.Minute, CreateInput{})
	mk("blocked", "task", 2*time.Minute, CreateInput{BlockedBy: []string{busy}})

	entries, _, err := GateListEffective(t.Context(), db, stamp(t0, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, len(entries))
	for _, e := range entries {
		order = append(order, e.ID)
	}
	if i, j := slices.Index(order, busy), slices.Index(order, idle); i < 0 || j < 0 || i > j {
		t.Errorf("gate order %v, want the gate blocking an open bead ahead of the older idle gate", order)
	}
}
