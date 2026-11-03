// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"
)

type HoldCancelInput struct {
	Filter ReadyFilter
	Actor  string
	Now    string
	At     string
	Clock  time.Time

	DisplayID func(Bead) string
}

type HeldCancel struct {
	Bead      Bead
	HeldSince time.Time
	Progress  time.Time
}

type HoldCancelResult struct {
	Mode      string
	Cancelled []HeldCancel
	Newly     []string
}

func HoldCancelRun(ctx context.Context, tx *sql.Tx, in HoldCancelInput) (HoldCancelResult, error) {
	coef, err := LoadCoefficients(ctx, tx)
	if err != nil {
		return HoldCancelResult{}, err
	}
	res := HoldCancelResult{Mode: coef[CoefficientTimeAdjust]}
	if res.Mode != TimeAdjustOn {
		return res, nil
	}
	at := nowOrDefault(in.At)
	atT, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return HoldCancelResult{}, fmt.Errorf("hold cancel: now %q: %w", at, err)
	}
	raised, err := RaisedPriorities(ctx, tx, at)
	if err != nil {
		return HoldCancelResult{}, err
	}
	filter := in.Filter
	filter.Now = at
	adj, held, err := heldReadyBeads(ctx, tx, filter, at, coef, raised)
	if err != nil {
		return HoldCancelResult{}, err
	}
	cancelAfter := coef.Duration("cancel_after")
	targets := slices.DeleteFunc(held, func(b Bead) bool {
		return adj.HeldFrom[b.ID].Add(cancelAfter).After(atT)
	})
	slices.SortFunc(targets, func(a, b Bead) int { return compareAdjusted(adj, a, b) })
	for _, b := range targets {
		res.Cancelled = append(res.Cancelled, HeldCancel{Bead: b, HeldSince: adj.HeldFrom[b.ID], Progress: adj.HeldSince[b.ID]})
	}

	res.Newly, err = NewlyReadyDiff(ctx, tx, func() error {
		for _, c := range res.Cancelled {
			reason := fmt.Sprintf("hold-cancel: held since %s, no progress since %s (%s); undo: lm update %s --reopen",
				FormatTime(c.HeldSince), FormatTime(c.Progress), atT.Sub(c.Progress).Round(time.Second), in.DisplayID(c.Bead))
			_, _, err := UpdateBeadAutoResolve(ctx, tx, UpdateInput{
				ID: c.Bead.ID, Cancel: true, Priority: -1, Reason: reason, Actor: in.Actor, Now: in.Now, Clock: in.Clock,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return res, err
}
