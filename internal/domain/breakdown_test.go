// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"database/sql"
	"testing"
	"time"
)

var breakdownT0 = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func at(h int) time.Time { return breakdownT0.Add(time.Duration(h) * time.Hour) }

func breakdownDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testDB(t)
	setCoefficients(t, db, map[string]string{
		"promote_after": "24h", "promote_after_gate": "6h", "decay_after": "24h", "hold_after": "24h",
		"cancel_after": "720h", "redetect_window": "24h", "due_lead": "72h",
		"severity_1_ceiling": "1", "severity_2_ceiling": "1", "severity_2_floor": "2",
		"severity_3_ceiling": "2", "severity_3_floor": "4", "severity_4_ceiling": "3", "severity_4_floor": "4",
	})
	return db
}

func breakdownBead(t *testing.T, db *sql.DB, title string, in CreateInput) string {
	t.Helper()
	in.Title, in.BeadType, in.Namespace, in.Actor, in.Now = title, "task", "lm", "u", stamp(breakdownT0, 0)
	return mustCreate(t, db, in)
}

func breakdownAt(t *testing.T, db *sql.DB, hours int, id string) Breakdown {
	t.Helper()
	got, err := BreakdownOf(t.Context(), db, stamp(breakdownT0, time.Duration(hours)*time.Hour), id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestBreakdownStates(t *testing.T) {
	db := breakdownDB(t)
	downstreamOf := breakdownBead(t, db, "aged", CreateInput{Priority: 4})
	downstream := breakdownBead(t, db, "downstream", CreateInput{Priority: 4, BlockedBy: []string{downstreamOf}})
	held := breakdownBead(t, db, "held", CreateInput{Priority: 2})
	decayed := breakdownBead(t, db, "decayed", CreateInput{Priority: 1})
	plain := breakdownBead(t, db, "plain", CreateInput{Priority: 2, Severity: 1})

	for _, tc := range []struct {
		name      string
		id        string
		state     string
		eff       EffectivePriority
		effIfOn   *EffectivePriority
		next      []Transition
		levers    []Lever
		wantReady bool
	}{
		{
			name: "promoted to the severity 3 ceiling", id: downstreamOf, state: StatePromoted,
			eff: EffectivePriority{Priority: 4}, effIfOn: &EffectivePriority{Priority: 2, Source: SourceAged},
			levers:    []Lever{{Kind: LeverSeverity, Severity: 2, Ceiling: 1}, {Kind: LeverExpedite}},
			wantReady: true,
		},
		{
			name: "held after the floor plus the hold interval", id: held, state: StateHeld,
			eff: EffectivePriority{Priority: 2}, effIfOn: &EffectivePriority{Priority: 4, Source: SourceDecayed},
			next:      []Transition{{At: at(72).Add(720 * time.Hour), Kind: TransitionHoldCancel, Timed: true}},
			levers:    []Lever{{Kind: LeverRevive}, {Kind: LeverDue}, {Kind: LeverBlocks}, {Kind: LeverExpedite}},
			wantReady: true,
		},
		{
			name: "decayed to the floor, hold ahead", id: decayed, state: StateDecayed,
			eff: EffectivePriority{Priority: 1}, effIfOn: &EffectivePriority{Priority: 4, Source: SourceDecayed},
			next: []Transition{
				{At: at(96), Kind: TransitionHold, Timed: true},
				{At: at(96).Add(720 * time.Hour), Kind: TransitionHoldCancel, Timed: true},
			},
			levers:    []Lever{{Kind: LeverDue}, {Kind: LeverBlocks}, {Kind: LeverExpedite}},
			wantReady: true,
		},
		{
			name: "severity 1 never moves", id: plain, state: StateNormal,
			eff:       EffectivePriority{Priority: 2},
			levers:    []Lever{{Kind: LeverDue}, {Kind: LeverBlocks}, {Kind: LeverExpedite}},
			wantReady: true,
		},
		{
			name: "blocked Bead is not time adjusted", id: downstream, state: StateNormal,
			eff:    EffectivePriority{Priority: 4},
			levers: []Lever{{Kind: LeverDue}, {Kind: LeverBlocks}, {Kind: LeverExpedite}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := breakdownAt(t, db, 84, tc.id)
			if got.State != tc.state || got.Effective != tc.eff || got.Ready != tc.wantReady {
				t.Errorf("state=%s effective=%+v ready=%v, want %s %+v %v", got.State, got.Effective, got.Ready, tc.state, tc.eff, tc.wantReady)
			}
			if (got.EffectiveOn == nil) != (tc.effIfOn == nil) || (tc.effIfOn != nil && *got.EffectiveOn != *tc.effIfOn) {
				t.Errorf("effective if on = %v, want %v", got.EffectiveOn, tc.effIfOn)
			}
			if len(got.Next) != len(tc.next) {
				t.Fatalf("next = %+v, want %+v", got.Next, tc.next)
			}
			for i, n := range tc.next {
				if !got.Next[i].At.Equal(n.At) || got.Next[i].Kind != n.Kind || got.Next[i].Priority != n.Priority || got.Next[i].Timed != n.Timed {
					t.Errorf("next[%d] = %+v, want %+v", i, got.Next[i], n)
				}
			}
			if len(got.Levers) != len(tc.levers) {
				t.Fatalf("levers = %+v, want %+v", got.Levers, tc.levers)
			}
			for i, l := range tc.levers {
				if got.Levers[i] != l {
					t.Errorf("levers[%d] = %+v, want %+v", i, got.Levers[i], l)
				}
			}
		})
	}

	t.Run("demand evidence and sources", func(t *testing.T) {
		got := breakdownAt(t, db, 84, downstreamOf)
		if got.Demand.Blocks != downstream || got.Demand.Milestone != "" || got.Demand.Due || got.Demand.Redetected != "" {
			t.Errorf("demand = %+v, want only blocks %s", got.Demand, downstream)
		}
		if !got.ReadySince.Time.Equal(at(0)) || got.ReadySince.Kind != "status" {
			t.Errorf("ready_since = %+v, want status at t0", got.ReadySince)
		}
		if !got.Progress.Time.Equal(at(0)) || got.Progress.Kind != "created_at" {
			t.Errorf("progress = %+v, want created_at at t0", got.Progress)
		}
		if got.Severity != 3 || got.Ceiling != 2 || got.Floor != 4 || !got.Holdable {
			t.Errorf("severity = %d ceiling %d floor %d holdable %v", got.Severity, got.Ceiling, got.Floor, got.Holdable)
		}
	})
}

func TestBreakdownTimeAdjustModes(t *testing.T) {
	db := breakdownDB(t)
	id := breakdownBead(t, db, "aged", CreateInput{Priority: 4})
	breakdownBead(t, db, "downstream", CreateInput{Priority: 4, BlockedBy: []string{id}})
	for _, tc := range []struct {
		mode    string
		eff     EffectivePriority
		hasIfOn bool
	}{
		{TimeAdjustOff, EffectivePriority{Priority: 4}, true},
		{TimeAdjustShadow, EffectivePriority{Priority: 4}, true},
		{TimeAdjustOn, EffectivePriority{Priority: 2, Source: SourceAged}, false},
	} {
		setCoefficients(t, db, map[string]string{"time_adjust": tc.mode})
		got := breakdownAt(t, db, 84, id)
		if got.Mode != tc.mode || got.Effective != tc.eff || (got.EffectiveOn != nil) != tc.hasIfOn {
			t.Errorf("mode %s: mode=%s effective=%+v ifOn=%v", tc.mode, got.Mode, got.Effective, got.EffectiveOn)
		}
		if got.State != StatePromoted {
			t.Errorf("mode %s: state = %s, want promoted in every mode", tc.mode, got.State)
		}
	}
}

func TestBreakdownNextSteps(t *testing.T) {
	db := breakdownDB(t)
	due := at(1000)
	promoting := breakdownBead(t, db, "promoting", CreateInput{Priority: 4, Due: &due, Clock: breakdownT0})
	decaying := breakdownBead(t, db, "decaying", CreateInput{Priority: 1})
	gate := breakdownBead(t, db, "gate", CreateInput{Priority: 4, Severity: 4, Due: &due, Clock: breakdownT0})
	execSQL(t, db, `UPDATE beads SET type = 'gate' WHERE id = ?`, gate)

	got := breakdownAt(t, db, 30, promoting)
	if got.State != StatePromoted || got.EffectiveOn == nil || got.EffectiveOn.Priority != 3 {
		t.Fatalf("promoting: state=%s ifOn=%v, want promoted to P3", got.State, got.EffectiveOn)
	}
	if len(got.Next) != 2 || got.Next[0].Kind != TransitionPromote || !got.Next[0].At.Equal(at(48)) || got.Next[0].Priority != 2 ||
		got.Next[1].Kind != TransitionDue || !got.Next[1].At.Equal(at(1000-72)) || got.Next[1].Priority != 1 || got.Next[1].Timed {
		t.Errorf("promoting next = %+v, want promote->P2 at 48h then due lead->P1 at 928h", got.Next)
	}
	if !got.Due.Equal(due) || !got.DueFrom.Equal(at(928)) || got.DueActive {
		t.Errorf("due = %v from %v active %v", got.Due, got.DueFrom, got.DueActive)
	}

	got = breakdownAt(t, db, 30, decaying)
	if got.State != StateDecayed || got.EffectiveOn == nil || got.EffectiveOn.Priority != 2 {
		t.Fatalf("decaying: state=%s ifOn=%v, want decayed to P2", got.State, got.EffectiveOn)
	}
	if len(got.Next) != 3 || got.Next[0].Kind != TransitionDecay || !got.Next[0].At.Equal(at(48)) || got.Next[0].Priority != 3 ||
		got.Next[1].Kind != TransitionHold || !got.Next[1].At.Equal(at(96)) {
		t.Errorf("decaying next = %+v, want decay->P3 at 48h then hold at 96h", got.Next)
	}

	got = breakdownAt(t, db, 2, gate)
	if len(got.Next) != 2 || got.Next[0].Kind != TransitionPromote || !got.Next[0].At.Equal(at(6)) || got.Next[0].Priority != 3 {
		t.Errorf("gate next = %+v, want the first promote step on the 6h gate interval", got.Next)
	}

	got = breakdownAt(t, db, 1000-72, promoting)
	if !got.DueActive {
		t.Errorf("due lead reached: DueActive = false")
	}
	for _, n := range got.Next {
		if n.Kind == TransitionDue {
			t.Errorf("due lead already reached but next lists %+v", n)
		}
	}
}

func TestBreakdownExpediteAndClaim(t *testing.T) {
	db := breakdownDB(t)
	expedited := breakdownBead(t, db, "expedited", CreateInput{Priority: 4})
	execSQL(t, db, `UPDATE beads SET expedite_until = ?, expedite_reason = 'urgent' WHERE id = ?`, stamp(breakdownT0, 100*time.Hour), expedited)

	got := breakdownAt(t, db, 84, expedited)
	if !got.ExpediteActive || !got.ExpediteUntil.Equal(at(100)) || got.ExpediteReason != "urgent" || got.Graph.Priority != 0 || got.Graph.Source != SourceExpedite {
		t.Errorf("active expedite: %+v", got)
	}
	var expires *Transition
	for i := range got.Next {
		if got.Next[i].Kind == TransitionExpedite {
			expires = &got.Next[i]
		}
	}
	if expires == nil || !expires.At.Equal(at(100)) || expires.Priority != 4 || expires.Timed {
		t.Errorf("expedite next = %+v, want expedite expires -> P4 at 100h", got.Next)
	}
	for _, l := range got.Levers {
		if l.Kind == LeverExpedite {
			t.Errorf("an active expedite still lists the expedite lever")
		}
	}

	got = breakdownAt(t, db, 101, expedited)
	if got.ExpediteActive || got.Graph.Priority != 4 {
		t.Errorf("expired expedite: active=%v graph=%+v", got.ExpediteActive, got.Graph)
	}

	claimed := breakdownBead(t, db, "claimed", CreateInput{Priority: 1})
	execSQL(t, db, `UPDATE beads SET status = 'in_progress' WHERE id = ?`, claimed)
	got = breakdownAt(t, db, 84, claimed)
	if got.Ready || !got.ReadySince.Time.IsZero() || got.State != StateNormal || len(got.Next) != 0 || got.Progress.Time.IsZero() {
		t.Errorf("in_progress: ready=%v ready_since=%+v state=%s next=%+v progress=%+v", got.Ready, got.ReadySince, got.State, got.Next, got.Progress)
	}
}

func TestBreakdownMilestoneAndRedetected(t *testing.T) {
	db := breakdownDB(t)
	epic := breakdownBead(t, db, "epic", CreateInput{Priority: 4, Labels: []string{"milestone:x"}})
	child := breakdownBead(t, db, "child", CreateInput{Priority: 4, Parent: epic})
	execSQL(t, db, `UPDATE beads SET last_redetected_at = ? WHERE id = ?`, stamp(breakdownT0, 83*time.Hour), child)

	got := breakdownAt(t, db, 84, child)
	if got.Demand.Milestone != epic || got.Demand.Redetected != stamp(breakdownT0, 83*time.Hour) || got.Demand.Blocks != "" {
		t.Errorf("demand = %+v, want milestone %s and a redetected stamp", got.Demand, epic)
	}
	got = breakdownAt(t, db, 200, child)
	if got.Demand.Redetected != "" {
		t.Errorf("redetected outside the window still counts: %+v", got.Demand)
	}
}

func TestBreakdownErrors(t *testing.T) {
	db := breakdownDB(t)
	id := breakdownBead(t, db, "x", CreateInput{Priority: 2})
	if _, err := BreakdownOf(t.Context(), db, "not-a-time", id); err == nil {
		t.Error("a bad now must fail")
	}
	if _, err := BreakdownOf(t.Context(), db, stamp(breakdownT0, 0), "nope"); err == nil {
		t.Error("an unknown id must fail")
	}
	execSQL(t, db, `DELETE FROM coefficients WHERE key = 'time_adjust'`)
	if _, err := BreakdownOf(t.Context(), db, stamp(breakdownT0, 0), id); err == nil {
		t.Error("a missing coefficient must fail")
	}
	db2 := breakdownDB(t)
	id2 := breakdownBead(t, db2, "y", CreateInput{Priority: 2})
	for failOn := 1; ; failOn++ {
		_, err := BreakdownOf(t.Context(), &nthCallErrQuerier{Querier: db2, failOn: failOn}, stamp(breakdownT0, 84*time.Hour), id2)
		if err == nil {
			break
		}
	}
}
