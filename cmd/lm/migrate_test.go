// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/storage"
)

func makePreReleaseBeadsDir(t *testing.T) {
	t.Helper()
	initBeadsDir(t)

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), "DROP INDEX idx_beads_short_id"); err != nil {
		t.Fatalf("alter pre-release database: %v", err)
	}
	if _, err := db.ExecContext(t.Context(), "PRAGMA user_version = 19"); err != nil {
		t.Fatalf("build pre-release database: %v", err)
	}
}

func TestMigrateCLIAlreadyCurrent(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"migrate"}, "")
	if code != 0 {
		t.Fatalf("migrate: exit code = %d, stderr = %q", code, errS)
	}
	want := fmt.Sprintf("- Schema: %d (already current)\n", storage.SchemaVersion)
	if out != want {
		t.Fatalf("migrate stdout = %q, want %q", out, want)
	}
}

func TestMigrateCLIRejectsPreReleaseDatabase(t *testing.T) {
	makePreReleaseBeadsDir(t)

	out, errS, code := runCmd(t, []string{"migrate", "--actor", "cli-test"}, "")
	if code != 1 {
		t.Fatalf("migrate: exit code = %d, want 1 (stdout=%q stderr=%q)", code, out, errS)
	}
	if out != "" {
		t.Fatalf("migrate stdout = %q, want empty on failure", out)
	}
	if _, err := os.Stat(beadsDBPath(t) + ".bak-schema19"); err == nil {
		t.Fatal("backup created for a rejected migrate")
	}
}

func TestMigrateCLIReadOnlyExitsNonZero(t *testing.T) {
	makePreReleaseBeadsDir(t)
	makeDBReadOnly(t)

	out, errS, code := runCmd(t, []string{"migrate"}, "")
	if code != 1 {
		t.Fatalf("migrate: exit code = %d, want 1 (stdout=%q stderr=%q)", code, out, errS)
	}
	if out != "" {
		t.Fatalf("migrate stdout = %q, want empty on failure", out)
	}
	if !strings.HasPrefix(errS, "Error: ") {
		t.Fatalf("migrate stderr = %q, want it to start with %q", errS, "Error: ")
	}
}

func TestRunNewerSchemaExitsOneAndLeavesDatabase(t *testing.T) {
	initBeadsDir(t)
	rawVersion := func() int {
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t))+"?_journal_mode=WAL")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		var v int
		if err := db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 1 {
		t.Fatalf("list: exit code = %d, want 1 (stdout=%q stderr=%q)", code, out, errS)
	}
	if !strings.Contains(errS, "a newer lm is required") {
		t.Fatalf("list stderr = %q, want it to say a newer lm is required", errS)
	}
	if v := rawVersion(); v != 999 {
		t.Fatalf("user_version = %d, want 999 left untouched", v)
	}
}

func TestMigrateCLIRejectsPositionalArgs(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"migrate", "extra"}, "")
	if code != 1 {
		t.Fatalf("migrate extra: exit code = %d, want 1", code)
	}
	if out != "" {
		t.Fatalf("migrate extra stdout = %q, want empty", out)
	}
	if !strings.Contains(errS, "no positional arguments") {
		t.Fatalf("migrate extra stderr = %q, want it to mention positional arguments", errS)
	}
}

func TestMigrateCLINoBeadsDirReturnsError(t *testing.T) {
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)

	out, errS, code := runCmd(t, []string{"migrate"}, "")
	if code != 1 {
		t.Fatalf("migrate: exit code = %d, want 1 (stdout=%q stderr=%q)", code, out, errS)
	}
	if out != "" {
		t.Fatalf("migrate stdout = %q, want empty", out)
	}
	if !strings.HasPrefix(errS, "Error: ") {
		t.Fatalf("migrate stderr = %q, want it to start with %q", errS, "Error: ")
	}
}
