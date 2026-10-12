// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func buildReleaseLM(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "lm")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: could not determine source location")
	}
	cmd := exec.CommandContext(t.Context(), "go", "build", "-tags=", "-o", binPath, ".")
	cmd.Dir = filepath.Dir(thisFile)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build lm: %v\n%s", err, out)
	}
	return binPath
}

func TestReleaseBuildIgnoresLMNow(t *testing.T) {
	bin := buildReleaseLM(t)
	dbDir := filepath.Join(t.TempDir(), ".loom")
	run := func(lmNow string, args ...string) (string, string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Env = append(subprocessEnv(dbDir), "LM_NOW="+lmNow, "NO_COLOR=1")
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		_ = cmd.Run()
		return out.String(), errBuf.String()
	}
	const warning = "warning: LM_NOW is ignored by release builds"

	if _, errS := run("", "init"); strings.Contains(errS, warning) {
		t.Fatalf("init without LM_NOW warned: %q", errS)
	}
	out, _ := run("", "create", "--title", "held by alice")
	id := mustParseCreatedID(t, out)
	if _, errS := run("", "update", id, "--claim", "--claim-ttl", "30m", "--actor", "alice"); errS != "" {
		t.Fatalf("claim by alice: stderr %q", errS)
	}

	const future = "2099-01-01T00:00:00Z"
	listOut, errS := run(future, "list", "--claim-expired")
	if strings.Contains(listOut, "held by alice") {
		t.Errorf("list --claim-expired with LM_NOW in the future = %q, want no row", listOut)
	}
	if strings.Count(errS, warning) != 1 {
		t.Errorf("stderr = %q, want the warning exactly once", errS)
	}
	if _, errS := run("not-a-time", "list"); !strings.Contains(errS, warning) || strings.Contains(errS, "invalid LM_NOW") {
		t.Errorf("stderr with garbage LM_NOW = %q, want only the warning", errS)
	}

	run(future, "update", id, "--claim", "--actor", "bob")
	show, _ := run(future, "show", id)
	if !strings.Contains(show, `claimed_by: "alice"`) {
		t.Errorf("show after bob's takeover attempt:\n%s\nwant claimed_by alice", show)
	}
	run(future, "ready", "--claim", "--actor", "bob")
	if show, _ := run("", "show", id); !strings.Contains(show, `claimed_by: "alice"`) {
		t.Errorf("show after bob's ready --claim:\n%s\nwant claimed_by alice", show)
	}

	if help, _ := run("", "help"); strings.Contains(help, "LM_NOW") {
		t.Errorf("release help lists LM_NOW:\n%s", help)
	}
}
