// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"
)

func createDue(t *testing.T, db *sql.DB, t0 time.Time, title string, priority int, dueIn time.Duration) string {
	t.Helper()
	due := t0.Add(dueIn)
	return mustCreate(t, db, CreateInput{
		Title: title, BeadType: "task", Namespace: "lm", Actor: "u", Priority: priority,
		Now: stamp(t0, 0), Due: &due, Clock: t0,
	})
}

func TestRaisedPrioritiesDueLead(t *testing.T) {
	db := testDB(t)
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	now := stamp(t0, 0)
	near := createDue(t, db, t0, "near", 3, 24*time.Hour)
	far := createDue(t, db, t0, "far", 3, 100*time.Hour)
	edge := createDue(t, db, t0, "edge", 3, 72*time.Hour)
	p1 := createDue(t, db, t0, "p1", 1, 24*time.Hour)
	prereq := mustCreate(t, db, CreateInput{Title: "prereq", BeadType: "task", Namespace: "lm", Actor: "u", Priority: 3, Now: stamp(t0, 0)})
	child := mustCreate(t, db, CreateInput{Title: "child", BeadType: "task", Namespace: "lm", Actor: "u", Priority: 3, Now: stamp(t0, time.Second)})
	mustLink(t, db, near, prereq, BlocksDepType)
	mustLink(t, db, child, near, ParentChildDepType)
	span := time.Hour
	both := createDue(t, db, t0, "both", 3, 24*time.Hour)
	execSQL(t, db, `UPDATE beads SET expedite_until = ?, expedite_reason = 'x' WHERE id = ?`, stamp(t0, span), both)
	dueUnderExpedite := createDue(t, db, t0, "due-under-expedite", 3, 24*time.Hour)
	expeditedDownstream := mustCreate(t, db, CreateInput{Title: "expedited-downstream", BeadType: "task", Namespace: "lm", Actor: "u", Priority: 3, Now: stamp(t0, 2*time.Second)})
	execSQL(t, db, `UPDATE beads SET expedite_until = ?, expedite_reason = 'x' WHERE id = ?`, stamp(t0, span), expeditedDownstream)
	mustLink(t, db, expeditedDownstream, dueUnderExpedite, BlocksDepType)

	raised, err := RaisedPriorities(t.Context(), db, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := FillSources(t.Context(), db, now, raised, []string{prereq, child, dueUnderExpedite}); err != nil {
		t.Fatal(err)
	}
	if got, want := raised[near], (EffectivePriority{Priority: 1, Source: "due"}); got != want {
		t.Errorf("eff(near) = %+v, want %+v", got, want)
	}
	if got, want := raised[edge], (EffectivePriority{Priority: 1, Source: "due"}); got != want {
		t.Errorf("eff(edge) = %+v, want %+v", got, want)
	}
	if e, ok := raised[far]; ok {
		t.Errorf("eff(far) = %+v, want not raised", e)
	}
	if e, ok := raised[p1]; ok {
		t.Errorf("eff(p1) = %+v, want not raised (stored P1)", e)
	}
	if got, want := raised[prereq], (EffectivePriority{Priority: 1, Source: near}); got != want {
		t.Errorf("eff(prereq) = %+v, want %+v", got, want)
	}
	if got, want := raised[child], (EffectivePriority{Priority: 1, Source: near}); got != want {
		t.Errorf("eff(child) = %+v, want %+v", got, want)
	}
	if got, want := raised[both], (EffectivePriority{Priority: 0, Source: "expedite"}); got != want {
		t.Errorf("eff(both) = %+v, want %+v", got, want)
	}
	if got, want := raised[dueUnderExpedite], (EffectivePriority{Priority: 0, Source: expeditedDownstream}); got != want {
		t.Errorf("eff(dueUnderExpedite) = %+v, want %+v", got, want)
	}
	if err := FillSources(t.Context(), db, "not-a-time", raised, []string{prereq}); err == nil {
		t.Error("FillSources with an unparsable now returned nil")
	}

	for id, want := range map[string]EffectivePriority{
		near:   {Priority: 1, Source: "due"},
		prereq: {Priority: 1, Source: near},
		child:  {Priority: 1, Source: near},
		both:   {Priority: 0, Source: "expedite"},
	} {
		got, ok, err := EffectivePriorityOf(t.Context(), db, now, id)
		if err != nil || !ok || got != want {
			t.Errorf("EffectivePriorityOf(%s) = %+v, %v, %v, want %+v", id, got, ok, err, want)
		}
	}
	if got, ok, err := EffectivePriorityOf(t.Context(), db, now, far); err != nil || ok || got.Priority != 3 {
		t.Errorf("EffectivePriorityOf(far) = %+v, %v, %v, want stored P3 not raised", got, ok, err)
	}

	later := stamp(t0, 30*time.Hour)
	raised, err = RaisedPriorities(t.Context(), db, later)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := raised[far], (EffectivePriority{Priority: 1, Source: "due"}); got != want {
		t.Errorf("at +30h eff(far) = %+v, want %+v", got, want)
	}
}

func TestRaisedPrioritiesDueLeadFollowsCoefficient(t *testing.T) {
	db := testDB(t)
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	id := createDue(t, db, t0, "x", 3, 100*time.Hour)
	if raised, err := RaisedPriorities(t.Context(), db, stamp(t0, 0)); err != nil || len(raised) != 0 {
		t.Fatalf("default lead: raised = %v, err = %v, want none", raised, err)
	}
	setCoefficients(t, db, map[string]string{"due_lead": "120h"})
	raised, err := RaisedPriorities(t.Context(), db, stamp(t0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := raised[id], (EffectivePriority{Priority: 1, Source: "due"}); got != want {
		t.Errorf("eff = %+v, want %+v", got, want)
	}
}

func TestListSortPriorityTimeAdjust(t *testing.T) {
	f := newAdjustFixture(t)
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	soon := createDue(t, f.db, t0, "soon", 4, 100*time.Hour)
	execSQL(t, f.db, `UPDATE beads SET severity = 1 WHERE id = ?`, soon)
	f.ids["soon"] = soon

	list := func() ([]string, map[string]EffectivePriority) {
		beads, eff, _, err := ListBeadsEffective(t.Context(), f.db, ListFilter{Priority: -1, Sort: SortPriority, Now: f.now})
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(beads))
		for i, b := range beads {
			ids[i] = b.ID
		}
		return ids, eff
	}
	names := func(ids []string) []string {
		byID := map[string]string{}
		for name, id := range f.ids {
			byID[id] = name
		}
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = byID[id]
		}
		return out
	}

	execSQL(t, f.db, `UPDATE coefficients SET value = 'off' WHERE key = 'time_adjust'`)
	offIDs, offEff := list()
	execSQL(t, f.db, `UPDATE coefficients SET value = 'shadow' WHERE key = 'time_adjust'`)
	shadowIDs, shadowEff := list()
	if !slices.Equal(offIDs, shadowIDs) {
		t.Errorf("shadow order %v differs from off order %v", names(shadowIDs), names(offIDs))
	}
	for _, eff := range []map[string]EffectivePriority{offEff, shadowEff} {
		for id, e := range eff {
			if e.Source == SourceAged || e.Source == SourceDecayed {
				t.Errorf("eff(%s) = %+v carries a time-adjusted source outside on", id, e)
			}
		}
		if got, want := eff[soon], (EffectivePriority{Priority: 1, Source: "due"}); got != want {
			t.Errorf("eff(soon) = %+v, want %+v in every mode", got, want)
		}
	}

	execSQL(t, f.db, `UPDATE coefficients SET value = 'on' WHERE key = 'time_adjust'`)
	onIDs, onEff := list()
	want := map[string]EffectivePriority{
		"aged":            {Priority: 2, Source: SourceAged},
		"milestone-child": {Priority: 2, Source: SourceAged},
		"redetected":      {Priority: 2, Source: SourceAged},
		"decayed":         {Priority: 4, Source: SourceDecayed},
		"soon":            {Priority: 1, Source: "due"},
	}
	for name, e := range want {
		if got := onEff[f.ids[name]]; got != e {
			t.Errorf("on eff(%s) = %+v, want %+v", name, got, e)
		}
	}
	if !slices.Contains(onIDs, f.ids["held"]) || !slices.Contains(onIDs, f.ids["epic"]) {
		t.Errorf("on order %v drops a held or parent Bead that lm list shows", names(onIDs))
	}

	ready := f.list(t, true)
	var onReady []string
	for _, id := range onIDs {
		if slices.ContainsFunc(ready.Beads, func(b Bead) bool { return b.ID == id }) {
			onReady = append(onReady, id)
		}
	}
	readyOrder := make([]string, 0, len(ready.Beads))
	for _, b := range ready.Beads {
		readyOrder = append(readyOrder, b.ID)
	}
	if !slices.Equal(onReady, readyOrder) {
		t.Errorf("on list order %v differs from on lm ready order %v", names(onReady), names(readyOrder))
	}

	sawErr := false
	for failOn := 1; ; failOn++ {
		_, _, _, err := ListBeadsEffective(t.Context(), &nthCallErrQuerier{Querier: f.db, failOn: failOn}, ListFilter{Priority: -1, Sort: SortPriority, Now: f.now})
		if err == nil {
			break
		}
		sawErr = true
		if !errors.Is(err, errBoom) {
			t.Errorf("failOn=%d: err = %v, want errBoom", failOn, err)
		}
	}
	if !sawErr {
		t.Error("no query failure surfaced")
	}

	limited, _, total, err := ListBeadsEffective(t.Context(), f.db, ListFilter{Priority: -1, Sort: SortPriority, Now: f.now, Limit: 2})
	if err != nil || len(limited) != 2 || total != len(onIDs) || limited[0].ID != onIDs[0] || limited[1].ID != onIDs[1] {
		t.Errorf("limited = %d beads total %d err %v, want first two of %d", len(limited), total, err, len(onIDs))
	}
}
