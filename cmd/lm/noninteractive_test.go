// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNoExecUsage(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: could not determine source location")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	execNeedle := []byte(`"` + "os" + "/" + "exec" + `"`)
	self := thisFile

	skipDirs := []string{
		filepath.Join(repoRoot, "cmd", "harness"),
	}

	for _, dir := range []string{"cmd", "internal"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if slices.Contains(skipDirs, path) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || path == self || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(b, execNeedle) {
				t.Errorf("%s imports os/exec; lm must not launch external processes", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

func TestNoEditorOrPagerLaunchedAcrossSubcommands(t *testing.T) {
	t.Setenv("EDITOR", "/bin/false")
	t.Setenv("PAGER", "/bin/false")
	initBeadsDir(t)

	id := mustCreateBead(t, "--title", "t", "--description", "d")

	cases := [][]string{
		{"where"},
		{"list"},
		{"show", id},
		{"update", id, "--claim"},
		{"close", id, "--reason", "done"},
	}
	for _, args := range cases {
		done := make(chan int, 1)
		go func(args []string) {
			_, _, code := runCmd(t, args, "")
			done <- code
		}(args)
		select {
		case <-done:

		case <-time.After(5 * time.Second):
			t.Fatalf("%v: timed out, suspected editor/pager launch or stdin wait", args)
		}
	}
}

func TestStdinClosedDoesNotHangOnDashFlag(t *testing.T) {
	initBeadsDir(t)

	closed, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closed.Close() }()

	done := make(chan struct{})
	var stdout, stderr bytes.Buffer
	go func() {
		run([]string{"create", "--title", "t", "--description", "-"}, closed, &stdout, &stderr)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("create --description - with /dev/null stdin timed out (blocked waiting for input)")
	}
}
