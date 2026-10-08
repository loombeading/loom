// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/loombeading/loom/internal/domain"
)

var importKnownDepKeys = map[string]bool{
	"_type": true, "bead_id": true, "depends_on_id": true, "type": true,
	"created_at": true, "removed": true, "_set_at": true,
}

type depKey struct {
	beadID, dependsOnID, typ string
}

func importDependencies(ctx context.Context, tx *sql.Tx, rows []importRow, opt Options, sum *Summary) error {
	byKey := map[depKey][]importRow{}
	var order []depKey

	for _, row := range rows {
		for k := range row {
			if !importKnownDepKeys[k] {
				return fmt.Errorf("import: dependency row has unknown key %q", k)
			}
		}
		beadID, err := rawString(row["bead_id"])
		if err != nil || beadID == "" {
			return fmt.Errorf("import: dependency row missing bead_id: %w", err)
		}
		dependsOnID, err := rawString(row["depends_on_id"])
		if err != nil || dependsOnID == "" {
			return fmt.Errorf("import: dependency row missing depends_on_id: %w", err)
		}
		typ, err := rawString(row["type"])
		if err != nil || typ == "" {
			return fmt.Errorf("import: dependency row missing type: %w", err)
		}

		if !domain.ValidDepTypes[typ] {
			return fmt.Errorf("import: dependency %s -> %s has out-of-vocabulary type %q", beadID, dependsOnID, typ)
		}

		k := depKey{beadID, dependsOnID, typ}
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], row)
	}
	slices.SortFunc(order, func(a, b depKey) int {
		return cmp.Or(
			cmp.Compare(a.beadID, b.beadID),
			cmp.Compare(a.dependsOnID, b.dependsOnID),
			cmp.Compare(a.typ, b.typ),
		)
	})

	for _, k := range order {
		isNew, err := mergeDependency(ctx, tx, k, byKey[k], opt, sum)
		if err != nil {
			return err
		}
		if isNew {
			sum.NewDeps++
		}
	}
	return nil
}

type linkState struct {
	removed  bool
	ts       string
	explicit bool
}

func (a linkState) newerThan(b linkState) bool {
	if a.ts != b.ts {
		return a.ts > b.ts
	}
	if a.removed != b.removed {
		return a.removed
	}
	return a.explicit && !b.explicit
}

func mergeDependency(ctx context.Context, tx *sql.Tx, k depKey, rows []importRow, opt Options, sum *Summary) (bool, error) {
	createdAt, orig, isNew, err := loadDependency(ctx, tx, k)
	if err != nil {
		return false, err
	}
	minCreatedAt, cur, err := foldDependencyRows(rows, createdAt, orig, !isNew)
	if err != nil {
		return false, err
	}

	var newRemovedAt any
	if cur.explicit {
		newRemovedAt = cur.ts
	}

	if isNew {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed, removed_set_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			k.beadID, k.dependsOnID, k.typ, minCreatedAt, cur.removed, newRemovedAt)
		if err != nil {
			return false, fmt.Errorf("import: insert dependency: %w", err)
		}
		sum.depsChanged = true
		return true, auditDependencyMerge(ctx, tx, k, cur.removed, opt, "new link")
	}

	if cur == orig && minCreatedAt == createdAt {
		return false, nil
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE dependencies SET created_at = ?, removed = ?, removed_set_at = ?
		WHERE bead_id = ? AND depends_on_id = ? AND type = ?`,
		minCreatedAt, cur.removed, newRemovedAt, k.beadID, k.dependsOnID, k.typ)
	if err != nil {
		return false, fmt.Errorf("import: update dependency: %w", err)
	}
	sum.depsChanged = true
	return false, auditDependencyMerge(ctx, tx, k, cur.removed, opt, "")
}

func loadDependency(ctx context.Context, tx *sql.Tx, k depKey) (string, linkState, bool, error) {
	var createdAt string
	var cur linkState
	var removedAt sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT created_at, removed, removed_set_at FROM dependencies
		WHERE bead_id = ? AND depends_on_id = ? AND type = ?`,
		k.beadID, k.dependsOnID, k.typ,
	).Scan(&createdAt, &cur.removed, &removedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", linkState{}, true, nil
	}
	if err != nil {
		return "", linkState{}, false, fmt.Errorf("import: load dependency: %w", err)
	}
	cur.ts, cur.explicit = createdAt, removedAt.Valid
	if removedAt.Valid {
		cur.ts = removedAt.String
	}
	return createdAt, cur, false, nil
}

func foldDependencyRows(rows []importRow, minCreatedAt string, cur linkState, haveCur bool) (string, linkState, error) {
	for _, row := range rows {
		rowCreatedAt, v, err := parseDependencyRow(row)
		if err != nil {
			return "", linkState{}, err
		}
		if minCreatedAt == "" || (rowCreatedAt != "" && rowCreatedAt < minCreatedAt) {
			minCreatedAt = rowCreatedAt
		}
		if !haveCur || v.newerThan(cur) {
			cur, haveCur = v, true
		}
	}
	return minCreatedAt, cur, nil
}

func parseDependencyRow(row importRow) (string, linkState, error) {
	createdAt, err := rawString(row["created_at"])
	if err != nil {
		return "", linkState{}, fmt.Errorf("import: dependency created_at: %w", err)
	}
	removed, err := rawBool(row["removed"])
	if err != nil {
		return "", linkState{}, fmt.Errorf("import: dependency removed: %w", err)
	}
	at, err := rawAtMap(row)
	if err != nil {
		return "", linkState{}, err
	}
	ts, err := rawString(at["removed"])
	if err != nil {
		return "", linkState{}, fmt.Errorf("import: dependency _at.removed: %w", err)
	}
	v := linkState{removed: removed, ts: ts, explicit: ts != "" || removed}
	if ts == "" {
		v.ts = createdAt
	}
	return createdAt, v, nil
}

func auditDependencyMerge(ctx context.Context, tx *sql.Tx, k depKey, removed bool, opt Options, reason string) error {
	return domain.Audit(ctx, tx, domain.AuditRecord{
		OccurredAt: opt.Now, Actor: opt.Actor, BeadID: k.beadID,
		Kind: domain.AuditKindMerge, Field: domain.FieldDependency, Origin: domain.OriginImport,
		NewValue: fmt.Sprintf(`{"depends_on_id":%q,"type":%q,"removed":%t}`, k.dependsOnID, k.typ, removed),
		Reason:   reason,
	})
}
