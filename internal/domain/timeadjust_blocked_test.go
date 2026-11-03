// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func (f *adjustFixture) blocked(t *testing.T, limit int) ([]BlockedBead, map[string]EffectivePriority, int) {
	t.Helper()
	rows, eff, total, err := BlockedBeadsEffective(t.Context(), f.db, ReadyFilter{Priority: -1, Now: f.now, Limit: limit})
	if err != nil {
		t.Fatal(err)
	}
	return rows, eff, total
}

func TestBlockedHeldRowsByTimeAdjust(t *testing.T) {
	f := newAdjustFixture(t)
	held := f.ids["held"]
	for _, mode := range []string{"off", "shadow"} {
		execSQL(t, f.db, `UPDATE coefficients SET value = ? WHERE key = 'time_adjust'`, mode)
		rows, _, total := f.blocked(t, 0)
		for _, r := range rows {
			if r.HeldSince != "" || r.ID == held {
				t.Errorf("time_adjust=%s: row %s (held since %q) must not be a held row", mode, r.ID, r.HeldSince)
			}
		}
		if total != len(rows) {
			t.Errorf("time_adjust=%s: total = %d, rows = %d", mode, total, len(rows))
		}
	}

	execSQL(t, f.db, `UPDATE coefficients SET value = 'on' WHERE key = 'time_adjust'`)
	rows, eff, total := f.blocked(t, 0)
	var got *BlockedBead
	for i := range rows {
		if rows[i].ID == held {
			got = &rows[i]
		}
	}
	if got == nil {
		t.Fatalf("on: rows = %+v, want the held Bead %s", rows, held)
	}
	if got.HeldSince != "2026-06-01T00:00:00.000Z" || len(got.Blockers) != 0 {
		t.Errorf("held row: since = %q, blockers = %v, want 2026-06-01T00:00:00.000Z and none", got.HeldSince, got.Blockers)
	}
	if total != len(rows) {
		t.Errorf("on: total = %d, rows = %d", total, len(rows))
	}
	if e := eff[f.ids["decayed"]]; e.Source != SourceDecayed {
		t.Errorf("on: eff(decayed) = %+v, want the time-adjusted source carried to the blocked list", e)
	}
	for _, r := range rows {
		if r.ID == f.ids["downstream"] && (r.HeldSince != "" || len(r.Blockers) == 0) {
			t.Errorf("downstream row = %+v, want a blocked-by row", r)
		}
	}

	limited, _, limitedTotal := f.blocked(t, 1)
	if len(limited) != 1 || limitedTotal != total {
		t.Errorf("limit 1: rows = %d, total = %d, want 1 and %d", len(limited), limitedTotal, total)
	}
	again, _, _ := f.blocked(t, 0)
	if !slices.EqualFunc(rows, again, func(a, b BlockedBead) bool { return a.ID == b.ID && a.HeldSince == b.HeldSince }) {
		t.Errorf("same now, different rows: %v vs %v", rows, again)
	}
}

func TestGateListTimeAdjusted(t *testing.T) {
	db := testDB(t)
	setCoefficients(t, db, map[string]string{
		"promote_after": "1000h", "promote_after_gate": "24h", "decay_after": "1000h", "hold_after": "1000h",
		"severity_3_ceiling": "1", "severity_3_floor": "4",
	})
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	created := stamp(t0, 0)
	now := stamp(t0, 48*time.Hour)
	target := func() string {
		return mustCreate(t, db, CreateInput{Title: "target", Priority: 4, BeadType: "task", Namespace: "lm", Actor: "u", Now: created})
	}
	aging := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target()}, Fields: GateFields{Subject: "aging"}, Namespace: "lm", Actor: "u", Now: created})
	execSQL(t, db, `UPDATE beads SET priority = 3 WHERE id = ?`, aging)
	plain := mustCreate(t, db, CreateInput{Title: "plain", Priority: 2, BeadType: "gate", Namespace: "lm", Actor: "u", Now: created})
	gated := mustGateCreate(t, db, GateCreateInput{Blocks: []string{target()}, Fields: GateFields{Subject: "gated"}, Namespace: "lm", Actor: "u", Now: created})
	execSQL(t, db, `UPDATE beads SET priority = 4 WHERE id = ?`, gated)
	mustLink(t, db, gated, newBead(t, db, created), BlocksDepType)

	order := func(entries []GateListEntry) []string {
		ids := make([]string, len(entries))
		for i, e := range entries {
			ids[i] = e.ID
		}
		return ids
	}
	for _, mode := range []string{"off", "shadow"} {
		execSQL(t, db, `UPDATE coefficients SET value = ? WHERE key = 'time_adjust'`, mode)
		entries, eff, err := GateListEffective(t.Context(), db, now)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := order(entries), []string{plain, aging, gated}; !slices.Equal(got, want) {
			t.Errorf("time_adjust=%s: order = %v, want %v", mode, got, want)
		}
		if e, ok := eff[aging]; ok && e.Source == SourceAged {
			t.Errorf("time_adjust=%s: eff(aging) = %+v, want no promotion", mode, e)
		}
	}

	execSQL(t, db, `UPDATE coefficients SET value = 'on' WHERE key = 'time_adjust'`)
	entries, eff, err := GateListEffective(t.Context(), db, now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := order(entries), []string{aging, plain, gated}; !slices.Equal(got, want) {
		t.Errorf("on: order = %v, want %v", got, want)
	}
	if e := eff[aging]; e.Priority != 1 || e.Source != SourceAged {
		t.Errorf("on: eff(aging) = %+v, want P1 aged", e)
	}
	if e, ok := eff[gated]; ok && e.Source == SourceAged {
		t.Errorf("on: eff(gated) = %+v, a Gate that waits on a prerequisite must not be promoted", e)
	}
	if e, ok := eff[plain]; ok && e.Source == SourceAged {
		t.Errorf("on: eff(plain) = %+v, a Gate that blocks nothing has no demand", e)
	}
}

func TestTimeAdjustedListsSurfaceQueryErrors(t *testing.T) {
	f := newAdjustFixture(t)
	execSQL(t, f.db, `UPDATE coefficients SET value = 'on' WHERE key = 'time_adjust'`)
	sweep := func(name string, run func(q Querier) error) {
		t.Helper()
		sawErr := false
		for failOn := 1; failOn < 200; failOn++ {
			err := run(&nthCallErrQuerier{Querier: f.db, failOn: failOn})
			if err == nil {
				break
			}
			sawErr = true
			if !errors.Is(err, errBoom) {
				t.Errorf("%s failOn=%d: err = %v, want errBoom", name, failOn, err)
			}
		}
		if !sawErr {
			t.Errorf("%s: no query failure surfaced", name)
		}
	}
	sweep("BlockedBeadsEffective", func(q Querier) error {
		_, _, _, err := BlockedBeadsEffective(t.Context(), q, ReadyFilter{Priority: -1, Now: f.now})
		return err
	})

	created := stamp(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 0)
	target := mustCreate(t, f.db, CreateInput{Title: "target", Priority: 4, BeadType: "task", Namespace: "lm", Actor: "u", Now: created})
	mustGateCreate(t, f.db, GateCreateInput{Blocks: []string{target}, Fields: GateFields{Subject: "aging"}, Namespace: "lm", Actor: "u", Now: created})
	sweep("GateListEffective", func(q Querier) error {
		_, _, err := GateListEffective(t.Context(), q, f.now)
		return err
	})
}
