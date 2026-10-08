// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"slices"
	"strings"
	"unicode"
)

const SimilarLimit = 5

type SimilarMatch struct {
	Bead Bead
	K, N int
}

func SimilarWords(s string) map[string]struct{} {
	words := map[string]struct{}{}
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		if run[0] <= unicode.MaxASCII {
			if len(run) >= 2 {
				words[strings.ToLower(string(run))] = struct{}{}
			}
		} else {
			for i := 0; i+1 < len(run); i++ {
				words[string(run[i:i+2])] = struct{}{}
			}
		}
		run = run[:0]
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(run) > 0 && (run[0] <= unicode.MaxASCII) != (r <= unicode.MaxASCII) {
			flush()
		}
		run = append(run, r)
	}
	flush()
	return words
}

func SimilarBeads(ctx context.Context, q Querier, namespace, title, newID string) ([]SimilarMatch, error) {
	t := SimilarWords(title)
	n := len(t)
	if n < 2 {
		return nil, nil
	}
	beads, err := queryBeads(ctx, q, "SELECT "+listRowCols+` FROM beads
		WHERE namespace = ? AND status IN (?, ?) AND type != ? AND id != ?
		AND id NOT IN (SELECT depends_on_id FROM dependencies WHERE bead_id = ? AND removed = 0)`+listOrderClause,
		[]any{namespace, StatusOpen, StatusInProgress, BeadTypeGate, newID, newID}, "")
	if err != nil {
		return nil, err
	}
	var out []SimilarMatch
	for _, b := range beads {
		c := SimilarWords(b.Title + "\n" + b.Description.String)
		k := 0
		for w := range t {
			if _, ok := c[w]; ok {
				k++
			}
		}
		if k >= 2 && 2*k >= n {
			out = append(out, SimilarMatch{Bead: b, K: k, N: n})
		}
	}
	slices.SortStableFunc(out, func(a, b SimilarMatch) int { return b.K - a.K })
	if len(out) > SimilarLimit {
		out = out[:SimilarLimit]
	}
	return out, nil
}
