// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/storage"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := filepath.Join(t.TempDir(), storage.BeadsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(context.Background(), dir, storage.OpenOptions{
		Env:         func(string) string { return "" },
		Actor:       "test",
		AllowCreate: true,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db.SQL
}

func mustCreate(t *testing.T, db *sql.DB, in domain.CreateInput) string {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.CreateBead(context.Background(), tx, in)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateBead: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}
