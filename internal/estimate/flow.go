// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package estimate

import (
	"math"
	"slices"
	"time"
)

const FlowMinSamples = 10

const (
	flowMedianRank = 0.5
	flowTailRank   = 0.9
)

type FlowSample struct {
	ID        string
	CreatedAt time.Time
	ClosedAt  time.Time
	ClaimedAt time.Time
	ReadyAt   time.Time
	Reopened  bool
}

type FlowStat struct {
	N   int
	P50 time.Duration
	P90 time.Duration
}

type Flow struct {
	Closed   int
	Reopened int
	Lead     FlowStat
	Cycle    FlowStat
	Ready    FlowStat
}

func ComputeFlow(from, to time.Time, samples []FlowSample) Flow {
	var f Flow
	var lead, cycle, ready []time.Duration
	for _, s := range samples {
		if !s.ClosedAt.After(from) || s.ClosedAt.After(to) {
			continue
		}
		f.Closed++
		if s.Reopened {
			f.Reopened++
		}
		lead = append(lead, s.ClosedAt.Sub(s.CreatedAt))
		if s.ClaimedAt.IsZero() {
			continue
		}
		cycle = append(cycle, s.ClosedAt.Sub(s.ClaimedAt))
		ready = append(ready, s.ClaimedAt.Sub(s.ReadyAt))
	}
	f.Lead = flowStat(lead)
	f.Cycle = flowStat(cycle)
	f.Ready = flowStat(ready)
	return f
}

func flowStat(d []time.Duration) FlowStat {
	st := FlowStat{N: len(d)}
	if st.N < FlowMinSamples {
		return st
	}
	slices.Sort(d)
	st.P50 = nearestRank(d, flowMedianRank)
	st.P90 = nearestRank(d, flowTailRank)
	return st
}

func nearestRank(sorted []time.Duration, p float64) time.Duration {
	return sorted[int(math.Ceil(p*float64(len(sorted))))-1]
}
