// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAdjustTable(t *testing.T) {
	p := adjustParams{
		promote: 72 * time.Hour, promoteGate: 6 * time.Hour, decay: 7 * 24 * time.Hour, hold: 14 * 24 * time.Hour,
		ceiling: [MaxSeverity + 1]int{0, 1, 1, 2, 3},
		floor:   [MaxSeverity + 1]int{0, 0, 2, 4, 4},
	}
	const day = 24 * time.Hour
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		in         adjustInput
		elapsed    time.Duration
		wantPrio   int
		wantSource string
		wantHeld   bool
	}{
		{"demand, no time passed", adjustInput{Graph: 4, Stored: 4, Severity: 3, Demand: true}, 0, 4, "", false},
		{"demand, one step", adjustInput{Graph: 4, Stored: 4, Severity: 3, Demand: true}, 3 * day, 3, SourceAged, false},
		{"demand, stops at the severity 3 ceiling", adjustInput{Graph: 4, Stored: 4, Severity: 3, Demand: true}, 30 * day, 2, SourceAged, false},
		{"demand, severity 4 ceiling is P3", adjustInput{Graph: 4, Stored: 4, Severity: 4, Demand: true}, 30 * day, 3, SourceAged, false},
		{"demand, severity 1 climbs to P1", adjustInput{Graph: 4, Stored: 4, Severity: 1, Demand: true}, 30 * day, 1, SourceAged, false},
		{"demand, graph value above the ceiling is kept", adjustInput{Graph: 1, Stored: 3, Severity: 3, Demand: true}, 30 * day, 1, "", false},
		{"demand, gate climbs on the gate interval", adjustInput{Graph: 4, Stored: 4, Severity: 3, Demand: true, Gate: true}, 12 * time.Hour, 2, SourceAged, false},
		{"demand never holds", adjustInput{Graph: 4, Stored: 4, Severity: 4, Demand: true}, 400 * day, 3, SourceAged, false},
		{"no demand, one decay step", adjustInput{Graph: 1, Stored: 1, Severity: 3}, 7 * day, 2, SourceDecayed, false},
		{"no demand, decay stops at the floor", adjustInput{Graph: 1, Stored: 1, Severity: 3}, 30 * day, 4, SourceDecayed, false},
		{"no demand, held after floor plus hold", adjustInput{Graph: 1, Stored: 1, Severity: 3}, 35 * day, 4, SourceDecayed, true},
		{"no demand, one day before hold", adjustInput{Graph: 1, Stored: 1, Severity: 3}, 35*day - time.Second, 4, SourceDecayed, false},
		{"stored at the floor holds after hold only", adjustInput{Graph: 4, Stored: 4, Severity: 3}, 14 * day, 4, "", true},
		{"stored at the floor, just before hold", adjustInput{Graph: 4, Stored: 4, Severity: 3}, 14*day - time.Second, 4, "", false},
		{"stored P2 reaches the floor in two steps", adjustInput{Graph: 2, Stored: 2, Severity: 3}, 28 * day, 4, SourceDecayed, true},
		{"severity 4 holds", adjustInput{Graph: 4, Stored: 4, Severity: 4}, 14 * day, 4, "", true},
		{"severity 2 decays but never holds", adjustInput{Graph: 1, Stored: 1, Severity: 2}, 400 * day, 2, SourceDecayed, false},
		{"severity 2 below its floor is not moved", adjustInput{Graph: 4, Stored: 4, Severity: 2}, 400 * day, 4, "", false},
		{"severity 1 neither decays nor holds", adjustInput{Graph: 1, Stored: 1, Severity: 1}, 400 * day, 1, "", false},
		{"clock before progress changes nothing", adjustInput{Graph: 1, Stored: 1, Severity: 3}, -day, 1, "", false},
		{"severity out of range is clamped", adjustInput{Graph: 4, Stored: 4, Severity: 9, Demand: true}, 30 * day, 3, SourceAged, false},
		{"expedited without demand neither decays nor holds", adjustInput{Graph: 0, Stored: 2, Severity: 3}, 400 * day, 0, "", false},
		{"expedited severity 4 never holds", adjustInput{Graph: 0, Stored: 4, Severity: 4}, 400 * day, 0, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			in.ReadySince, in.Progress = start, start
			in.Now = start.Add(tc.elapsed)
			got := adjust(p, in)
			if got.Priority != tc.wantPrio || got.Source != tc.wantSource || got.Held != tc.wantHeld {
				t.Errorf("adjust = %+v, want priority %d source %q held %v", got, tc.wantPrio, tc.wantSource, tc.wantHeld)
			}
		})
	}
}

func setCoefficients(t *testing.T, db *sql.DB, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if _, err := db.ExecContext(t.Context(), `UPDATE coefficients SET value = ? WHERE key = ?`, v, k); err != nil {
			t.Fatal(err)
		}
	}
}

func stamp(t0 time.Time, d time.Duration) string {
	return t0.Add(d).UTC().Format("2006-01-02T15:04:05.000Z")
}

func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

type adjustFixture struct {
	db  *sql.DB
	now string
	ids map[string]string
}

func newAdjustFixture(t *testing.T) *adjustFixture {
	t.Helper()
	db := testDB(t)
	setCoefficients(t, db, map[string]string{
		"promote_after": "24h", "promote_after_gate": "24h", "decay_after": "24h", "hold_after": "24h", "redetect_window": "24h",
		"severity_1_ceiling": "1", "severity_2_ceiling": "1", "severity_2_floor": "2",
		"severity_3_ceiling": "2", "severity_3_floor": "4", "severity_4_ceiling": "3", "severity_4_floor": "4",
	})
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	f := &adjustFixture{db: db, now: stamp(t0, 84*time.Hour), ids: map[string]string{}}
	create := func(name string, in CreateInput) string {
		in.Title, in.BeadType, in.Namespace, in.Actor = name, "task", "lm", "u"
		if in.Now == "" {
			in.Now = stamp(t0, 0)
		}
		id := mustCreate(t, db, in)
		f.ids[name] = id
		return id
	}
	create("plain", CreateInput{Priority: 2, Severity: 1, Now: stamp(t0, -time.Hour)})
	aged := create("aged", CreateInput{Priority: 4})
	create("downstream", CreateInput{Priority: 4, BlockedBy: []string{aged}})
	create("held", CreateInput{Priority: 2})
	create("decayed", CreateInput{Priority: 1})
	epic := create("epic", CreateInput{Priority: 4, Labels: []string{"milestone:x"}})
	create("milestone-child", CreateInput{Priority: 4, Parent: epic})
	due := t0.Add(400 * time.Hour)
	create("due", CreateInput{Priority: 4, Severity: 4, Due: &due, Clock: t0})
	redetected := create("redetected", CreateInput{Priority: 4})
	execSQL(t, db, `UPDATE beads SET last_redetected_at = ? WHERE id = ?`, stamp(t0, 83*time.Hour), redetected)
	return f
}

func (f *adjustFixture) list(t *testing.T, adjusted bool) ReadyResult {
	t.Helper()
	res, err := ReadyList(t.Context(), f.db, ReadyFilter{Priority: -1, Now: f.now, TimeAdjusted: adjusted})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (f *adjustFixture) order(res ReadyResult) []string {
	byID := map[string]string{}
	for name, id := range f.ids {
		byID[id] = name
	}
	out := make([]string, 0, len(res.Beads))
	for _, b := range res.Beads {
		out = append(out, byID[b.ID])
	}
	return out
}

func TestReadyListTimeAdjusted(t *testing.T) {
	f := newAdjustFixture(t)
	res := f.list(t, true)

	if !res.Adjusted || res.Held != 1 {
		t.Fatalf("adjusted = %v, held = %d, want true and 1", res.Adjusted, res.Held)
	}
	want := map[string]EffectivePriority{
		"aged":            {Priority: 2, Source: SourceAged},
		"milestone-child": {Priority: 2, Source: SourceAged},
		"redetected":      {Priority: 2, Source: SourceAged},
		"due":             {Priority: 3, Source: SourceAged},
		"decayed":         {Priority: 4, Source: SourceDecayed},
	}
	for name, e := range want {
		if got := res.Eff[f.ids[name]]; got != e {
			t.Errorf("eff(%s) = %+v, want %+v", name, got, e)
		}
	}
	if got, ok := res.Eff[f.ids["plain"]]; ok && got.Source != "" {
		t.Errorf("eff(plain) = %+v, want no time-adjusted source", got)
	}
	order := f.order(res)
	if slices.Contains(order, "held") || slices.Contains(order, "downstream") || slices.Contains(order, "epic") {
		t.Errorf("order = %v lists a held, blocked or parent Bead", order)
	}
	if !res.Demand[f.ids["aged"]] || res.Demand[f.ids["plain"]] || res.Demand[f.ids["decayed"]] {
		t.Errorf("demand = %v", res.Demand)
	}
	if i, j := slices.Index(order, "aged"), slices.Index(order, "plain"); i < 0 || j < 0 || i > j {
		t.Errorf("order = %v, want demand (aged) ahead of the older P2 without demand (plain)", order)
	}
	if i, j := slices.Index(order, "due"), slices.Index(order, "decayed"); i < 0 || j < 0 || i > j {
		t.Errorf("order = %v, want due (P3) ahead of decayed (P4)", order)
	}
	again := f.list(t, true)
	if !slices.Equal(f.order(again), order) || again.Held != res.Held {
		t.Errorf("same now, different order: %v vs %v", f.order(again), order)
	}
}

func TestReadyListShadowAndOffKeepDefaultOutput(t *testing.T) {
	f := newAdjustFixture(t)
	execSQL(t, f.db, `UPDATE coefficients SET value = 'off' WHERE key = 'time_adjust'`)
	off := f.list(t, false)
	execSQL(t, f.db, `UPDATE coefficients SET value = 'shadow' WHERE key = 'time_adjust'`)
	shadow := f.list(t, false)

	if shadow.Adjusted || off.Adjusted || shadow.Held != 0 {
		t.Fatalf("shadow adjusted = %v held = %d, off adjusted = %v", shadow.Adjusted, shadow.Held, off.Adjusted)
	}
	if !slices.Equal(f.order(shadow), f.order(off)) {
		t.Errorf("shadow order %v differs from off order %v", f.order(shadow), f.order(off))
	}
	if !slices.Contains(f.order(shadow), "held") {
		t.Errorf("shadow order %v hides a Bead the old ready listed", f.order(shadow))
	}
	for id, e := range shadow.Eff {
		if e.Source == SourceAged || e.Source == SourceDecayed {
			t.Errorf("shadow eff(%s) = %+v carries a time-adjusted source", id, e)
		}
	}
	execSQL(t, f.db, `UPDATE coefficients SET value = 'on' WHERE key = 'time_adjust'`)
	on := f.list(t, false)
	forced := f.list(t, true)
	if !on.Adjusted || !slices.Equal(f.order(on), f.order(forced)) || on.Held != forced.Held {
		t.Errorf("on order %v (held %d) differs from the forced adjusted order %v (held %d)", f.order(on), on.Held, f.order(forced), forced.Held)
	}
}

func TestTimeAdjustReadySinceAndProgressResets(t *testing.T) {
	db := testDB(t)
	setCoefficients(t, db, map[string]string{
		"promote_after": "24h", "decay_after": "24h", "hold_after": "24h",
		"severity_3_ceiling": "2", "severity_3_floor": "4",
	})
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	now := stamp(t0, 84*time.Hour)
	recent := stamp(t0, 82*time.Hour)
	mk := func(title string, in CreateInput) string {
		in.Title, in.BeadType, in.Namespace, in.Actor, in.Now = title, "task", "lm", "u", stamp(t0, 0)
		return mustCreate(t, db, in)
	}
	byPrereq := mk("prereq-reset", CreateInput{Priority: 4})
	prereq := mk("prereq", CreateInput{Priority: 4})
	mustLink(t, db, byPrereq, prereq, BlocksDepType)
	mk("down-1", CreateInput{Priority: 4, BlockedBy: []string{byPrereq}})
	execSQL(t, db, `UPDATE beads SET status = 'closed', closed_at = ? WHERE id = ?`, recent, prereq)

	byRemoved := mk("removed-reset", CreateInput{Priority: 4})
	gone := mk("gone", CreateInput{Priority: 4})
	mustLink(t, db, byRemoved, gone, BlocksDepType)
	mk("down-2", CreateInput{Priority: 4, BlockedBy: []string{byRemoved}})
	execSQL(t, db, `UPDATE dependencies SET removed = 1, removed_set_at = ? WHERE bead_id = ? AND depends_on_id = ?`, recent, byRemoved, gone)

	parent := mk("child-reset", CreateInput{Priority: 4})
	child := mk("child", CreateInput{Priority: 4, Parent: parent})
	mk("down-3", CreateInput{Priority: 4, BlockedBy: []string{parent}})
	execSQL(t, db, `UPDATE beads SET status = 'closed', closed_at = ? WHERE id = ?`, recent, child)

	titled := mk("title-touch", CreateInput{Priority: 1})
	execSQL(t, db, `UPDATE beads SET title_set_at = ? WHERE id = ?`, recent, titled)
	linked := mk("link-touch", CreateInput{Priority: 1})
	other := mk("other", CreateInput{Priority: 1})
	execSQL(t, db, `INSERT INTO dependencies (bead_id, depends_on_id, type, created_at) VALUES (?, ?, 'discovered-from', ?)`, linked, other, recent)
	execSQL(t, db, `UPDATE beads SET title_set_at = ?, status_set_at = ? WHERE id = ?`, stamp(t0, 0), stamp(t0, 0), other)
	untouched := mk("untouched", CreateInput{Priority: 1})

	res, err := ReadyList(t.Context(), db, ReadyFilter{Priority: -1, Now: now, TimeAdjusted: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{"prereq-reset": byPrereq, "removed-reset": byRemoved, "child-reset": parent, "title-touch": titled, "link-touch": linked} {
		if e, ok := res.Eff[id]; ok && e.Source != "" {
			t.Errorf("eff(%s) = %+v, want the clock reset to leave it unadjusted", name, e)
		}
	}
	if res.Held != 1 {
		t.Errorf("held = %d, want 1 (only the untouched P4 whose link was removed)", res.Held)
	}
	if e := res.Eff[untouched]; e.Source != SourceDecayed {
		t.Errorf("eff(untouched) = %+v, want decayed", e)
	}
}

func TestReadyListTimeAdjustFailures(t *testing.T) {
	f := newAdjustFixture(t)
	filter := ReadyFilter{Priority: -1, Now: f.now, TimeAdjusted: true}
	sawErr := false
	for failOn := 1; ; failOn++ {
		_, err := ReadyList(t.Context(), &nthCallErrQuerier{Querier: f.db, failOn: failOn}, filter)
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
	filter.Now = "not-a-time"
	if _, err := ReadyList(t.Context(), f.db, filter); err == nil || !strings.Contains(err.Error(), `now "not-a-time"`) {
		t.Errorf("err = %v, want a now parse error", err)
	}
	execSQL(t, f.db, `DELETE FROM coefficients WHERE key = 'time_adjust'`)
	if _, err := ReadyList(t.Context(), f.db, ReadyFilter{Priority: -1, Now: f.now}); err == nil || !strings.Contains(err.Error(), "time_adjust") {
		t.Errorf("err = %v, want a missing time_adjust error", err)
	}
}

func TestValidateTimeAdjustCoefficient(t *testing.T) {
	for _, v := range []string{"off", "shadow", "on"} {
		if err := ValidateCoefficientValue("time_adjust", v); err != nil {
			t.Errorf("time_adjust=%q: %v", v, err)
		}
	}
	for _, v := range []string{"", "maybe", "ON", "1"} {
		var ve *ValidationError
		if err := ValidateCoefficientValue("time_adjust", v); !errors.As(err, &ve) || !strings.Contains(err.Error(), "time_adjust") {
			t.Errorf("time_adjust=%q: err = %v, want a validation error naming the key", v, err)
		}
	}
}

func TestTimeAdjustWithoutCandidatesKeepsRaised(t *testing.T) {
	f := newAdjustFixture(t)
	raised := map[string]EffectivePriority{"x": {Priority: 1, Source: "expedite"}}
	res, err := TimeAdjust(t.Context(), f.db, f.now, mustCoefficients(t, f.db), raised, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Eff) != 1 || res.Eff["x"] != raised["x"] {
		t.Errorf("Eff = %+v, want a copy of raised", res.Eff)
	}
	if len(res.Demand)+len(res.Held) != 0 {
		t.Errorf("Demand = %v, Held = %v, want both empty", res.Demand, res.Held)
	}
}

func TestTimeAdjustKeepsExpeditedAtP0(t *testing.T) {
	f := newAdjustFixture(t)
	held := f.ids["held"]
	execSQL(t, f.db, `UPDATE beads SET severity = 3, expedite_until = ?, expedite_reason = 'stop the bleeding' WHERE id = ?`, stamp(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 100*time.Hour), held)
	res := f.list(t, true)
	if got := res.Eff[held]; got.Priority != 0 || got.Source != SourceExpedite {
		t.Errorf("eff(held) = %+v, want P0 from expedite", got)
	}
	if res.Held != 0 || res.Demand[held] {
		t.Errorf("held = %d, demand = %v, want the expedited Bead neither held nor in demand", res.Held, res.Demand[held])
	}
	if order := f.order(res); len(order) == 0 || order[0] != "held" {
		t.Errorf("order = %v, want the expedited Bead first", order)
	}
}

func TestTimeAdjustIgnoresUnparsableStamps(t *testing.T) {
	f := newAdjustFixture(t)
	execSQL(t, f.db, `UPDATE beads SET title_set_at = 'garbage', last_redetected_at = 'garbage' WHERE id = ?`, f.ids["decayed"])
	res := f.list(t, true)
	if e := res.Eff[f.ids["decayed"]]; e.Source != SourceDecayed {
		t.Errorf("eff(decayed) = %+v, want decayed from the remaining valid stamps", e)
	}
	if res.Demand[f.ids["decayed"]] {
		t.Error("an unparsable last_redetected_at must not count as demand")
	}
}
