// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type TokenCost struct {
	ID         string
	BeadID     string
	Actor      string
	RecordedAt string
	TokensIn   int
	TokensOut  int
}

type AddTokenCostInput struct {
	BeadID    string
	TokensIn  int
	TokensOut int
	Reason    string
	Actor     string
	Now       string
}

func AddTokenCost(ctx context.Context, tx *sql.Tx, in AddTokenCostInput) error {
	if in.TokensIn < 0 || in.TokensOut < 0 {
		return &ValidationError{Msg: "tokens-in and tokens-out must not be negative"}
	}
	if in.TokensIn == 0 && in.TokensOut == 0 {
		return &ValidationError{Msg: "at least one of --in / --out must be non-zero"}
	}

	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM beads WHERE id = ?`, in.BeadID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBeadNotFound
	}
	if err != nil {
		return fmt.Errorf("add token cost: lookup: %w", err)
	}

	id := NewCanonicalID()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO token_costs (id, bead_id, actor, recorded_at, tokens_in, tokens_out)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, in.BeadID, in.Actor, in.Now, in.TokensIn, in.TokensOut,
	); err != nil {
		return fmt.Errorf("add token cost: insert: %w", err)
	}

	return Audit(ctx, tx, AuditRecord{
		OccurredAt: in.Now,
		Actor:      in.Actor,
		BeadID:     in.BeadID,
		Kind:       AuditKindField,
		Field:      "token_cost",
		NewValue:   fmt.Sprintf("%d/%d", in.TokensIn, in.TokensOut),
		Reason:     in.Reason,
	})
}

type TokenCostTotals struct {
	TokensIn  int
	TokensOut int
	Records   int
}

func (t TokenCostTotals) Total() int { return t.TokensIn + t.TokensOut }

func GetTokenCostTotals(ctx context.Context, db Querier, beadID string) (TokenCostTotals, error) {
	var totals TokenCostTotals
	var tokensIn, tokensOut sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(tokens_in), 0), COALESCE(SUM(tokens_out), 0)
		FROM token_costs WHERE bead_id = ?`, beadID,
	).Scan(&totals.Records, &tokensIn, &tokensOut)
	if err != nil {
		return TokenCostTotals{}, fmt.Errorf("get token cost totals: %w", err)
	}
	totals.TokensIn = int(tokensIn.Int64)
	totals.TokensOut = int(tokensOut.Int64)
	return totals, nil
}

const maxTokenCostTotalDepth = 64

func GetTokenCostTotal(ctx context.Context, db Querier, beadID string) (int, error) {
	var total sql.NullInt64
	err := db.QueryRowContext(ctx, `
		WITH RECURSIVE descendants(id, depth) AS (
			SELECT ?, 0
			UNION ALL
			SELECT d.bead_id, descendants.depth + 1
			FROM dependencies d
			JOIN descendants ON d.depends_on_id = descendants.id
			WHERE d.type = ? AND d.removed = 0 AND descendants.depth < ?
		)
		SELECT COALESCE(SUM(tc.tokens_in + tc.tokens_out), 0)
		FROM token_costs tc
		WHERE tc.bead_id IN (SELECT id FROM descendants)`,
		beadID, ParentChildDepType, maxTokenCostTotalDepth,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("get token cost total: %w", err)
	}
	return int(total.Int64), nil
}
