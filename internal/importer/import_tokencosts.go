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

func importTokenCosts(ctx context.Context, tx *sql.Tx, rows []importRow, opt Options, sum *Summary) error {
	seen := map[string]bool{}
	var order []string
	byID := map[string]importRow{}
	for _, row := range rows {
		id, err := rawString(row["id"])
		if err != nil || id == "" {
			return fmt.Errorf("import: token_cost row missing id: %w", err)
		}
		for k := range row {
			if !importKnownTokenCostKeys[k] {
				return fmt.Errorf("import: token_cost row has unknown key %q", k)
			}
		}
		if !seen[id] {
			seen[id] = true
			order = append(order, id)
			byID[id] = row
		}
	}
	sort.Strings(order)

	for _, id := range order {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM token_costs WHERE id = ?`, id).Scan(&exists); err == nil {
			continue
		} else if err != sql.ErrNoRows {
			return fmt.Errorf("import: load token_cost: %w", err)
		}
		row := byID[id]
		beadID, _ := rawString(row["bead_id"])
		actorVal, _ := rawString(row["actor"])
		recordedAt, _ := rawString(row["recorded_at"])
		tokensIn, err := rawInt(row["tokens_in"])
		if err != nil {
			return fmt.Errorf("import: token_cost tokens_in: %w", err)
		}
		tokensOut, err := rawInt(row["tokens_out"])
		if err != nil {
			return fmt.Errorf("import: token_cost tokens_out: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO token_costs (id, bead_id, actor, recorded_at, tokens_in, tokens_out)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, beadID, actorVal, recordedAt, tokensIn, tokensOut); err != nil {
			return fmt.Errorf("import: insert token_cost %s: %w", id, err)
		}
		if err := domain.Audit(ctx, tx, domain.AuditRecord{
			OccurredAt: opt.Now, Actor: opt.Actor, BeadID: beadID,
			Kind: domain.AuditKindMerge, Field: "token_cost", Origin: domain.OriginImport,
			NewValue: fmt.Sprintf(`{"id":%q,"tokens_in":%d,"tokens_out":%d}`, id, tokensIn, tokensOut),
		}); err != nil {
			return err
		}
		sum.NewTokenCosts++
	}
	return nil
}

var importKnownTokenCostKeys = map[string]bool{
	"_type": true, "id": true, "bead_id": true, "actor": true,
	"recorded_at": true, "tokens_in": true, "tokens_out": true,
}
