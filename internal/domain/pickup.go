// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"fmt"
	"time"
)

type PickupPosition struct {
	Ahead      int
	Interrupts int
}

func PickupPositions(ctx context.Context, q Querier, now time.Time, windowHours float64) (map[string]PickupPosition, error) {
	beads, raised, _, err := ReadyBeadsEffective(ctx, q, ReadyFilter{Priority: -1})
	if err != nil {
		return nil, err
	}

	windowStart := now.Add(-time.Duration(windowHours * float64(time.Hour)))
	eff := make([]int, len(beads))
	inWindow := make([]bool, len(beads))
	for i, b := range beads {
		eff[i] = effectiveOf(raised, b.ID, b.Priority)
		createdAt, perr := time.Parse(time.RFC3339, b.CreatedAt)
		if perr != nil {
			return nil, fmt.Errorf("pickup ahead: parse created_at %q: %w", b.CreatedAt, perr)
		}
		inWindow[i] = createdAt.After(windowStart) && !createdAt.After(now)
	}

	out := make(map[string]PickupPosition, len(beads))
	for i, b := range beads {
		var interrupts int
		for j := range i {
			if eff[j] < eff[i] && inWindow[j] {
				interrupts++
			}
		}
		out[b.ID] = PickupPosition{Ahead: i, Interrupts: interrupts}
	}
	return out, nil
}

func PickupAhead(ctx context.Context, q Querier, id string, now time.Time, windowHours float64) (bool, int, int, error) {
	positions, err := PickupPositions(ctx, q, now, windowHours)
	if err != nil {
		return false, 0, 0, err
	}
	p, ok := positions[id]
	return ok, p.Ahead, p.Interrupts, nil
}
