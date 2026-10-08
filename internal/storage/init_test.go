// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInitCreatesDatabase(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, BeadsDirName)

	result, err := Init(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := os.Stat(result.DBPath); err != nil {
		t.Fatalf("database file not created: %v", err)
	}
}

func TestInitRejectsExistingDatabase(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, BeadsDirName)

	if _, err := Init(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test"}); err != nil {
		t.Fatal(err)
	}

	_, err := Init(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test"})
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("err = %v, want ErrAlreadyInitialized", err)
	}
}

func TestInitRejectedUnderReadOnly(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, BeadsDirName)

	_, err := Init(context.Background(), dir, OpenOptions{Env: envMap(map[string]string{"LM_READONLY": "1"})})
	if !errors.Is(err, ErrReadOnlyWrite) {
		t.Fatalf("err = %v, want ErrReadOnlyWrite", err)
	}
}
