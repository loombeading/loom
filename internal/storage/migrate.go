// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type MigrateResult struct {
	From, To int
	Backup   string
}

var migrateBuildDSN = buildDSN
var migrateSQLOpen = sql.Open
var migrateBeginTx = func(ctx context.Context, db *sql.DB) (*sql.Tx, error) { return db.BeginTx(ctx, nil) }

func Migrate(ctx context.Context, dir string, opts OpenOptions) (MigrateResult, error) {
	return migrateTo(ctx, dir, opts, migrationFiles, SchemaVersion)
}

func migrateTo(ctx context.Context, dir string, opts OpenOptions, files fs.FS, target int) (MigrateResult, error) {
	db, err := openMigrateDB(dir, opts)
	if err != nil {
		return MigrateResult{}, err
	}
	defer func() { _ = db.Close() }()

	v, err := userVersion(ctx, db)
	if err != nil {
		return MigrateResult{}, err
	}
	if target == 0 && (v == prePublicVersion || v == 0) {
		return reshapePrePublic(ctx, db, dir, v)
	}
	if v > target {
		return MigrateResult{}, ErrSchemaTooNew
	}
	if v == target {
		return MigrateResult{From: v, To: v}, nil
	}

	steps := make([]string, 0, target-v)
	for n := v + 1; n <= target; n++ {
		b, err := fs.ReadFile(files, migrationPath(n))
		if err != nil {
			return MigrateResult{}, fmt.Errorf("read migration %d: %w", n, err)
		}
		steps = append(steps, string(b))
	}

	backup := DBPath(dir) + ".bak-schema" + strconv.Itoa(v)
	if err := backupOnce(ctx, db, backup); err != nil {
		return MigrateResult{}, err
	}

	tx, err := migrateBeginTx(ctx, db)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("begin migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	steps = append(steps, "PRAGMA user_version = "+strconv.Itoa(target))
	for i, stmt := range steps {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return MigrateResult{}, fmt.Errorf("apply migration %d: %w", v+1+i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return MigrateResult{}, fmt.Errorf("commit migration: %w", err)
	}
	return MigrateResult{From: v, To: target, Backup: backup}, nil
}

const prePublicVersion = 1

func reshapePrePublic(ctx context.Context, db *sql.DB, dir string, from int) (MigrateResult, error) {
	got, err := schemaShape(ctx, db)
	if err != nil {
		return MigrateResult{}, err
	}
	want, err := referenceShape(ctx)
	if err != nil {
		return MigrateResult{}, err
	}
	if got != want {
		old, err := preRenameShape(ctx)
		switch {
		case err != nil:
			return MigrateResult{}, err
		case got == old:
			return renamePrePublic(ctx, db, dir, from)
		case from == 0:
			return MigrateResult{From: 0, To: 0}, nil
		}
		return MigrateResult{}, ErrSchemaTooNew
	}
	if from == 0 {
		return MigrateResult{From: 0, To: 0}, nil
	}

	backup := DBPath(dir) + ".bak-schema" + strconv.Itoa(prePublicVersion)
	if err := backupOnce(ctx, db, backup); err != nil {
		return MigrateResult{}, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA user_version = 0`); err != nil {
		return MigrateResult{}, fmt.Errorf("relabel schema version: %w", err)
	}
	return MigrateResult{From: prePublicVersion, To: 0, Backup: backup}, nil
}

var referenceSQLOpen = sql.Open

func referenceShape(ctx context.Context) (string, error) {
	fresh, err := referenceSQLOpen("sqlite", "file::memory:")
	if err != nil {
		return "", fmt.Errorf("open reference schema: %w", err)
	}
	defer func() { _ = fresh.Close() }()
	fresh.SetMaxOpenConns(1)
	if _, err := fresh.ExecContext(ctx, strings.Join(schemaDDL, ";\n")); err != nil {
		return "", fmt.Errorf("create reference schema: %w", err)
	}
	return schemaShape(ctx, fresh)
}

func schemaShape(ctx context.Context, db *sql.DB) (string, error) {
	var out strings.Builder
	for _, q := range []string{
		`SELECT 'table ' || m.name || ' ' || ti.cid || ' ' || ti.name || ':' || ti.type || ':' || ti."notnull" || ':' || coalesce(ti.dflt_value, '') || ':' || ti.pk AS line
		FROM sqlite_master m, pragma_table_info(m.name) ti
		WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%' ORDER BY m.name, ti.cid`,
		`SELECT 'index ' || m.name || ' ' || m.tbl_name || ' ' || il."unique" || ':' || il.partial || ' ' || ix.seqno || ' ' || ix.name || ':' || ix.desc AS line
		FROM sqlite_master m, pragma_index_list(m.tbl_name) il, pragma_index_xinfo(m.name) ix
		WHERE m.type = 'index' AND il.name = m.name AND ix.key = 1 ORDER BY m.name, ix.seqno`,
		`SELECT 'trigger ' || name || ' ' || tbl_name || ' ' || sql AS line FROM sqlite_master WHERE type = 'trigger' ORDER BY name`,
	} {
		var part string
		if err := db.QueryRowContext(ctx, `SELECT coalesce(group_concat(line, char(10)), '') FROM (`+q+`)`).Scan(&part); err != nil {
			return "", fmt.Errorf("read schema: %w", err)
		}
		out.WriteString(part + "\n")
	}
	return out.String(), nil
}

func migrationPath(v int) string {
	return "migrations/" + strconv.Itoa(v) + ".sql"
}

func backupOnce(ctx context.Context, db *sql.DB, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("back up to %s: %w", path, err)
	}
	return nil
}

func openMigrateDB(dir string, opts OpenOptions) (*sql.DB, error) {
	if opts.Env == nil {
		return nil, errors.New("storage.Migrate: OpenOptions.Env is required")
	}

	dbPath := DBPath(dir)
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrDBNotFound, dbPath)
		}
		return nil, fmt.Errorf("stat %s: %w", dbPath, err)
	}

	if IsReadOnlyEnv(opts.Env("LM_READONLY")) {
		return nil, ErrReadOnlyWrite
	}

	busyTimeoutMS := opts.BusyTimeoutMS
	if busyTimeoutMS == 0 {
		busyTimeoutMS = DefaultBusyTimeoutMS
	}
	dsn, err := migrateBuildDSN(dbPath, false, "", busyTimeoutMS)
	if err != nil {
		return nil, err
	}
	db, err := migrateSQLOpen("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
