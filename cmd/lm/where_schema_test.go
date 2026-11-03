// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/storage"
)

func TestRunWherePrintsSchemaLineOnVersionMismatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
		errText string
	}{
		{"too-new", storage.SchemaVersion + 1, "newer"},
		{"too-old", storage.SchemaVersion - 1, "older"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			setIsolatedXDGHome(t)

			var stdout, stderr bytes.Buffer
			if code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
				t.Fatalf("init: exit code = %d, stderr = %q", code, stderr.String())
			}
			lmDir, _, err := storage.Locate(os.Getenv)
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := sql.Open("sqlite", storage.DBPath(lmDir))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sqlDB.ExecContext(context.Background(), fmt.Sprintf(`PRAGMA user_version = %d`, tc.version)); err != nil {
				t.Fatal(err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}

			stdout.Reset()
			stderr.Reset()
			code := run([]string{"where"}, strings.NewReader(""), &stdout, &stderr)
			if code != 1 {
				t.Fatalf("where: exit code = %d, want 1", code)
			}
			want := fmt.Sprintf("- Schema: %d (binary expects %d)\n", tc.version, storage.SchemaVersion)
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), want)
			}
			if !strings.Contains(stderr.String(), tc.errText) {
				t.Fatalf("stderr = %q, want the %s schema error", stderr.String(), tc.errText)
			}
		})
	}
}
