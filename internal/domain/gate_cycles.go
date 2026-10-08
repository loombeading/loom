// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

type GateCycleReport struct {
	Cycles  []string
	NoOwner []string
}

func (r GateCycleReport) Empty() bool { return len(r.Cycles) == 0 && len(r.NoOwner) == 0 }

type reportLine struct{ key, text string }

func GateCycles(ctx context.Context, tx *sql.Tx) (GateCycleReport, error) {
	gates, err := openGateLabels(ctx, tx)
	if err != nil {
		return GateCycleReport{}, err
	}
	g := &waitGraph{ctx: ctx, tx: tx}
	var cycles, noOwner []reportLine
	for _, gate := range gates {
		for _, x := range WaitTargets(gate.list) {
			owners, err := g.ownersOf(x)
			if err != nil {
				return GateCycleReport{}, err
			}
			if len(owners) == 0 {
				d, err := DisplayIDFor(ctx, tx, gate.id)
				if err != nil {
					return GateCycleReport{}, err
				}
				noOwner = append(noOwner, reportLine{gate.id + "\x00" + x, fmt.Sprintf("%s %q", d, x)})
				continue
			}
			for _, p := range owners {
				path, err := findPath(ctx, tx, g, p, gate.id, BlocksDepType)
				if err != nil {
					return GateCycleReport{}, err
				}
				if path == nil {
					continue
				}
				slices.Reverse(path)
				path = append(path, gate.id)
				display, err := displayIDsFor(ctx, tx, path)
				if err != nil {
					return GateCycleReport{}, err
				}
				cycles = append(cycles, reportLine{strings.Join(path, "\x00"), strings.Join(display, " -> ")})
			}
		}
	}
	return GateCycleReport{Cycles: sortedTexts(cycles), NoOwner: sortedTexts(noOwner)}, nil
}

func openGateLabels(ctx context.Context, tx *sql.Tx) ([]idList, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, labels FROM beads WHERE type = 'gate' AND labels IS NOT NULL AND status NOT IN (?, ?) ORDER BY id`, StatusClosed, StatusCancelled)
	if err != nil {
		return nil, fmt.Errorf("gate cycles: query gates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []idList
	for rows.Next() {
		var e idList
		var raw sql.NullString
		err := rows.Scan(&e.id, &raw)
		if err == nil {
			e.list, err = decodeStringList(raw)
		}
		if err != nil {
			return nil, fmt.Errorf("gate cycles: read gate: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func sortedTexts(lines []reportLine) []string {
	slices.SortFunc(lines, func(a, b reportLine) int { return cmp.Compare(a.key, b.key) })
	lines = slices.CompactFunc(lines, func(a, b reportLine) bool { return a.key == b.key })
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
	}
	return out
}
