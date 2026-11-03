// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestHeldReleased(t *testing.T) {
	type scenario struct {
		name      string
		mode      string
		setup     func(t *testing.T, f *adjustFixture)
		endpoints func(f *adjustFixture) []string
		write     func(t *testing.T, f *adjustFixture) error
		want      func(f *adjustFixture) []string
		wantOn    bool
	}
	held := func(f *adjustFixture) []string { return []string{f.ids["held"]} }
	endpointHeld := func(f *adjustFixture) []string { return []string{f.ids["held"]} }
	updateOf := func(target string, set func(in *UpdateInput, now time.Time)) func(t *testing.T, f *adjustFixture) error {
		return func(t *testing.T, f *adjustFixture) error {
			t.Helper()
			now, err := time.Parse(time.RFC3339Nano, f.now)
			if err != nil {
				t.Fatal(err)
			}
			in := UpdateInput{ID: f.ids[target], Priority: -1, Actor: "u", Now: f.now, Clock: now, Reason: "r"}
			set(&in, now)
			return mustUpdate(t, f.db, in)
		}
	}
	update := func(set func(in *UpdateInput, now time.Time)) func(t *testing.T, f *adjustFixture) error {
		return updateOf("held", set)
	}
	scenarios := []scenario{
		{
			name:      "blocks: a new open downstream gives the held Bead demand",
			mode:      "on",
			endpoints: endpointHeld,
			write: func(t *testing.T, f *adjustFixture) error {
				t.Helper()
				down := newBead(t, f.db, f.now)
				return mustAddLink(t, f.db, down, f.ids["held"], BlocksDepType)
			},
			want:   held,
			wantOn: true,
		},
		{
			name:      "parent-child: the held Bead joins a milestone Epic",
			mode:      "on",
			endpoints: func(f *adjustFixture) []string { return []string{f.ids["held"], f.ids["epic"]} },
			write: func(t *testing.T, f *adjustFixture) error {
				t.Helper()
				return mustAddLink(t, f.db, f.ids["held"], f.ids["epic"], ParentChildDepType)
			},
			want:   held,
			wantOn: true,
		},
		{
			name: "parent-child: a descendant of the endpoint gains milestone demand",
			mode: "on",
			setup: func(t *testing.T, f *adjustFixture) {
				t.Helper()
				mustLink(t, f.db, f.ids["held"], f.ids["plain"], ParentChildDepType)
			},
			endpoints: func(f *adjustFixture) []string { return []string{f.ids["plain"]} },
			write: updateOf("plain", func(in *UpdateInput, _ time.Time) {
				in.AddLabels = []string{"milestone:y"}
			}),
			want:   held,
			wantOn: true,
		},
		{
			name:      "severity",
			mode:      "on",
			endpoints: endpointHeld,
			write: update(func(in *UpdateInput, _ time.Time) {
				sev := 2
				in.Severity = &sev
			}),
			want:   held,
			wantOn: true,
		},
		{
			name:      "expedite",
			mode:      "on",
			endpoints: endpointHeld,
			write: update(func(in *UpdateInput, _ time.Time) {
				span := 2 * time.Hour
				in.ExpediteFor = &span
			}),
			want:   held,
			wantOn: true,
		},
		{
			name:      "due",
			mode:      "on",
			endpoints: endpointHeld,
			write: update(func(in *UpdateInput, now time.Time) {
				due := now.Add(48 * time.Hour)
				in.Due = &due
			}),
			want:   held,
			wantOn: true,
		},
		{
			name:      "outside the sub-graph: the held Bead is not reached from the endpoints",
			mode:      "on",
			endpoints: func(f *adjustFixture) []string { return []string{f.ids["plain"]} },
			write: func(t *testing.T, f *adjustFixture) error {
				t.Helper()
				down := newBead(t, f.db, f.now)
				return mustAddLink(t, f.db, down, f.ids["held"], BlocksDepType)
			},
			want:   func(*adjustFixture) []string { return nil },
			wantOn: true,
		},
		{
			name:      "no endpoints",
			mode:      "on",
			endpoints: func(*adjustFixture) []string { return nil },
			write:     func(*testing.T, *adjustFixture) error { return nil },
			want:      func(*adjustFixture) []string { return nil },
			wantOn:    true,
		},
		{
			name:      "nothing released by an unrelated write",
			mode:      "on",
			endpoints: endpointHeld,
			write:     func(*testing.T, *adjustFixture) error { return nil },
			want:      func(*adjustFixture) []string { return nil },
			wantOn:    true,
		},
		{
			name:      "shadow reports nothing",
			mode:      "shadow",
			endpoints: endpointHeld,
			write:     update(func(in *UpdateInput, _ time.Time) { sev := 2; in.Severity = &sev }),
			want:      func(*adjustFixture) []string { return nil },
		},
		{
			name:      "off reports nothing",
			mode:      "off",
			endpoints: endpointHeld,
			write:     update(func(in *UpdateInput, _ time.Time) { sev := 2; in.Severity = &sev }),
			want:      func(*adjustFixture) []string { return nil },
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			f := newAdjustFixture(t)
			execSQL(t, f.db, `UPDATE coefficients SET value = ? WHERE key = 'time_adjust'`, sc.mode)
			if sc.setup != nil {
				sc.setup(t, f)
			}
			ran := false
			got, on, err := HeldReleased(t.Context(), f.db, f.now, sc.endpoints(f), func() error {
				ran = true
				return sc.write(t, f)
			})
			if err != nil {
				t.Fatal(err)
			}
			if !ran {
				t.Error("fn was not called")
			}
			if on != sc.wantOn {
				t.Errorf("on = %v, want %v", on, sc.wantOn)
			}
			if want := sc.want(f); !slices.Equal(got, want) {
				t.Errorf("released = %v, want %v", got, want)
			}
		})
	}

	t.Run("fn error is returned", func(t *testing.T) {
		f := newAdjustFixture(t)
		execSQL(t, f.db, `UPDATE coefficients SET value = 'on' WHERE key = 'time_adjust'`)
		boom := errors.New("fn failed")
		got, _, err := HeldReleased(t.Context(), f.db, f.now, []string{f.ids["held"]}, func() error { return boom })
		if !errors.Is(err, boom) || got != nil {
			t.Errorf("got %v, err %v, want nil and fn's error", got, err)
		}
	})

	t.Run("fn error is returned when time_adjust is not on", func(t *testing.T) {
		f := newAdjustFixture(t)
		boom := errors.New("fn failed")
		if _, _, err := HeldReleased(t.Context(), f.db, f.now, nil, func() error { return boom }); !errors.Is(err, boom) {
			t.Errorf("err = %v, want fn's error", err)
		}
	})

	t.Run("query errors surface", func(t *testing.T) {
		f := newAdjustFixture(t)
		execSQL(t, f.db, `UPDATE coefficients SET value = 'on' WHERE key = 'time_adjust'`)
		sawErr := false
		for failOn := 1; failOn < 200; failOn++ {
			_, _, err := HeldReleased(t.Context(), &nthCallErrQuerier{Querier: f.db, failOn: failOn}, f.now, []string{f.ids["held"]}, func() error { return nil })
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
	})
}
