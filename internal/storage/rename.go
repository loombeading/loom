// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type columnRename struct{ table, old, new string }

var renamedBeadValueCols = [][2]string{
	{"namespace", "namespace"}, {"title", "title"}, {"description", "description"}, {"status", "status"},
	{"closed_at", "closed_at"}, {"priority", "priority"}, {"type", "type"}, {"claimed_by", "claimed_by"},
	{"summary", "summary"}, {"labels", "labels"}, {"depth", "reasoning_depth"}, {"external_refs", "external_refs"},
	{"claim_expires_at", "claim_expires_at"}, {"severity", "severity"}, {"due_at", "due_at"},
	{"expedite_until", "expedite_until"}, {"expedite_reason", "expedite_reason"},
	{"dedupe_key", "redetect_key"}, {"last_seen_at", "last_redetected_at"}, {"revived_at", "revived_at"},
}

const (
	oldRedetectIndex    = "idx_beads_dedupe"
	newRedetectIndex    = "idx_beads_redetect_key"
	oldRedetectIndexDDL = `CREATE UNIQUE INDEX idx_beads_dedupe ON beads (namespace, dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('open', 'in_progress')`
	newRedetectIndexDDL = `CREATE UNIQUE INDEX idx_beads_redetect_key ON beads (namespace, redetect_key) WHERE redetect_key IS NOT NULL AND status IN ('open', 'in_progress')`
)

func columnRenames() []columnRename {
	var out []columnRename
	for _, c := range renamedBeadValueCols {
		if c[0] != c[1] {
			out = append(out, columnRename{"beads", c[0], c[1]})
		}
		out = append(out, columnRename{"beads", c[0] + "_at", c[1] + "_set_at"})
	}
	return append(out,
		columnRename{"dependencies", "removed_at", "removed_set_at"},
		columnRename{"policy", "value_at", "value_set_at"},
	)
}

func renameForwardDDL() []string {
	stmts := []string{"DROP INDEX " + oldRedetectIndex}
	for _, r := range columnRenames() {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", r.table, r.old, r.new))
	}
	return append(stmts,
		"ALTER TABLE policy RENAME TO coefficients",
		newRedetectIndexDDL,
		`UPDATE coefficients SET key = 'redetect_window' WHERE key = 'reseen_window'`,
	)
}

func renameInverseDDL() []string {
	stmts := []string{"DROP INDEX " + newRedetectIndex, "ALTER TABLE coefficients RENAME TO policy"}
	for _, r := range columnRenames() {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", r.table, r.new, r.old))
	}
	return append(stmts, oldRedetectIndexDDL)
}

func preRenameShape(ctx context.Context) (string, error) {
	fresh, err := referenceSQLOpen("sqlite", "file::memory:")
	if err != nil {
		return "", fmt.Errorf("open reference schema: %w", err)
	}
	defer func() { _ = fresh.Close() }()
	fresh.SetMaxOpenConns(1)
	if _, err := fresh.ExecContext(ctx, strings.Join(append(append([]string{}, schemaDDL...), renameInverseDDL()...), ";\n")); err != nil {
		return "", fmt.Errorf("create pre-rename reference schema: %w", err)
	}
	return schemaShape(ctx, fresh)
}

func renamePrePublic(ctx context.Context, db *sql.DB, dir string, from int) (MigrateResult, error) {
	backup := DBPath(dir) + ".bak-prerename"
	if err := backupOnce(ctx, db, backup); err != nil {
		return MigrateResult{}, err
	}
	tx, err := migrateBeginTx(ctx, db)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("begin rename: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range append(renameForwardDDL(), "PRAGMA user_version = 0") {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return MigrateResult{}, fmt.Errorf("rename schema: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return MigrateResult{}, fmt.Errorf("commit rename: %w", err)
	}
	return MigrateResult{From: from, To: 0, Backup: backup}, nil
}
