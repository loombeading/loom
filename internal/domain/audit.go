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
	AuditKindField = "field"
	AuditKindNote  = "note"
	AuditKindMerge = "merge"
)

const (
	OriginLocal  = "local"
	OriginImport = "import"
)

const (
	FieldClaim        = "claim"
	FieldRelease      = "release"
	FieldForceRelease = "force_release"
	FieldForceClaim   = "force_claim"

	FieldClaimTakeover = "claim_takeover"
	FieldReopen        = "reopen"
	FieldDependency    = "dependency"
	FieldRework        = "rework"
)

type AuditRecord struct {
	OccurredAt string
	Actor      string
	BeadID     string
	Kind       string
	Field      string
	NewValue   string
	Reason     string
	Origin     string
}

func Audit(ctx context.Context, tx *sql.Tx, rec AuditRecord) error {
	switch rec.Kind {
	case AuditKindField:
		if rec.Field == "" {
			return errors.New("audit: kind=field requires field")
		}
	case AuditKindNote:
		if rec.Reason == "" {
			return errors.New("audit: kind=note requires reason")
		}
	}
	if rec.Origin == "" {
		rec.Origin = OriginLocal
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_log (occurred_at, actor, bead_id, kind, field, new_value, reason, origin)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.OccurredAt, rec.Actor,
		nullIfEmpty(rec.BeadID),
		rec.Kind, nullIfEmpty(rec.Field), nullIfEmpty(rec.NewValue), nullIfEmpty(rec.Reason),
		rec.Origin,
	)
	if err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
