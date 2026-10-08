// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

var ReworkCauses = []string{"ci", "review", "followup"}

type AddReworkInput struct {
	BeadID string
	Cause  string
	Reason string
	Actor  string
	Now    string
}

func AddRework(ctx context.Context, tx *sql.Tx, in AddReworkInput) error {
	if in.Cause == "" {
		return &ValidationError{Msg: "--cause is required (ci, review, followup)"}
	}
	valid := slices.Contains(ReworkCauses, in.Cause)
	if !valid {
		return &ValidationError{Msg: fmt.Sprintf("invalid --cause %q (want ci, review, followup)", in.Cause)}
	}

	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM beads WHERE id = ?`, in.BeadID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBeadNotFound
	}
	if err != nil {
		return fmt.Errorf("add rework: lookup: %w", err)
	}

	return Audit(ctx, tx, AuditRecord{
		OccurredAt: in.Now,
		Actor:      in.Actor,
		BeadID:     in.BeadID,
		Kind:       AuditKindField,
		Field:      FieldRework,
		NewValue:   in.Cause,
		Reason:     in.Reason,
	})
}
