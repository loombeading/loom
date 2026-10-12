// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openWritable(t *testing.T, dir string) *DB {
	t.Helper()
	db, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test", AllowCreate: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustMkdirBeads(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), BeadsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpenAppliesPragmas(t *testing.T) {
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)

	var journalMode string
	if err := db.SQL.QueryRowContext(t.Context(), `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var synchronous int
	if err := db.SQL.QueryRowContext(t.Context(), `PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 1 {
		t.Fatalf("synchronous = %d, want 1 (NORMAL)", synchronous)
	}

	var foreignKeys int
	if err := db.SQL.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 0 {
		t.Fatalf("foreign_keys = %d, want 0 (OFF)", foreignKeys)
	}

	var busyTimeout int
	if err := db.SQL.QueryRowContext(t.Context(), `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != DefaultBusyTimeoutMS {
		t.Fatalf("busy_timeout = %d, want %d", busyTimeout, DefaultBusyTimeoutMS)
	}
}

func TestOpenBusyTimeoutOverride(t *testing.T) {
	dir := mustMkdirBeads(t)
	db, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test", BusyTimeoutMS: 1234, AllowCreate: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	var busyTimeout int
	if err := db.SQL.QueryRowContext(t.Context(), `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 1234 {
		t.Fatalf("busy_timeout = %d, want 1234", busyTimeout)
	}
}

func TestOpenNewCreatesSchema(t *testing.T) {
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)

	v, err := userVersion(context.Background(), db.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if v != SchemaVersion {
		t.Fatalf("user_version = %d, want %d", v, SchemaVersion)
	}
}

func TestOpenSameVersionNoOp(t *testing.T) {
	dir := mustMkdirBeads(t)

	db1 := openWritable(t, dir)
	_ = db1.Close()

	db2 := openWritable(t, dir)

	v, err := userVersion(context.Background(), db2.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if v != SchemaVersion {
		t.Fatalf("user_version = %d, want %d (reopen must not alter schema version)", v, SchemaVersion)
	}
}

func TestOpenNewerSchemaIsRejected(t *testing.T) {
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)
	if _, err := db.SQL.ExecContext(t.Context(), `PRAGMA user_version = 999`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	_, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test"})
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
}

func mustMkdirBeadsAtVersion(t *testing.T, version int) string {
	t.Helper()
	dir := mustMkdirBeads(t)
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(DBPath(dir))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()

	for _, stmt := range schemaDDL {
		if _, err := sqlDB.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	if _, err := sqlDB.ExecContext(t.Context(), fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpenPreReleaseSchemaIsRejected(t *testing.T) {
	dir := mustMkdirBeadsAtVersion(t, 19)

	_, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test"})
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}

	requireNonRoot(t)
	if err := os.Chmod(DBPath(dir), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(DBPath(dir), 0o644) })
	_, err = Open(context.Background(), dir, OpenOptions{Env: envMap(nil)})
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("read-only err = %v, want ErrSchemaTooNew", err)
	}
}

func TestOpenReadOnlyModeRO(t *testing.T) {
	requireNonRoot(t)
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)
	_ = db.Close()
	if err := os.Chmod(DBPath(dir), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(DBPath(dir), 0o644) })

	roDB, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil)})
	if err != nil {
		t.Fatalf("Open read-only: %v", err)
	}
	defer func() { _ = roDB.Close() }()

	if !roDB.ReadOnly {
		t.Fatal("ReadOnly = false, want true")
	}
	if roDB.Mode != ModeReadOnly {
		t.Fatalf("Mode = %q, want %q", roDB.Mode, ModeReadOnly)
	}

	if err := roDB.WithWrite(context.Background(), func(tx *sql.Tx) error { return nil }); !errors.Is(err, ErrReadOnlyWrite) {
		t.Fatalf("WithWrite on read-only DB: err = %v, want ErrReadOnlyWrite", err)
	}

	_, err = roDB.SQL.ExecContext(t.Context(), `INSERT INTO audit_log (occurred_at, actor, kind, origin) VALUES ('2026-01-01T00:00:00.000Z', 'x', 'note', 'local')`)
	if err == nil {
		t.Fatal("expected write to fail against a mode=ro connection")
	}
}

func TestOpenReadOnlyImmutableWhenShmMissingAndDirNotWritable(t *testing.T) {
	requireNonRoot(t)
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)
	_ = db.Close()

	_ = os.Remove(DBPath(dir) + "-shm")

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	roDB, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil)})
	if err != nil {
		t.Fatalf("Open read-only: %v", err)
	}
	defer func() { _ = roDB.Close() }()

	if roDB.Mode != ModeImmutable {
		t.Fatalf("Mode = %q, want %q", roDB.Mode, ModeImmutable)
	}
}

func TestOpenFallsBackToReadOnlyWhenDBNotWritable(t *testing.T) {
	requireNonRoot(t)
	cases := []struct {
		name     string
		lock     func(dir string) error
		wantMode string
	}{
		{"file", func(dir string) error { return os.Chmod(DBPath(dir), 0o444) }, ModeReadOnly},
		{"dir", func(dir string) error { return os.Chmod(dir, 0o555) }, ModeImmutable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := mustMkdirBeads(t)
			db := openWritable(t, dir)
			_ = db.Close()
			_ = os.Remove(DBPath(dir) + "-shm")
			_ = os.Remove(DBPath(dir) + "-wal")

			if err := c.lock(dir); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = os.Chmod(dir, 0o755)
				_ = os.Chmod(DBPath(dir), 0o644)
			})

			roDB, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil)})
			if err != nil {
				t.Fatalf("Open on non-writable DB: %v", err)
			}
			defer func() { _ = roDB.Close() }()

			if !roDB.ReadOnly {
				t.Fatal("ReadOnly = false, want true")
			}
			if roDB.Mode != c.wantMode {
				t.Fatalf("Mode = %q, want %q", roDB.Mode, c.wantMode)
			}
			if _, err := UserVersion(context.Background(), roDB); err != nil {
				t.Fatalf("UserVersion: %v", err)
			}
			if err := roDB.WithWrite(context.Background(), func(tx *sql.Tx) error { return nil }); !errors.Is(err, ErrReadOnlyWrite) {
				t.Fatalf("WithWrite: err = %v, want ErrReadOnlyWrite", err)
			}
		})
	}
}

func TestOpenExistingDBSucceedsWithoutAllowCreate(t *testing.T) {
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)
	_ = db.Close()

	db2, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test"})
	if err != nil {
		t.Fatalf("Open existing db without AllowCreate: %v", err)
	}
	_ = db2.Close()
}

func TestOpenReadOnlyMissingDBReturnsErrDBNotFound(t *testing.T) {
	requireNonRoot(t)
	dir := mustMkdirBeads(t)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil)})
	if !errors.Is(err, ErrDBNotFound) {
		t.Fatalf("err = %v, want ErrDBNotFound", err)
	}
}

func TestOpenMissingDBReturnsErrDBNotFoundAndDoesNotCreateFile(t *testing.T) {
	dir := mustMkdirBeads(t)
	dbPath := DBPath(dir)

	_, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil)})
	if !errors.Is(err, ErrDBNotFound) {
		t.Fatalf("err = %v, want ErrDBNotFound", err)
	}
	if _, statErr := os.Stat(dbPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("os.Stat(%s) = %v, want os.ErrNotExist (Open must not create the file)", dbPath, statErr)
	}
}

func TestOpenStatErrorOtherThanNotExistIsNotErrDBNotFound(t *testing.T) {
	requireNonRoot(t)

	dir := mustMkdirBeads(t)
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil)})
	if err == nil {
		t.Fatal("Open: want error, got nil")
	}
	if errors.Is(err, ErrDBNotFound) {
		t.Fatalf("err = %v, want an error other than ErrDBNotFound", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v, want errors.Is(err, fs.ErrPermission)", err)
	}
}

func TestWithWriteUsesBeginImmediateAndBlocksSecondWriter(t *testing.T) {
	dir := mustMkdirBeads(t)
	db1 := openWritable(t, dir)
	db2, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test", BusyTimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()

	holding := make(chan struct{})
	release := make(chan struct{})
	writeErr := make(chan error, 1)

	go func() {
		writeErr <- db1.WithWrite(context.Background(), func(tx *sql.Tx) error {
			close(holding)
			<-release
			_, err := tx.ExecContext(t.Context(), `INSERT INTO audit_log (occurred_at, actor, kind, origin) VALUES ('2026-01-01T00:00:00.000Z', 'x', 'note', 'local')`)
			return err
		})
	}()

	<-holding

	start := time.Now()
	secondErr := db2.WithWrite(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO audit_log (occurred_at, actor, kind, origin) VALUES ('2026-01-01T00:00:01.000Z', 'y', 'note', 'local')`)
		return err
	})
	elapsed := time.Since(start)

	close(release)
	if err := <-writeErr; err != nil {
		t.Fatalf("first writer: %v", err)
	}

	if secondErr == nil {
		if elapsed < 0 {
			t.Fatalf("second writer returned implausibly fast: %v", elapsed)
		}
		return
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("second writer failed too fast (%v) to have waited on busy_timeout: %v", elapsed, secondErr)
	}
}
