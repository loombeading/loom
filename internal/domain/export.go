// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
)

type ExportRow = map[string]any

func setIf(row ExportRow, key string, valid bool, val any) {
	if valid {
		row[key] = val
	}
}

const exportBeadCols = `
	id, namespace, namespace_set_at,
	title, title_set_at,
	description, description_set_at,
	status, status_set_at,
	priority, priority_set_at,
	type, type_set_at,
	labels, labels_set_at,
	claimed_by, claimed_by_set_at,
	claim_expires_at, claim_expires_at_set_at,
	summary, summary_set_at,
	external_refs, external_refs_set_at,
	reasoning_depth, reasoning_depth_set_at,
	created_at, updated_at,
	closed_at, closed_at_set_at,
	severity, severity_set_at,
	due_at, due_at_set_at,
	expedite_until, expedite_until_set_at,
	expedite_reason, expedite_reason_set_at,
	redetect_key, redetect_key_set_at,
	last_redetected_at, last_redetected_at_set_at,
	revived_at, revived_at_set_at
`

func ExportBeads(ctx context.Context, q Querier, emit func(ExportRow) error) error {
	rows, err := q.QueryContext(ctx, `SELECT `+exportBeadCols+` FROM beads ORDER BY id ASC`)
	if err != nil {
		return fmt.Errorf("export beads: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id, namespace, title, status, beadType, createdAt, updatedAt string
			namespaceAt, titleAt, statusAt, beadTypeAt, priorityAt       sql.NullString
			description, descriptionAt                                   sql.NullString
			labels, labelsAt                                             sql.NullString
			claimedBy, claimedByAt                                       sql.NullString
			claimExpiresAt, claimExpiresAtAt                             sql.NullString
			summary, summaryAt                                           sql.NullString
			externalRefs, externalRefsAt                                 sql.NullString
			depth                                                        sql.NullInt64
			depthAt                                                      sql.NullString
			closedAt, closedAtAt                                         sql.NullString
			priority                                                     int64
			severity                                                     int64
			severityAt                                                   sql.NullString
			opt                                                          [6]sql.NullString
			optAt                                                        [6]sql.NullString
		)
		if err := rows.Scan(
			&id, &namespace, &namespaceAt,
			&title, &titleAt,
			&description, &descriptionAt,
			&status, &statusAt,
			&priority, &priorityAt,
			&beadType, &beadTypeAt,
			&labels, &labelsAt,
			&claimedBy, &claimedByAt,
			&claimExpiresAt, &claimExpiresAtAt,
			&summary, &summaryAt,
			&externalRefs, &externalRefsAt,
			&depth, &depthAt,
			&createdAt, &updatedAt,
			&closedAt, &closedAtAt,
			&severity, &severityAt,
			&opt[0], &optAt[0], &opt[1], &optAt[1], &opt[2], &optAt[2],
			&opt[3], &optAt[3], &opt[4], &optAt[4], &opt[5], &optAt[5],
		); err != nil {
			return fmt.Errorf("export beads: scan: %w", err)
		}

		decodedLabels, err := decodeStringList(labels)
		if err != nil {
			return err
		}
		if decodedLabels == nil {
			decodedLabels = []string{}
		}
		decodedExternalRefs, err := decodeStringList(externalRefs)
		if err != nil {
			return err
		}
		if decodedExternalRefs == nil {
			decodedExternalRefs = []string{}
		}
		row := ExportRow{
			"_type":         "bead",
			"id":            id,
			"namespace":     namespace,
			"title":         title,
			colStatus:       status,
			"priority":      priority,
			"severity":      severity,
			"type":          beadType,
			"labels":        decodedLabels,
			"external_refs": decodedExternalRefs,
			"created_at":    createdAt,
			"updated_at":    updatedAt,
		}
		setIf(row, "description", description.Valid, description.String)
		setIf(row, "claimed_by", claimedBy.Valid, claimedBy.String)
		setIf(row, colClaimExpiresAt, claimExpiresAt.Valid, claimExpiresAt.String)
		setIf(row, "summary", summary.Valid, summary.String)
		setIf(row, "reasoning_depth", depth.Valid, depth.Int64)
		setIf(row, colClosedAt, closedAt.Valid, closedAt.String)
		optKeys := [6]string{"due_at", "expedite_until", "expedite_reason", "redetect_key", "last_redetected_at", "revived_at"}
		for i, key := range optKeys {
			setIf(row, key, opt[i].Valid, opt[i].String)
		}

		at := ExportRow{}
		setIf(at, "namespace", namespaceAt.Valid, namespaceAt.String)
		setIf(at, "title", titleAt.Valid, titleAt.String)
		setIf(at, "description", descriptionAt.Valid, descriptionAt.String)
		setIf(at, colStatus, statusAt.Valid, statusAt.String)
		setIf(at, "priority", priorityAt.Valid, priorityAt.String)
		setIf(at, "type", beadTypeAt.Valid, beadTypeAt.String)
		setIf(at, "labels", labelsAt.Valid, labelsAt.String)
		setIf(at, "claimed_by", claimedByAt.Valid, claimedByAt.String)
		setIf(at, colClaimExpiresAt, claimExpiresAtAt.Valid, claimExpiresAtAt.String)
		setIf(at, "summary", summaryAt.Valid, summaryAt.String)
		setIf(at, "external_refs", externalRefsAt.Valid, externalRefsAt.String)
		setIf(at, "reasoning_depth", depthAt.Valid, depthAt.String)
		setIf(at, colClosedAt, closedAtAt.Valid, closedAtAt.String)
		setIf(at, "severity", severityAt.Valid, severityAt.String)
		for i, key := range optKeys {
			setIf(at, key, optAt[i].Valid, optAt[i].String)
		}
		row["_set_at"] = at

		if err := emit(row); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("export beads: iterate: %w", err)
	}
	return nil
}

func ExportDependencies(ctx context.Context, q Querier, emit func(ExportRow) error) error {
	rows, err := q.QueryContext(ctx, `
		SELECT bead_id, depends_on_id, type, created_at, removed, removed_set_at
		FROM dependencies
		ORDER BY bead_id ASC, depends_on_id ASC, type ASC`)
	if err != nil {
		return fmt.Errorf("export dependencies: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var beadID, dependsOnID, typ, createdAt string
		var removed bool
		var removedAt sql.NullString
		if err := rows.Scan(&beadID, &dependsOnID, &typ, &createdAt, &removed, &removedAt); err != nil {
			return fmt.Errorf("export dependencies: scan: %w", err)
		}
		row := ExportRow{
			"_type":         "dependency",
			"bead_id":       beadID,
			"depends_on_id": dependsOnID,
			"type":          typ,
			"created_at":    createdAt,
			"removed":       removed,
		}
		at := ExportRow{}
		setIf(at, "removed", removedAt.Valid, removedAt.String)
		row["_set_at"] = at
		if err := emit(row); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("export dependencies: iterate: %w", err)
	}
	return nil
}

func ExportTokenCosts(ctx context.Context, q Querier, emit func(ExportRow) error) error {
	rows, err := q.QueryContext(ctx, `
		SELECT id, bead_id, actor, recorded_at, tokens_in, tokens_out
		FROM token_costs
		ORDER BY id ASC`)
	if err != nil {
		return fmt.Errorf("export token_costs: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id, beadID, actor, recordedAt string
		var tokensIn, tokensOut int64
		if err := rows.Scan(&id, &beadID, &actor, &recordedAt, &tokensIn, &tokensOut); err != nil {
			return fmt.Errorf("export token_costs: scan: %w", err)
		}
		row := ExportRow{
			"_type":       "token_cost",
			"id":          id,
			"bead_id":     beadID,
			"actor":       actor,
			"recorded_at": recordedAt,
			"tokens_in":   tokensIn,
			"tokens_out":  tokensOut,
		}
		if err := emit(row); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("export token_costs: iterate: %w", err)
	}
	return nil
}

func ExportCoefficients(ctx context.Context, q Querier, emit func(ExportRow) error) error {
	rows, err := q.QueryContext(ctx, `SELECT key, value, value_set_at FROM coefficients ORDER BY key ASC`)
	if err != nil {
		return fmt.Errorf("export coefficients: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var key, value, valueAt string
		if err := rows.Scan(&key, &value, &valueAt); err != nil {
			return fmt.Errorf("export coefficients: scan: %w", err)
		}
		row := ExportRow{
			"_type":   "coefficient",
			"key":     key,
			"value":   value,
			"_set_at": ExportRow{"value": valueAt},
		}
		if err := emit(row); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("export coefficients: iterate: %w", err)
	}
	return nil
}

func Export(ctx context.Context, q Querier, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	emit := func(row ExportRow) error { return enc.Encode(row) }

	if err := ExportBeads(ctx, q, emit); err != nil {
		return err
	}
	if err := ExportDependencies(ctx, q, emit); err != nil {
		return err
	}
	if err := ExportTokenCosts(ctx, q, emit); err != nil {
		return err
	}
	return ExportCoefficients(ctx, q, emit)
}
