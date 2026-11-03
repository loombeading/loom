// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	StateNormal   = "normal"
	StatePromoted = "promoted"
	StateDecayed  = "decayed"
	StateHeld     = "held"
)

const (
	TransitionExpedite   = "expedite"
	TransitionDue        = "due"
	TransitionPromote    = "promote"
	TransitionDecay      = "decay"
	TransitionHold       = "hold"
	TransitionHoldCancel = "hold-cancel"
)

const (
	LeverRevive   = "revive"
	LeverDue      = "due"
	LeverBlocks   = "blocks"
	LeverSeverity = "severity"
	LeverExpedite = "expedite"
)

type Transition struct {
	At       time.Time
	Kind     string
	Priority int
	Timed    bool
}

type Lever struct {
	Kind     string
	Severity int
	Ceiling  int
}

type Breakdown struct {
	Mode string

	Stored   int
	Severity int
	Ceiling  int
	Floor    int
	Holdable bool

	Graph EffectivePriority

	ExpediteUntil  time.Time
	ExpediteReason string
	ExpediteActive bool

	Due       time.Time
	DueFrom   time.Time
	DueActive bool

	Demand DemandEvidence

	Ready      bool
	ReadySince Stamp
	Progress   Stamp

	State       string
	Effective   EffectivePriority
	EffectiveOn *EffectivePriority

	Next   []Transition
	Levers []Lever
}

func beadReady(ctx context.Context, q Querier, b Bead) (bool, error) {
	if b.Status != StatusOpen {
		return false, nil
	}
	if b.BeadType == BeadTypeGate {
		blockers, err := ActiveBlockers(ctx, q, b.ID)
		return len(blockers) == 0, err
	}
	fromWhere, args := ReadyWhere(ReadyFilter{Priority: -1})
	var one int
	err := q.QueryRowContext(ctx, "SELECT 1 "+fromWhere+" AND i.id = ?", append(args, b.ID)...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("breakdown: ready of %s: %w", b.ID, err)
	}
	return true, nil
}

func BreakdownOf(ctx context.Context, q Querier, now, id string) (Breakdown, error) {
	now = nowOrDefault(now)
	nowT, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		return Breakdown{}, fmt.Errorf("breakdown: now %q: %w", now, err)
	}
	coef, err := LoadCoefficients(ctx, q)
	if err != nil {
		return Breakdown{}, err
	}
	b, err := GetBead(ctx, q, id)
	if err != nil {
		return Breakdown{}, err
	}
	graph, raised, err := EffectivePriorityOf(ctx, q, now, id)
	if err != nil {
		return Breakdown{}, err
	}
	raisedMap := map[string]EffectivePriority{}
	if raised {
		raisedMap[id] = graph
	}
	ds, err := derive(ctx, q, nowT, coef, raisedMap, []Bead{b}, []any{StatusOpen, StatusInProgress})
	if err != nil {
		return Breakdown{}, err
	}
	d := ds[id]
	ready, err := beadReady(ctx, q, b)
	if err != nil {
		return Breakdown{}, err
	}
	params := coef.adjustParams()
	sev := min(max(b.Severity, 1), MaxSeverity)

	out := Breakdown{
		Mode:     coef[CoefficientTimeAdjust],
		Stored:   b.Priority,
		Severity: b.Severity,
		Ceiling:  params.ceiling[sev],
		Floor:    params.floor[sev],
		Holdable: holdable(sev),
		Graph:    graph,
		Demand:   d.Demand,
		Ready:    ready,
		Progress: d.Progress,
		State:    StateNormal,
	}
	if ready {
		out.ReadySince = d.ReadySince
	}
	if b.ExpediteUntil.Valid {
		out.ExpediteUntil = parseStamp(b.ExpediteUntil)
		out.ExpediteReason = b.ExpediteReason.String
		out.ExpediteActive = out.ExpediteUntil.After(nowT)
	}
	lead := coef.Duration("due_lead")
	if b.DueAt.Valid {
		out.Due = parseStamp(b.DueAt)
		out.DueFrom = out.Due.Add(-lead)
		out.DueActive = !out.DueFrom.After(nowT)
	}

	res := adjustResult{Priority: graph.Priority}
	if ready {
		res = d.Result
	}
	switch {
	case res.Held:
		out.State = StateHeld
	case res.Source == SourceAged:
		out.State = StatePromoted
	case res.Source == SourceDecayed:
		out.State = StateDecayed
	}
	adjusted := graph
	if res.Source != "" {
		adjusted = EffectivePriority{Priority: res.Priority, Source: res.Source}
	}
	out.Effective = graph
	if out.Mode == TimeAdjustOn {
		out.Effective = adjusted
	} else if adjusted != graph {
		out.EffectiveOn = &adjusted
	}

	out.Next, err = nextTransitions(ctx, q, nowT, coef, params, b, out, d, res)
	if err != nil {
		return Breakdown{}, err
	}
	out.Levers = leversOf(coef, out, adjusted.Priority)
	return out, nil
}

func nextTransitions(ctx context.Context, q Querier, nowT time.Time, coef Coefficients, params adjustParams, b Bead, out Breakdown, d derivation, res adjustResult) ([]Transition, error) {
	var next []Transition
	if out.ExpediteActive {
		after, _, err := EffectivePriorityOf(ctx, q, FormatTime(out.ExpediteUntil), b.ID)
		if err != nil {
			return nil, err
		}
		next = append(next, Transition{At: out.ExpediteUntil, Kind: TransitionExpedite, Priority: after.Priority})
	}
	if b.DueAt.Valid && !out.DueActive && out.Graph.Priority > 1 {
		next = append(next, Transition{At: out.DueFrom, Kind: TransitionDue, Priority: 1})
	}
	if out.Ready {
		sev := min(max(b.Severity, 1), MaxSeverity)
		graph := d.Input.Graph
		switch {
		case d.Input.Demand && graph > params.ceiling[sev]:
			every := params.promote
			if d.Input.Gate {
				every = params.promoteGate
			}
			k := steps(nowT.Sub(d.Input.ReadySince), every)
			if max(graph-k, params.ceiling[sev]) > params.ceiling[sev] && every > 0 {
				next = append(next, Transition{
					At:       d.Input.ReadySince.Add(time.Duration(k+1) * every),
					Kind:     TransitionPromote,
					Priority: max(graph-k-1, params.ceiling[sev]),
					Timed:    true,
				})
			}
		case !d.Input.Demand && params.floor[sev] != 0 && graph < params.floor[sev]:
			k := steps(nowT.Sub(d.Input.Progress), params.decay)
			if min(graph+k, params.floor[sev]) < params.floor[sev] && params.decay > 0 {
				next = append(next, Transition{
					At:       d.Input.Progress.Add(time.Duration(k+1) * params.decay),
					Kind:     TransitionDecay,
					Priority: min(graph+k+1, params.floor[sev]),
					Timed:    true,
				})
			}
		}
		if !res.HeldFrom.IsZero() {
			if nowT.Before(res.HeldFrom) {
				next = append(next, Transition{At: res.HeldFrom, Kind: TransitionHold, Timed: true})
			}
			if cancelAt := res.HeldFrom.Add(coef.Duration("cancel_after")); nowT.Before(cancelAt) {
				next = append(next, Transition{At: cancelAt, Kind: TransitionHoldCancel, Timed: true})
			}
		}
	}
	slices.SortStableFunc(next, func(a, b Transition) int { return a.At.Compare(b.At) })
	return next, nil
}

func leversOf(coef Coefficients, out Breakdown, current int) []Lever {
	var levers []Lever
	if out.State == StateHeld {
		levers = append(levers, Lever{Kind: LeverRevive})
	}
	if !out.Demand.Any() {
		levers = append(levers, Lever{Kind: LeverDue}, Lever{Kind: LeverBlocks})
	}
	if out.Severity > MinSeverity && out.Severity <= MaxSeverity && current <= out.Ceiling {
		n := out.Severity - 1
		if c := coef.Int(fmt.Sprintf("severity_%d_ceiling", n)); c < current {
			levers = append(levers, Lever{Kind: LeverSeverity, Severity: n, Ceiling: c})
		}
	}
	if !out.ExpediteActive {
		levers = append(levers, Lever{Kind: LeverExpedite})
	}
	return levers
}
