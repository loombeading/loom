// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/loombeading/loom/internal/domain"
)

type beadAcc struct {
	isNew      bool
	vals       map[string]beadColValue
	baseline   map[string]beadColValue
	considered map[string]bool
	createdAt  string
	updatedAt  string
}

func importBeads(ctx context.Context, tx *sql.Tx, rows []importRow, opt Options, sum *Summary) error {
	byID := map[string][]importRow{}
	var order []string
	for _, row := range rows {
		id, err := rawString(row["id"])
		if err != nil || id == "" {
			return fmt.Errorf("import: bead row missing id: %w", err)
		}
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = append(byID[id], row)
	}
	sort.Strings(order)

	for _, id := range order {
		acc, err := loadBeadAcc(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, row := range byID[id] {
			if err := foldBeadRow(acc, row); err != nil {
				return err
			}
		}
		changed, err := writeBeadAcc(ctx, tx, id, acc, opt)
		if err != nil {
			return err
		}
		if acc.isNew {
			sum.NewBeads++
		} else if changed {
			sum.UpdatedBeads++
		}
	}
	return nil
}

func loadBeadAcc(ctx context.Context, tx *sql.Tx, id string) (*beadAcc, error) {
	acc := &beadAcc{
		vals:       map[string]beadColValue{},
		baseline:   map[string]beadColValue{},
		considered: map[string]bool{},
	}

	var (
		namespace, title, status, beadType, createdAt, updatedAt string
		namespaceAt, titleAt, statusAt, beadTypeAt, priorityAt   sql.NullString
		description, descriptionAt                               sql.NullString
		labels, labelsAt                                         sql.NullString
		claimedBy, claimedByAt                                   sql.NullString
		claimExpiresAt, claimExpiresAtAt                         sql.NullString
		summary, summaryAt                                       sql.NullString
		externalRefs, externalRefsAt                             sql.NullString
		depth                                                    sql.NullInt64
		depthAt                                                  sql.NullString
		closedAt, closedAtAt                                     sql.NullString
		priority                                                 int64
		severity                                                 int64
		severityAt                                               sql.NullString
		optional                                                 [6]sql.NullString
		optionalAt                                               [6]sql.NullString
	)
	err := tx.QueryRowContext(ctx, `
		SELECT namespace, namespace_set_at, title, title_set_at, description, description_set_at,
		       status, status_set_at, priority, priority_set_at, type, type_set_at,
		       labels, labels_set_at, claimed_by, claimed_by_set_at,
		       claim_expires_at, claim_expires_at_set_at,
		       summary, summary_set_at,
		       external_refs, external_refs_set_at,
		       reasoning_depth, reasoning_depth_set_at,
		       created_at, updated_at, closed_at, closed_at_set_at,
		       severity, severity_set_at,
		       due_at, due_at_set_at, expedite_until, expedite_until_set_at,
		       expedite_reason, expedite_reason_set_at, redetect_key, redetect_key_set_at,
		       last_redetected_at, last_redetected_at_set_at, revived_at, revived_at_set_at
		FROM beads WHERE id = ?`, id,
	).Scan(
		&namespace, &namespaceAt, &title, &titleAt, &description, &descriptionAt,
		&status, &statusAt, &priority, &priorityAt, &beadType, &beadTypeAt,
		&labels, &labelsAt, &claimedBy, &claimedByAt,
		&claimExpiresAt, &claimExpiresAtAt,
		&summary, &summaryAt,
		&externalRefs, &externalRefsAt,
		&depth, &depthAt,
		&createdAt, &updatedAt, &closedAt, &closedAtAt,
		&severity, &severityAt,
		&optional[0], &optionalAt[0], &optional[1], &optionalAt[1],
		&optional[2], &optionalAt[2], &optional[3], &optionalAt[3],
		&optional[4], &optionalAt[4], &optional[5], &optionalAt[5],
	)
	if err == sql.ErrNoRows {
		acc.isNew = true
		return acc, nil
	}
	if err != nil {
		return nil, fmt.Errorf("import: load bead %s: %w", id, err)
	}

	set := func(col, val string, at sql.NullString) {
		if val == "" && !at.Valid {
			return
		}
		v := beadColValue{raw: val, rawBytes: []byte(val), ts: at.String}
		acc.vals[col] = v
		acc.baseline[col] = v
	}
	setInt := func(col string, val int64, at sql.NullString, present bool) {
		if !present {
			return
		}
		v := beadColValue{raw: val, rawBytes: []byte(strconv.FormatInt(val, 10)), ts: at.String}
		acc.vals[col] = v
		acc.baseline[col] = v
	}

	set("namespace", namespace, namespaceAt)
	set("title", title, titleAt)
	if description.Valid {
		set("description", description.String, descriptionAt)
	}
	set(colStatus, status, statusAt)
	setInt("priority", priority, priorityAt, true)
	set(colBeadType, beadType, beadTypeAt)
	if labels.Valid {
		set("labels", labels.String, labelsAt)
	}
	if claimedBy.Valid {
		set("claimed_by", claimedBy.String, claimedByAt)
	}
	if claimExpiresAt.Valid {
		set("claim_expires_at", claimExpiresAt.String, claimExpiresAtAt)
	}
	if summary.Valid {
		set("summary", summary.String, summaryAt)
	}
	if externalRefs.Valid {
		set("external_refs", externalRefs.String, externalRefsAt)
	}
	if depth.Valid {
		setInt(colReasoningDepth, depth.Int64, depthAt, true)
	}
	if closedAt.Valid {
		set("closed_at", closedAt.String, closedAtAt)
	}
	setInt("severity", severity, severityAt, true)
	for i, col := range []string{"due_at", "expedite_until", "expedite_reason", "redetect_key", "last_redetected_at", "revived_at"} {
		if optional[i].Valid {
			set(col, optional[i].String, optionalAt[i])
		}
	}
	acc.createdAt = createdAt
	acc.updatedAt = updatedAt
	return acc, nil
}

func foldBeadRow(acc *beadAcc, row importRow) error {
	rowUpdatedAt, err := rawString(row["updated_at"])
	if err != nil {
		return fmt.Errorf("import: bead row updated_at: %w", err)
	}
	foldBeadTimes(acc, row, rowUpdatedAt)

	at, err := rawAtMap(row)
	if err != nil {
		return err
	}

	for k := range row {
		if !importKnownBeadKeys[k] {
			return fmt.Errorf("import: bead row has unknown key %q", k)
		}
	}

	for _, c := range importBeadCols {
		raw, present := row[c.jsonKey]
		if !present {
			continue
		}

		val, keep, err := parseBeadCol(c.col, c.jsonKey, raw, at)
		if err != nil {
			return err
		}
		if !keep {
			continue
		}
		acc.considered[c.col] = true

		ts, err := rawString(at[c.jsonKey])
		if err != nil {
			return fmt.Errorf("import: bead _set_at.%s: %w", c.jsonKey, err)
		}
		if ts == "" {
			ts = rowUpdatedAt
		}
		val.ts = ts

		cur, hasCur := acc.vals[c.col]
		if lwwWins(val.ts, val.rawBytes, cur, hasCur) {
			acc.vals[c.col] = val
		}
	}
	return nil
}

func foldBeadTimes(acc *beadAcc, row importRow, rowUpdatedAt string) {
	if rowCreated, err := rawString(row["created_at"]); err == nil && rowCreated != "" {
		if acc.createdAt == "" || rowCreated < acc.createdAt {
			acc.createdAt = rowCreated
		}
	}
	if rowUpdatedAt > acc.updatedAt {
		acc.updatedAt = rowUpdatedAt
	}
}

func parseBeadCol(col, jsonKey string, raw json.RawMessage, at map[string]json.RawMessage) (beadColValue, bool, error) {
	switch col {
	case "labels":
		encoded, decoded, err := canonicalLabels(raw)
		if err != nil {
			return beadColValue{}, false, fmt.Errorf("import: bead labels: %w", err)
		}
		return listColValue(encoded, len(decoded), at["labels"])
	case "external_refs":
		encoded, decoded, err := canonicalExternalRefs(raw)
		if err != nil {
			return beadColValue{}, false, fmt.Errorf("import: bead external_refs: %w", err)
		}
		return listColValue(encoded, len(decoded), at["external_refs"])
	case "priority", colReasoningDepth, "severity":
		n, err := rawInt(raw)
		if err != nil {
			return beadColValue{}, false, fmt.Errorf("import: bead %s: %w", jsonKey, err)
		}
		if col == "priority" && (n < domain.MinPriority || n > domain.MaxPriority) {
			return beadColValue{}, false, fmt.Errorf("import: bead priority must be between %d and %d: %d", domain.MinPriority, domain.MaxPriority, n)
		}
		if col == "severity" && (n < domain.MinSeverity || n > domain.MaxSeverity) {
			return beadColValue{}, false, fmt.Errorf("import: bead severity must be between %d and %d: %d", domain.MinSeverity, domain.MaxSeverity, n)
		}
		if col == colReasoningDepth && n != 0 && (n < domain.MinReasoningDepth || n > domain.MaxReasoningDepth) {
			return beadColValue{}, false, fmt.Errorf("import: bead reasoning_depth must be between %d and %d: %d", domain.MinReasoningDepth, domain.MaxReasoningDepth, n)
		}
		return beadColValue{raw: n, rawBytes: raw}, true, nil
	default:
		return parseBeadStringCol(col, jsonKey, raw)
	}
}

func listColValue(encoded string, n int, at json.RawMessage) (beadColValue, bool, error) {
	if n == 0 && at == nil {
		return beadColValue{}, false, nil
	}
	return beadColValue{raw: encoded, rawBytes: []byte(encoded)}, true, nil
}

func parseBeadStringCol(col, jsonKey string, raw json.RawMessage) (beadColValue, bool, error) {
	s, err := rawString(raw)
	if err != nil {
		return beadColValue{}, false, fmt.Errorf("import: bead %s: %w", jsonKey, err)
	}
	if col == colStatus && !domain.ValidStatuses[s] {
		return beadColValue{}, false, fmt.Errorf("import: bead has out-of-vocabulary status %q", s)
	}
	if col == colBeadType && !domain.ValidBeadTypes[s] {
		return beadColValue{}, false, fmt.Errorf("import: bead has out-of-vocabulary type %q", s)
	}
	return beadColValue{raw: s, rawBytes: []byte(s)}, true, nil
}

func lwwWins(ts string, bytesVal []byte, cur beadColValue, hasCur bool) bool {
	if !hasCur {
		return true
	}
	if ts != cur.ts {
		return ts > cur.ts
	}
	return bytes.Compare(bytesVal, cur.rawBytes) > 0
}
