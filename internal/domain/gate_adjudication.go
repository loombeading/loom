// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

const AdjudicatedByPrefix = "adjudicated-by:"

var ErrGateNotAdjudicated = errors.New("a human/confirm Gate needs adjudicated-by:<closed adjudicate Gate>; create it with --kind adjudicate")

const (
	AdjudicationMissing    = 1
	AdjudicationUnresolved = 2
	AdjudicationNotJudge   = 3
	AdjudicationNotClosed  = 4
)

type GateNotAdjudicatedError struct{ Path int }

func (e *GateNotAdjudicatedError) Error() string { return ErrGateNotAdjudicated.Error() }
func (e *GateNotAdjudicatedError) Unwrap() error { return ErrGateNotAdjudicated }

func isPersonKindLabel(l string) bool { return l == "kind:human" || l == "kind:confirm" }

func adjudicationRequired(before, after []string) bool {
	if !slices.ContainsFunc(after, isPersonKindLabel) {
		return false
	}
	for _, l := range after {
		if isPersonKindLabel(l) && !slices.Contains(before, l) {
			return true
		}
	}
	for _, l := range before {
		if strings.HasPrefix(l, AdjudicatedByPrefix) && !slices.Contains(after, l) {
			return true
		}
	}
	return false
}

func checkGateAdjudication(ctx context.Context, tx *sql.Tx, before, after []string) ([]string, error) {
	if !adjudicationRequired(before, after) {
		return after, nil
	}
	out := make([]string, 0, len(after))
	found := false
	for _, l := range after {
		ref, ok := strings.CutPrefix(l, AdjudicatedByPrefix)
		if !ok {
			out = append(out, l)
			continue
		}
		found = true
		id, err := ResolveID(ctx, tx, ref)
		if err != nil {
			return nil, &GateNotAdjudicatedError{Path: AdjudicationUnresolved}
		}
		b, err := GetBead(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if b.BeadType != BeadTypeGate || !slices.Contains(b.Labels, "kind:adjudicate") {
			return nil, &GateNotAdjudicatedError{Path: AdjudicationNotJudge}
		}
		if b.Status != StatusClosed {
			return nil, &GateNotAdjudicatedError{Path: AdjudicationNotClosed}
		}
		out = append(out, AdjudicatedByPrefix+id)
	}
	if !found {
		return nil, &GateNotAdjudicatedError{Path: AdjudicationMissing}
	}
	return NormalizeLabels(out), nil
}
