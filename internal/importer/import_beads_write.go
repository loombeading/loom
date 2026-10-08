// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/loombeading/loom/internal/domain"
)

func writeBeadAcc(ctx context.Context, tx *sql.Tx, id string, acc *beadAcc, opt Options) (bool, error) {
	if acc.isNew {
		for _, req := range []string{"namespace", "title", colStatus, colBeadType} {
			if _, ok := acc.vals[req]; !ok {
				return false, fmt.Errorf("import: new bead %s missing required field %q", id, req)
			}
		}
		if _, ok := acc.vals["priority"]; !ok {
			acc.vals["priority"] = beadColValue{raw: int64(domain.DefaultPriority), rawBytes: []byte(strconv.Itoa(domain.DefaultPriority))}
		}
		if acc.createdAt == "" {
			acc.createdAt = acc.updatedAt
		}
	}

	changed := false
	var auditFields []domain.AuditRecord
	for _, c := range importBeadCols {
		if !acc.considered[c.col] {
			continue
		}
		v := acc.vals[c.col]
		baseline, hadBaseline := acc.baseline[c.col]
		if !hadBaseline || !beadColValueEqual(v, baseline) {
			changed = true
			auditFields = append(auditFields, domain.AuditRecord{Field: c.col, NewValue: beadValToString(v)})
		}
	}

	shortID := ""
	if acc.isNew {
		var err error
		if shortID, err = domain.ImportShortID(ctx, tx, id, opt.ShortLen); err != nil {
			return false, err
		}
	}
	if err := persistBead(ctx, tx, id, acc, changed, shortID); err != nil {
		return false, err
	}

	for i := range auditFields {
		auditFields[i].OccurredAt = opt.Now
		auditFields[i].Actor = opt.Actor
		auditFields[i].BeadID = id
		auditFields[i].Kind = domain.AuditKindMerge
		auditFields[i].Origin = domain.OriginImport
		if err := domain.Audit(ctx, tx, auditFields[i]); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func beadColValueEqual(a, b beadColValue) bool {
	return string(a.rawBytes) == string(b.rawBytes)
}

func beadValToString(v beadColValue) string {
	switch x := v.raw.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case string:
		return x
	default:
		return fmt.Sprintf("%v", x)
	}
}

func persistBead(ctx context.Context, tx *sql.Tx, id string, acc *beadAcc, changed bool, shortID string) error {
	cols := []string{}
	vals := []any{}
	setClauses := []string{}

	addCol := func(col string, at string, val any) {
		cols = append(cols, col, col+"_set_at")
		vals = append(vals, val, sqlNullable(at))
		setClauses = append(setClauses, col+" = ?", col+"_set_at = ?")
	}

	for _, c := range importBeadCols {
		v, ok := acc.vals[c.col]
		if !ok {
			continue
		}
		if !acc.considered[c.col] && !acc.isNew {
			continue
		}
		addCol(c.col, v.ts, v.raw)
	}

	if acc.isNew {
		cols = append(cols, "id", "created_at", "updated_at", "short_id")
		vals = append(vals, id, acc.createdAt, acc.updatedAt, shortID)
		placeholders := strings.Repeat("?,", len(cols))
		placeholders = strings.TrimSuffix(placeholders, ",")
		// #nosec G201
		q := fmt.Sprintf(`INSERT INTO beads (%s) VALUES (%s)`, strings.Join(cols, ", "), placeholders)
		if _, err := tx.ExecContext(ctx, q, vals...); err != nil {
			return fmt.Errorf("import: insert bead %s: %w", id, err)
		}
		return nil
	}

	if !changed {
		return nil
	}
	setClauses = append(setClauses, "updated_at = ?")
	vals = append(vals, acc.updatedAt)
	vals = append(vals, id)
	// #nosec G201
	q := fmt.Sprintf(`UPDATE beads SET %s WHERE id = ?`, strings.Join(setClauses, ", "))
	if _, err := tx.ExecContext(ctx, q, vals...); err != nil {
		return fmt.Errorf("import: update bead %s: %w", id, err)
	}
	return nil
}

func sqlNullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
