// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	MinShortLen        = 3
	DefaultShortLen    = 4
	shortIDRedrawLimit = 16
)

type ShortIDs map[string]string

func LoadShortIDs(ctx context.Context, q Querier) (ShortIDs, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, short_id FROM beads`)
	if err != nil {
		return nil, fmt.Errorf("short ids: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := ShortIDs{}
	for rows.Next() {
		var id string
		var short sql.NullString
		if err := rows.Scan(&id, &short); err != nil {
			return nil, fmt.Errorf("short ids: scan: %w", err)
		}
		out[id] = short.String
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("short ids: iterate: %w", err)
	}
	return out, nil
}

func DisplayID(namespace, canonical string, s ShortIDs) string {
	short := s[canonical]
	if short == "" {
		short = canonical[:min(DefaultShortLen, len(canonical))]
	}
	return namespace + "-" + short
}

func prefixTaken(ctx context.Context, q Querier, prefix string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM beads WHERE id GLOB ? AND status IN ('open', 'in_progress') LIMIT 1`, prefix+"*").Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("short id: check prefix: %w", err)
	}
	return true, nil
}

func allocateCanonicalID(ctx context.Context, q Querier, k int) (string, string, error) {
	k = min(max(k, MinShortLen), CanonicalIDLen)
	for misses := 0; ; {
		id := NewCanonicalID()
		taken, err := prefixTaken(ctx, q, id[:k])
		if err != nil {
			return "", "", err
		}
		if !taken {
			return id, id[:k], nil
		}
		if misses++; misses >= shortIDRedrawLimit && k < CanonicalIDLen {
			k++
			misses = 0
		}
	}
}

func ImportShortID(ctx context.Context, q Querier, canonical string, k int) (string, error) {
	k = min(max(k, MinShortLen), CanonicalIDLen)
	for l := k; l < CanonicalIDLen; l++ {
		taken, err := prefixTaken(ctx, q, canonical[:l])
		if err != nil {
			return "", err
		}
		if !taken {
			return canonical[:l], nil
		}
	}
	return canonical, nil
}
