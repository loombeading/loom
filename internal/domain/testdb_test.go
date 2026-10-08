// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/loombeading/loom/internal/storage"
)

type nthCallErrQuerier struct {
	Querier

	failOn int
	n      int
}

var errBoom = errors.New("boom")

func (q *nthCallErrQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.n++
	if q.n == q.failOn {
		return nil, errBoom
	}
	return q.Querier.QueryContext(ctx, query, args...)
}

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

func insertBead(t *testing.T, db *sql.DB, id, namespace, createdAt string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO beads (id, namespace, title, status, priority, type, created_at, updated_at, short_id)
		VALUES (?, ?, 'x', 'open', 2, 'task', ?, ?, substr(?, 1, 3))`,
		id, namespace, createdAt, createdAt, id)
	if err != nil {
		t.Fatalf("insertBead: %v", err)
	}
}

func insertParentChild(t *testing.T, db *sql.DB, child, parent, createdAt string, removed bool) {
	t.Helper()
	removedInt := 0
	if removed {
		removedInt = 1
	}
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed)
		VALUES (?, ?, 'parent-child', ?, ?)`,
		child, parent, createdAt, removedInt)
	if err != nil {
		t.Fatalf("insertParentChild: %v", err)
	}
}
