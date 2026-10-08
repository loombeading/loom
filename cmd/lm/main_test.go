// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/storage"
)

func TestRunVersion(t *testing.T) {
	version = "test-version"
	defer func() { version = "dev" }()
	readBuildInfo = func() (*debug.BuildInfo, bool) { return nil, false }
	defer func() { readBuildInfo = debug.ReadBuildInfo }()

	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := stdout.String(); got != "lm test-version (- -)\n" {
		t.Fatalf("stdout = %q, want %q", got, "lm test-version (- -)\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunVersionWithVCSInfo(t *testing.T) {
	version = "test-version"
	defer func() { version = "dev" }()
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef0123456789"},
			{Key: "vcs.time", Value: "2026-09-08T01:02:03Z"},
		}}, true
	}
	defer func() { readBuildInfo = debug.ReadBuildInfo }()

	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := "lm test-version (abcdef012 2026-09-08)\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestResolveVCSNoBuildInfo(t *testing.T) {
	readBuildInfo = func() (*debug.BuildInfo, bool) { return nil, false }
	defer func() { readBuildInfo = debug.ReadBuildInfo }()

	commit, date := resolveVCS()
	if commit != "-" || date != "-" {
		t.Fatalf("resolveVCS() = (%q, %q), want (\"-\", \"-\")", commit, date)
	}
}

func TestResolveVersionLdflags(t *testing.T) {
	version = "v0.2609.1"
	defer func() { version = "dev" }()
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		t.Fatal("readBuildInfo should not be called when version is ldflags-injected")
		return nil, false
	}
	defer func() { readBuildInfo = debug.ReadBuildInfo }()

	if got := resolveVersion(); got != "v0.2609.1" {
		t.Fatalf("resolveVersion() = %q, want %q", got, "v0.2609.1")
	}
}

func TestResolveVersionBuildInfoFallback(t *testing.T) {
	version = "dev"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v0.2609.1"}}, true
	}
	defer func() { readBuildInfo = debug.ReadBuildInfo }()

	if got := resolveVersion(); got != "v0.2609.1" {
		t.Fatalf("resolveVersion() = %q, want %q", got, "v0.2609.1")
	}
}

func TestResolveVersionBuildInfoDevelFallsBackToDev(t *testing.T) {
	cases := []string{"(devel)", ""}
	for _, mainVersion := range cases {
		version = "dev"
		readBuildInfo = func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}, true
		}

		if got := resolveVersion(); got != "dev" {
			t.Fatalf("resolveVersion() with Main.Version=%q = %q, want %q", mainVersion, got, "dev")
		}
	}
	readBuildInfo = debug.ReadBuildInfo
}

func TestResolveVersionNoBuildInfo(t *testing.T) {
	version = "dev"
	readBuildInfo = func() (*debug.BuildInfo, bool) { return nil, false }
	defer func() { readBuildInfo = debug.ReadBuildInfo }()

	if got := resolveVersion(); got != "dev" {
		t.Fatalf("resolveVersion() = %q, want %q", got, "dev")
	}
}

func TestRunUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, strings.NewReader(""), &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "bogus") {
		t.Fatalf("stderr = %q, want to mention the unknown subcommand", stderr.String())
	}
}

func TestRunNoSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(""), &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunInitAndWhere(t *testing.T) {
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "- Database:") {
		t.Fatalf("init stdout = %q, want a Database line", stdout.String())
	}
	if strings.Contains(stdout.String(), "Recommendation:") {
		t.Fatalf("init stdout = %q, must not propose a VCS ignore recommendation (EDD: default DB lives outside the repo)", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"where"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("where: exit code = %d, stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "- Database:") || !strings.Contains(got, "- Resolved via: default") {
		t.Fatalf("where stdout = %q, want Database and Resolved via lines", got)
	}
	if strings.Contains(got, "Readonly") {
		t.Fatalf("where stdout = %q, must not show Readonly when not read-only", got)
	}
}

func TestRunInitTwiceFails(t *testing.T) {
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("first init: exit code = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("second init: exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("second init stdout = %q, want empty", stdout.String())
	}
}

func TestRunWhereWithoutBeadsDirFails(t *testing.T) {
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"where"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunWhereWithEmptyBeadsDirFailsWithoutCreatingDB(t *testing.T) {
	t.Chdir(t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LM_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	defaultDir := filepath.Join(home, ".local", "share", "loom")
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := storage.DBPath(defaultDir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"where"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not found") {
		t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), "not found")
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Stat(%s) = %v, want os.ErrNotExist (where must not create loom.db)", dbPath, err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("os.Stat(%s) after init = %v, want nil", dbPath, err)
	}
}

func TestRunWhereReadOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	setIsolatedXDGHome(t)
	t.Setenv("LM_READONLY", "0")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, stderr.String())
	}

	t.Setenv("LM_READONLY", "1")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"where"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("where: exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "- Readonly: yes (mode=ro)") {
		t.Fatalf("stdout = %q, want a Readonly line", stdout.String())
	}
}

func TestRunUpdateReadOnlyRejectsToStderr(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	setIsolatedXDGHome(t)
	t.Setenv("LM_READONLY", "0")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"create", "--title", "t"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create: exit code = %d, stderr = %q", code, stderr.String())
	}
	id := mustParseCreatedID(t, stdout.String())

	t.Setenv("LM_READONLY", "1")
	stdout.Reset()
	stderr.Reset()
	code := run([]string{"update", id, "--priority", "2"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("update under LM_READONLY=1: exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Rejected") {
		t.Fatalf("stderr = %q, want it to contain Rejected", stderr.String())
	}
}

func TestRunUpdateReadOnlyTruthyValueRejectsWrite(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	setIsolatedXDGHome(t)
	t.Setenv("LM_READONLY", "0")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"create", "--title", "t"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create: exit code = %d, stderr = %q", code, stderr.String())
	}
	id := mustParseCreatedID(t, stdout.String())

	t.Setenv("LM_READONLY", "true")
	stdout.Reset()
	stderr.Reset()
	code := run([]string{"update", id, "--title", "renamed"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("update under LM_READONLY=true: exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Rejected") {
		t.Fatalf("stderr = %q, want it to contain Rejected", stderr.String())
	}
}

func TestRunWhereShowsSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	setIsolatedXDGHome(t)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"where"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("where: exit code = %d, stderr = %q", code, stderr.String())
	}
	want := fmt.Sprintf("- Schema: %d (binary expects %d)", storage.SchemaVersion, storage.SchemaVersion)
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want a Schema line %q", stdout.String(), want)
	}
}
