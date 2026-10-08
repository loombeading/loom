// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func envMap(m map[string]string) Env {
	return func(k string) string { return m[k] }
}

func TestLocateBDDir(t *testing.T) {
	dir := t.TempDir()
	got, via, err := Locate(envMap(map[string]string{"LM_DIR": dir}))
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if got != filepath.Clean(dir) {
		t.Fatalf("dir = %q, want %q", got, dir)
	}
	if via != "LM_DIR" {
		t.Fatalf("via = %q, want LM_DIR", via)
	}
}

func TestLocateBDDirRelativeIsError(t *testing.T) {
	_, _, err := Locate(envMap(map[string]string{"LM_DIR": "relative/path"}))
	if err == nil {
		t.Fatal("expected an error for a relative LM_DIR")
	}
}

func TestLocateXDGDataHomeOverride(t *testing.T) {
	xdgData := t.TempDir()
	loomDir := filepath.Join(xdgData, "loom")
	if err := os.Mkdir(loomDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, via, err := Locate(envMap(map[string]string{"XDG_DATA_HOME": xdgData}))
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if got != loomDir {
		t.Fatalf("dir = %q, want %q", got, loomDir)
	}
	if via != "XDG_DATA_HOME" {
		t.Fatalf("via = %q, want XDG_DATA_HOME", via)
	}
}

func TestLocateXDGDataHomeDefault(t *testing.T) {
	home := t.TempDir()
	defaultDir := filepath.Join(home, ".local", "share", "loom")
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, via, err := Locate(envMap(map[string]string{"HOME": home}))
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if got != defaultDir {
		t.Fatalf("dir = %q, want %q", got, defaultDir)
	}
	if via != "default" {
		t.Fatalf("via = %q, want default", via)
	}
}

func TestLocateDoesNotSearchCWD(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	beadsDir := filepath.Join(cwd, BeadsDirName)
	if err := os.Mkdir(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	_, _, err := Locate(envMap(map[string]string{"HOME": home}))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (must not resolve cwd's .loom)", err)
	}
}

func TestLocateNotFound(t *testing.T) {
	home := t.TempDir()
	_, _, err := Locate(envMap(map[string]string{"HOME": home}))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestResolveInitDirDefaultsToXDGDataHome(t *testing.T) {
	home := t.TempDir()
	dir, via, err := ResolveInitDir(envMap(map[string]string{"HOME": home}))
	if err != nil {
		t.Fatalf("ResolveInitDir: %v", err)
	}
	if want := filepath.Join(home, ".local", "share", "loom"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	if via != "default" {
		t.Fatalf("via = %q, want default", via)
	}
}

func TestResolveInitDirBDDir(t *testing.T) {
	dir := t.TempDir()
	got, via, err := ResolveInitDir(envMap(map[string]string{"LM_DIR": dir}))
	if err != nil {
		t.Fatalf("ResolveInitDir: %v", err)
	}
	if got != filepath.Clean(dir) {
		t.Fatalf("dir = %q, want %q", got, dir)
	}
	if via != "LM_DIR" {
		t.Fatalf("via = %q, want LM_DIR", via)
	}
}

func TestResolveInitDirBDDirRelativeIsError(t *testing.T) {
	_, _, err := ResolveInitDir(envMap(map[string]string{"LM_DIR": "relative/path"}))
	if err == nil {
		t.Fatal("expected an error for a relative LM_DIR")
	}
}
