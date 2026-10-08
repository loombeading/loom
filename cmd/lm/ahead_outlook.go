// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/estimate"
)

const outlookStages = 5

type outlookGates struct {
	date     int
	lastDate string
	human    int
	judge    int
}

type outlookInput struct {
	closed    int
	created   [outlookStages]int
	remaining [outlookStages]int
	gates     [outlookStages]outlookGates
}

func (v *aheadView) outlookInput(now time.Time, raised map[string]domain.EffectivePriority, readyIDs []string, inProgress []domain.Bead, gateWaiting []estimate.GateWait, gateEntries []domain.GateListEntry) outlookInput {
	var in outlookInput
	effective := func(id string) int {
		if e, ok := raised[id]; ok {
			return e.Priority
		}
		return v.domainBeads[id].Priority
	}
	stage := func(p int) int { return min(max(p, 0), outlookStages-1) }

	windowStart := now.Add(-releaseWindowHours * time.Hour)
	in.closed, _ = closeVelocityCounts(v.throughput, now, releaseWindowHours)
	for _, tb := range v.throughput {
		if !tb.CreatedAt.After(windowStart) || tb.CreatedAt.After(now) {
			continue
		}
		b := v.domainBeads[tb.ID]
		switch b.Status {
		case domain.StatusCancelled:
		case domain.StatusClosed:
			in.created[stage(b.Priority)]++
		default:
			in.created[stage(effective(tb.ID))]++
		}
	}

	for _, id := range v.keepInNamespace(readyIDs) {
		if v.domainBeads[id].BeadType != domain.BeadTypeGate {
			in.remaining[stage(effective(id))]++
		}
	}
	for _, b := range inProgress {
		in.remaining[stage(effective(b.ID))]++
	}

	gateKindByID := gateKindMap(gateEntries)
	gateLabelsByID := gateLabelsMap(gateEntries)
	var seen [outlookStages]map[string]bool
	for _, gw := range gateWaiting {
		if !v.inNamespace(gw.BeadID) {
			continue
		}
		k := stage(effective(gw.BeadID))
		if seen[k] == nil {
			seen[k] = map[string]bool{}
		}
		for _, gid := range gw.GateIDs {
			if seen[k][gid] {
				continue
			}
			seen[k][gid] = true
			g := &in.gates[k]
			if dates := gateWaitDates(gateLabelsByID[gid]); len(dates) > 0 {
				g.date++
				for _, d := range dates {
					g.lastDate = max(g.lastDate, d)
				}
			}
			switch gateKindByID[gid] {
			case domain.GateKindHuman:
				g.human++
			case domain.GateKindAdjudicate:
				g.judge++
			}
		}
	}
	return in
}

func outlookSection(now time.Time, in outlookInput) []string {
	createdTotal := 0
	for _, c := range in.created {
		createdTotal += c
	}
	perClose := "n/a"
	if in.closed > 0 {
		perClose = fmt.Sprintf("%.2f", float64(createdTotal)/float64(in.closed))
	}
	days := float64(releaseWindowHours) / 24
	sec := appendHeading(nil, "## Outlook")
	sec = append(sec, fmt.Sprintf("- Window: %.0fd closed=%d created=%d → %.2f/day in %.2f/day, %s created per close",
		days, in.closed, createdTotal, float64(in.closed)/days, float64(createdTotal)/days, perClose))

	stop := make([]estimate.ReleaseWindow, outlookStages)
	cont := make([]estimate.ReleaseWindow, outlookStages)
	for k := range outlookStages {
		stop[k] = estimate.ReleaseWindow{Remaining: in.remaining[k]}
		cont[k] = estimate.ReleaseWindow{Remaining: in.remaining[k], Created: in.created[k]}
	}
	stopETA := estimate.ReleaseETA(now, releaseWindowHours, in.closed, in.closed, stop)
	contETA := estimate.ReleaseETA(now, releaseWindowHours, in.closed, in.closed, cont)

	cum, rows := 0, 0
	for k := range outlookStages {
		cum += in.remaining[k]
		if in.remaining[k] == 0 {
			continue
		}
		rows++
		line := fmt.Sprintf("- P%d: n=%d cum=%d stop=%s cont=%s", k, in.remaining[k], cum, outlookWhen(in.closed, stopETA[k]), outlookWhen(in.closed, contETA[k]))
		sec = append(sec, line+in.gates[k].suffix())
	}
	if rows == 0 {
		sec = append(sec, "(none)")
	}
	return sec
}

func outlookWhen(closed int, r estimate.ReleaseETAResult) string {
	switch {
	case closed == 0:
		return "見込み無し"
	case r.EarlyDiverges:
		return "発散"
	}
	return r.Early.In(jst).Format("2006-01-02(Mon) 15:04 JST")
}

func (g outlookGates) suffix() string {
	var parts []string
	if g.date > 0 {
		parts = append(parts, fmt.Sprintf("date=%d(%s)", g.date, g.lastDate))
	}
	if g.human > 0 {
		parts = append(parts, fmt.Sprintf("human=%d", g.human))
	}
	if g.judge > 0 {
		parts = append(parts, fmt.Sprintf("judge=%d", g.judge))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  gates: " + strings.Join(parts, " ")
}
