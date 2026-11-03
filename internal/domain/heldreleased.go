// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"fmt"
	"slices"
)

const demandReachFormat = ` AND i.id IN (WITH RECURSIVE sub(id) AS (
	SELECT id FROM beads WHERE id IN (%s)
	UNION
	SELECT d.depends_on_id FROM dependencies d JOIN sub s ON d.bead_id = s.id
	WHERE d.type = ? AND d.removed = 0
	UNION
	SELECT d.bead_id FROM dependencies d JOIN sub s ON d.depends_on_id = s.id
	WHERE d.type = ? AND d.removed = 0
) SELECT id FROM sub)`

func HeldReleased(ctx context.Context, q Querier, now string, endpoints []string, fn func() error) ([]string, bool, error) {
	coef, err := LoadCoefficients(ctx, q)
	if err != nil {
		return nil, false, err
	}
	if coef[CoefficientTimeAdjust] != TimeAdjustOn {
		return nil, false, fn()
	}
	reach := make([]any, 0, len(endpoints)+2)
	for _, id := range endpoints {
		reach = append(reach, id)
	}
	reach = append(reach, BlocksDepType, ParentChildDepType)
	_, before, err := readyHeldWhere(ctx, q, now, coef, fmt.Sprintf(demandReachFormat, inPlaceholders(len(endpoints))), reach)
	if err != nil {
		return nil, false, err
	}
	if err := fn(); err != nil {
		return nil, false, err
	}
	if len(before) == 0 {
		return nil, true, nil
	}
	ids := make([]any, 0, len(before))
	for id := range before {
		ids = append(ids, id)
	}
	ready, after, err := readyHeldWhere(ctx, q, now, coef, ` AND i.id IN (`+inPlaceholders(len(ids))+`)`, ids)
	if err != nil {
		return nil, false, err
	}
	var released []string
	for id := range ready {
		if !after[id] {
			released = append(released, id)
		}
	}
	slices.Sort(released)
	return released, true, nil
}

func readyHeldWhere(ctx context.Context, q Querier, now string, coef Coefficients, clause string, clauseArgs []any) (map[string]bool, map[string]bool, error) {
	fromWhere, args := ReadyWhere(ReadyFilter{Priority: -1})
	keys, err := queryBeads(ctx, q, "SELECT "+readyKeyCols+" "+fromWhere+clause, append(args, clauseArgs...), "")
	if err != nil {
		return nil, nil, err
	}
	raised, err := RaisedPriorities(ctx, q, now)
	if err != nil {
		return nil, nil, err
	}
	adj, err := TimeAdjust(ctx, q, now, coef, raised, keys)
	if err != nil {
		return nil, nil, err
	}
	ready, held := make(map[string]bool, len(keys)), map[string]bool{}
	for _, b := range keys {
		ready[b.ID] = true
		if adj.Held[b.ID] {
			held[b.ID] = true
		}
	}
	return ready, held, nil
}
