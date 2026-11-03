// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
)

const ifOnSuffix = "  [if time_adjust=on]"

func writePriority(ctx context.Context, w io.Writer, q domain.Querier, n domain.ShortIDs, now time.Time, bead domain.Bead, displayID string) error {
	bd, err := domain.BreakdownOf(ctx, q, domain.FormatTime(now), bead.ID)
	if err != nil {
		return err
	}
	ref := func(id string) (string, error) {
		resolver, err := domain.NewAliasResolverFor(ctx, q, []string{id})
		if err != nil {
			return "", err
		}
		s, err := sourceDisplayID(ctx, q, resolver, id, n)
		return output.EscapeInline(s), err
	}
	reason := func(e domain.EffectivePriority) (string, error) {
		switch {
		case e.Source == "":
			return "stored", nil
		case isLiteralSource(e.Source):
			return e.Source, nil
		}
		return ref(e.Source)
	}
	self := output.EscapeInline(displayID)

	fmt.Fprintln(w, "## Priority")
	fmt.Fprintf(w, "- stored: P%d\n", bd.Stored)
	floor := "none"
	if bd.Floor != 0 {
		floor = fmt.Sprintf("P%d", bd.Floor)
	}
	hold := "no"
	if bd.Holdable {
		hold = "yes"
	}
	fmt.Fprintf(w, "- severity: %d (ceiling=P%d floor=%s hold=%s)\n", bd.Severity, bd.Ceiling, floor, hold)

	if via, err := reason(bd.Graph); err != nil {
		return err
	} else if bd.Graph.Source == "" {
		fmt.Fprintf(w, "- graph: P%d (self)\n", bd.Graph.Priority)
	} else {
		fmt.Fprintf(w, "- graph: P%d via %s\n", bd.Graph.Priority, via)
	}

	switch {
	case bd.ExpediteActive:
		fmt.Fprintf(w, "- expedite: active until %s (%s)\n", domain.FormatTime(bd.ExpediteUntil), output.EscapeInline(bd.ExpediteReason))
	case !bd.ExpediteUntil.IsZero():
		fmt.Fprintf(w, "- expedite: expired at %s\n", domain.FormatTime(bd.ExpediteUntil))
	default:
		fmt.Fprintln(w, "- expedite: none")
	}

	if bd.Due.IsZero() {
		fmt.Fprintln(w, "- due: none")
	} else {
		state := "pending"
		if bd.DueActive {
			state = "active"
		}
		fmt.Fprintf(w, "- due: %s (lead from %s, %s)\n", domain.FormatTime(bd.Due), domain.FormatTime(bd.DueFrom), state)
	}

	var demand []string
	if bd.Demand.Milestone != "" {
		epic, err := ref(bd.Demand.Milestone)
		if err != nil {
			return err
		}
		demand = append(demand, "milestone "+epic)
	}
	if bd.Demand.Blocks != "" {
		down, err := ref(bd.Demand.Blocks)
		if err != nil {
			return err
		}
		demand = append(demand, "blocks "+down)
	}
	if bd.Demand.Due {
		demand = append(demand, "due")
	}
	if bd.Demand.Redetected != "" {
		demand = append(demand, "redetected "+output.EscapeInline(bd.Demand.Redetected))
	}
	if len(demand) == 0 {
		fmt.Fprintln(w, "- demand: none")
	} else {
		fmt.Fprintf(w, "- demand: %s\n", strings.Join(demand, ", "))
	}

	if !bd.Ready {
		fmt.Fprintln(w, "- ready_since: not ready")
	} else {
		src := bd.ReadySince.Kind
		if bd.ReadySince.ID != "" {
			id, err := ref(bd.ReadySince.ID)
			if err != nil {
				return err
			}
			src += " " + id
		}
		fmt.Fprintf(w, "- ready_since: %s (%s)\n", domain.FormatTime(bd.ReadySince.Time), src)
	}
	fmt.Fprintf(w, "- progress: %s (%s)\n", domain.FormatTime(bd.Progress.Time), bd.Progress.Kind)

	suffix := ""
	if bd.Mode != domain.TimeAdjustOn {
		suffix = ifOnSuffix
	}
	fmt.Fprintf(w, "- state: %s%s\n", bd.State, suffix)

	why, err := reason(bd.Effective)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "- effective: P%d (%s)\n", bd.Effective.Priority, why)
	if bd.EffectiveOn != nil {
		why, err := reason(*bd.EffectiveOn)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "- effective_if_on: P%d (%s)\n", bd.EffectiveOn.Priority, why)
	}

	if len(bd.Next) == 0 {
		fmt.Fprintln(w, "- next: none")
	}
	for _, t := range bd.Next {
		at, in := domain.FormatTime(t.At), formatIn(t.At.Sub(now))
		tail := ""
		if t.Timed {
			tail = suffix
		}
		switch t.Kind {
		case domain.TransitionExpedite:
			fmt.Fprintf(w, "- next: %s (in %s) expedite expires -> P%d\n", at, in, t.Priority)
		case domain.TransitionDue:
			fmt.Fprintf(w, "- next: %s (in %s) due lead -> P1\n", at, in)
		case domain.TransitionPromote:
			fmt.Fprintf(w, "- next: %s (in %s) promote -> P%d%s\n", at, in, t.Priority, tail)
		case domain.TransitionDecay:
			fmt.Fprintf(w, "- next: %s (in %s) decay -> P%d%s\n", at, in, t.Priority, tail)
		case domain.TransitionHold:
			fmt.Fprintf(w, "- next: %s (in %s) hold%s\n", at, in, tail)
		case domain.TransitionHoldCancel:
			fmt.Fprintf(w, "- next: %s (in %s) hold-cancel eligible%s\n", at, in, tail)
		}
	}

	if len(bd.Levers) == 0 {
		fmt.Fprintln(w, "- lever: none")
	}
	for _, l := range bd.Levers {
		switch l.Kind {
		case domain.LeverRevive:
			fmt.Fprintf(w, "- lever: lm update %s --revive --reason <理由> -> back to Ready\n", self)
		case domain.LeverDue:
			fmt.Fprintf(w, "- lever: lm update %s --due <時刻> -> demand\n", self)
		case domain.LeverBlocks:
			fmt.Fprintf(w, "- lever: lm dep add <下流> %s --type blocks -> demand\n", self)
		case domain.LeverSeverity:
			fmt.Fprintf(w, "- lever: lm update %s --severity %d -> ceiling P%d\n", self, l.Severity, l.Ceiling)
		case domain.LeverExpedite:
			fmt.Fprintf(w, "- lever: lm update %s --expedite <期間> --reason <理由> -> P0\n", self)
		}
	}
	fmt.Fprintln(w)
	return nil
}

func formatIn(d time.Duration) string {
	d = max(d, 0)
	return fmt.Sprintf("%dh%dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
}
