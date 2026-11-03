// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/config"
	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
	"github.com/loombeading/loom/internal/xdg"
)

func writeBeadsConfig(t *testing.T, contents string) {
	t.Helper()
	cfgDir, _, err := xdg.ConfigHome(os.Getenv, "loom")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, config.FileName), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWhereShowsConfigLine(t *testing.T) {
	initBeadsDir(t)

	out, _, code := runCmd(t, []string{"where"}, "")
	if code != 0 {
		t.Fatalf("where: exit code = %d", code)
	}
	if !strings.Contains(out, "- Config:") || !strings.Contains(out, "(not found)") {
		t.Fatalf("where stdout = %q, want a Config line saying not found", out)
	}

	writeBeadsConfig(t, `{"list":{"limit":5}}`)
	out, _, code = runCmd(t, []string{"where"}, "")
	if code != 0 {
		t.Fatalf("where: exit code = %d", code)
	}
	if !strings.Contains(out, "- Config:") || !strings.Contains(out, "(loaded)") {
		t.Fatalf("where stdout = %q, want a Config line saying loaded", out)
	}
}

func TestConfigUnknownKeyFailsCommands(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"list":{"bogus":1}}`)

	_, stderr, code := runCmd(t, []string{"list"}, "")
	if code != 1 {
		t.Fatalf("list: exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "bogus") {
		t.Fatalf("list stderr = %q, want to mention the unknown key", stderr)
	}
}

func TestWhereResolvesViaXDGDataHome(t *testing.T) {
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)
	xdgData := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdgData)

	if _, errS, code := runCmd(t, []string{"init"}, ""); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, errS)
	}
	out, errS, code := runCmd(t, []string{"where"}, "")
	if code != 0 {
		t.Fatalf("where: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Resolved via: XDG_DATA_HOME\n") {
		t.Fatalf("where stdout = %q, want Resolved via XDG_DATA_HOME", out)
	}
	if want := filepath.Join(xdgData, "loom", "loom.db"); !strings.Contains(out, want) {
		t.Fatalf("where stdout = %q, want the database under %s", out, want)
	}
}

func TestConfigInsideLMDirIsNotRead(t *testing.T) {
	initBeadsDir(t)
	if err := os.WriteFile(filepath.Join(beadsDir(t), config.FileName), []byte(`{"list":{"bogus":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, errS, code := runCmd(t, []string{"list"}, ""); code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	out, _, code := runCmd(t, []string{"where"}, "")
	if code != 0 {
		t.Fatalf("where: exit code = %d", code)
	}
	if !strings.Contains(out, "(not found)") {
		t.Fatalf("where stdout = %q, want the Config line to say not found", out)
	}
}

func TestLegacyTOMLConfigIsNotRead(t *testing.T) {
	initBeadsDir(t)
	cfgDir, _, err := xdg.ConfigHome(os.Getenv, "loom")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("[list\nlimit = = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, errS, code := runCmd(t, []string{"list"}, ""); code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	out, _, code := runCmd(t, []string{"where"}, "")
	if code != 0 {
		t.Fatalf("where: exit code = %d", code)
	}
	if !strings.Contains(out, "(not found)") {
		t.Fatalf("where stdout = %q, want the Config line to say not found", out)
	}
}

func TestWhereIgnoresGitCommonDirLoom(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	root := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitRun("init", "-q")
	gitRun("commit", "-q", "--allow-empty", "-m", "x")
	worktree := filepath.Join(t.TempDir(), "w")
	gitRun("worktree", "add", "-q", worktree)
	for _, dir := range []string{filepath.Join(root, ".loom"), filepath.Join(root, ".git", ".loom")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "loom.db"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Chdir(worktree)
	setIsolatedXDGHome(t)
	out, _, code := runCmd(t, []string{"where"}, "")
	if code != 1 {
		t.Fatalf("where: exit code = %d, want 1 (git's common directory must not be consulted)", code)
	}
	if out != "" {
		t.Fatalf("where stdout = %q, want empty", out)
	}
}

func TestBuiltInDefaultsAreFixedLiterals(t *testing.T) {
	if output.DefaultLimit != 50 {
		t.Fatalf("output.DefaultLimit = %d, want 50", output.DefaultLimit)
	}
	if storage.DefaultBusyTimeoutMS != 5000 {
		t.Fatalf("storage.DefaultBusyTimeoutMS = %d, want 5000", storage.DefaultBusyTimeoutMS)
	}
	if output.DefaultRetentionDays != 0 {
		t.Fatalf("output.DefaultRetentionDays = %d, want 0", output.DefaultRetentionDays)
	}
	if domain.DefaultShortLen != 4 {
		t.Fatalf("domain.DefaultShortLen = %d, want 4", domain.DefaultShortLen)
	}
}

func TestListLimitPrecedence(t *testing.T) {
	initBeadsDir(t)
	for range 3 {
		mustCreateBead(t, "--title", "t")
	}

	out, _, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d", code)
	}
	if strings.Contains(out, "Truncated") {
		t.Fatalf("list stdout = %q, want no truncation under the built-in default", out)
	}

	writeBeadsConfig(t, `{"list":{"limit":2}}`)
	out, _, code = runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d", code)
	}
	if !strings.Contains(out, "Truncated") {
		t.Fatalf("list stdout = %q, want truncation under config list.limit=2", out)
	}

	out, _, code = runCmd(t, []string{"list", "--limit", "0"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d", code)
	}
	if strings.Contains(out, "Truncated") {
		t.Fatalf("list stdout = %q, want no truncation when --limit 0 overrides config", out)
	}
}

func TestNamespaceDefaultPrecedence(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"namespace":{"default":"web"}}`)

	out, stderr, code := runCmd(t, []string{"create", "--title", "t"}, "")
	if code != 0 {
		t.Fatalf("create: exit code = %d, stderr = %q", code, stderr)
	}
	if id := mustParseCreatedID(t, out); !strings.HasPrefix(id, "web-") {
		t.Fatalf("create stdout = %q, want namespace web- from config.json", out)
	}

	t.Setenv("LM_NAMESPACE", "api")
	out, stderr, code = runCmd(t, []string{"create", "--title", "t2"}, "")
	if code != 0 {
		t.Fatalf("create: exit code = %d, stderr = %q", code, stderr)
	}
	if id := mustParseCreatedID(t, out); !strings.HasPrefix(id, "api-") {
		t.Fatalf("create stdout = %q, want namespace api- from LM_NAMESPACE", out)
	}

	out, stderr, code = runCmd(t, []string{"create", "--title", "t3", "--namespace", "cli"}, "")
	if code != 0 {
		t.Fatalf("create: exit code = %d, stderr = %q", code, stderr)
	}
	if id := mustParseCreatedID(t, out); !strings.HasPrefix(id, "cli-") {
		t.Fatalf("create stdout = %q, want namespace cli- from --namespace", out)
	}
}

func TestNamespaceDefaultFiltersEnumeration(t *testing.T) {
	initBeadsDir(t)
	webID := mustCreateBead(t, "--title", "web task", "--namespace", "web")
	appID := mustCreateBead(t, "--title", "app task", "--namespace", "app")
	webID, appID = mustDisplayAlias(t, webID), mustDisplayAlias(t, appID)

	writeBeadsConfig(t, `{"namespace":{"default":"web"}}`)

	out, stderr, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(out, `namespace="web"`) {
		t.Fatalf("list stdout = %q, want a Filter: line naming namespace=web from config namespace.default", out)
	}
	if !strings.Contains(out, webID) || strings.Contains(out, appID) {
		t.Fatalf("list stdout = %q, want only %s under config namespace.default=web, not %s", out, webID, appID)
	}

	t.Setenv("LM_NAMESPACE", "app")
	out, stderr, code = runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(out, appID) || strings.Contains(out, webID) {
		t.Fatalf("list stdout = %q, want LM_NAMESPACE=app to override config namespace.default=web", out)
	}

	out, stderr, code = runCmd(t, []string{"list", "--namespace", ""}, "")
	if code != 0 {
		t.Fatalf(`list --namespace "": exit code = %d, stderr = %q`, code, stderr)
	}
	if !strings.Contains(out, webID) || !strings.Contains(out, appID) {
		t.Fatalf(`list --namespace "" stdout = %q, want both %s and %s once explicitly reset`, out, webID, appID)
	}
}

func TestListStatusUnfinishedFilterLine(t *testing.T) {
	initBeadsDir(t)
	openID := mustCreateBead(t, "--title", "open task")
	closedID := mustCreateBead(t, "--title", "closed task")
	if _, errS, code := runCmd(t, []string{"close", closedID}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	openID, closedID = mustDisplayAlias(t, openID), mustDisplayAlias(t, closedID)

	out, errS, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, `Filter: status="unfinished"`) {
		t.Fatalf("list stdout = %q, want an implicit status=\"unfinished\" Filter: line", out)
	}
	if !strings.Contains(out, openID) || strings.Contains(out, closedID) {
		t.Fatalf("list stdout = %q, want only the open Bead under the implicit unfinished filter", out)
	}

	out, errS, code = runCmd(t, []string{"list", "--all"}, "")
	if code != 0 {
		t.Fatalf("list --all: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, `status=`) {
		t.Fatalf("list --all stdout = %q, must not show the implicit status filter", out)
	}

	out, errS, code = runCmd(t, []string{"list", "--status", "closed"}, "")
	if code != 0 {
		t.Fatalf("list --status closed: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, `status="closed"`) || strings.Contains(out, `status="unfinished"`) {
		t.Fatalf("list --status closed stdout = %q, want the explicit status echoed, not the implicit unfinished label", out)
	}

	searchOut, errS, code := runCmd(t, []string{"search", "task"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(searchOut, `status="unfinished"`) {
		t.Fatalf("search stdout = %q, want an implicit status=\"unfinished\" condition", searchOut)
	}
	if !strings.Contains(searchOut, openID) || strings.Contains(searchOut, closedID) {
		t.Fatalf("search stdout = %q, want only the open Bead under the implicit unfinished filter", searchOut)
	}
}

func TestBusyTimeoutMSFromConfig(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"db":{"busy_timeout_ms":1234}}`)

	dir, _, cfg, _, cfgFound, err := resolveDir(os.Getenv)
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	if !cfgFound {
		t.Fatal("resolveDir: cfgFound = false, want true")
	}
	db, err := openDBAt(dir, os.Getenv, "", cfg)
	if err != nil {
		t.Fatalf("openDBAt: %v", err)
	}
	defer func() { _ = db.Close() }()

	var busyTimeout int
	if err := db.SQL.QueryRowContext(t.Context(), `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 1234 {
		t.Fatalf("busy_timeout = %d, want 1234 (from config.json's db.busy_timeout_ms)", busyTimeout)
	}
}
