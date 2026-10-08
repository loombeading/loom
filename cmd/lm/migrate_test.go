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
	t.Setenv("LM_READONLY", "1")

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
