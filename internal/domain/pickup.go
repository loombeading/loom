// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"fmt"
	"time"
)

func PickupAhead(ctx context.Context, q Querier, id string, now time.Time, windowHours float64) (bool, int, int, error) {
	beads, raised, _, err := ReadyBeadsEffective(ctx, q, ReadyFilter{Priority: -1})
	if err != nil {
		return false, 0, 0, err
	}

	idx := -1
	for i, b := range beads {
		if b.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, 0, 0, nil
	}

	self := beads[idx]
	selfEff := effectiveOf(raised, self.ID, self.Priority)
	windowStart := now.Add(-time.Duration(windowHours * float64(time.Hour)))
	var interrupts int
	for _, b := range beads[:idx] {
		if effectiveOf(raised, b.ID, b.Priority) >= selfEff {
			continue
		}
		createdAt, perr := time.Parse(time.RFC3339, b.CreatedAt)
		if perr != nil {
			return false, 0, 0, fmt.Errorf("pickup ahead: parse created_at %q: %w", b.CreatedAt, perr)
		}
		if createdAt.After(windowStart) && !createdAt.After(now) {
			interrupts++
		}
	}
	return true, idx, interrupts, nil
}
