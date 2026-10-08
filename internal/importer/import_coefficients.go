// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/loombeading/loom/internal/domain"
)

type policyCandidate struct {
	value string
	at    string
}

func importCoefficients(ctx context.Context, tx *sql.Tx, rows []importRow, opt Options, sum *Summary) error {
	best := map[string]policyCandidate{}
	for _, row := range rows {
		key, err := rawString(row["key"])
		if err != nil || key == "" {
			return fmt.Errorf("import: coefficient row missing key: %w", err)
		}
		value, err := rawString(row["value"])
		if err != nil {
			return fmt.Errorf("import: coefficient %s value: %w", key, err)
		}
		if err := domain.ValidateCoefficientValue(key, value); err != nil {
			return fmt.Errorf("import: %w", err)
		}
		at, err := rawAtMap(row)
		if err != nil {
			return err
		}
		ts, err := rawString(at["value"])
		if err != nil {
			return fmt.Errorf("import: coefficient %s _set_at.value: %w", key, err)
		}
		cand := policyCandidate{value: value, at: ts}
		if cur, ok := best[key]; !ok || policyWins(cand, cur) {
			best[key] = cand
		}
	}

	keys := make([]string, 0, len(best))
	for k := range best {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cand := best[key]
		var cur policyCandidate
		err := tx.QueryRowContext(ctx, `SELECT value, value_set_at FROM coefficients WHERE key = ?`, key).Scan(&cur.value, &cur.at)
		switch {
		case err == sql.ErrNoRows:
		case err != nil:
			return fmt.Errorf("import: load coefficient %s: %w", key, err)
		case !policyWins(cand, cur):
			continue
		case cand.value == cur.value && cand.at == cur.at:
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO coefficients (key, value, value_set_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, value_set_at = excluded.value_set_at`,
			key, cand.value, cand.at); err != nil {
			return fmt.Errorf("import: write coefficient %s: %w", key, err)
		}
		if err := domain.Audit(ctx, tx, domain.AuditRecord{
			OccurredAt: opt.Now, Actor: opt.Actor,
			Kind: domain.AuditKindMerge, Field: key, NewValue: cand.value, Origin: domain.OriginImport,
		}); err != nil {
			return err
		}
		sum.UpdatedCoefficients++
	}
	return nil
}

func policyWins(cand, cur policyCandidate) bool {
	if cand.at != cur.at {
		return cand.at > cur.at
	}
	return cand.value > cur.value
}
