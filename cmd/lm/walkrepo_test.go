// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func walkRepo(root string, skipNames map[string]bool, visit func(path string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root {
				if _, statErr := os.Lstat(filepath.Join(path, ".git")); statErr == nil {
					return filepath.SkipDir
				}
			}
			if skipNames[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		return visit(path, d)
	})
}

func TestWalkRepo(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, "a.txt"), "a")
	mustMkdir(t, filepath.Join(root, "sub"))
	mustWriteFile(t, filepath.Join(root, "sub", "b.txt"), "b")

	mustMkdir(t, filepath.Join(root, "nested"))
	mustWriteFile(t, filepath.Join(root, "nested", ".git"), "gitdir: /x")
	mustWriteFile(t, filepath.Join(root, "nested", "c.txt"), "c")

	mustMkdir(t, filepath.Join(root, "nestedrepo"))
	mustMkdir(t, filepath.Join(root, "nestedrepo", ".git"))
	mustWriteFile(t, filepath.Join(root, "nestedrepo", "d.txt"), "d")

	mustMkdir(t, filepath.Join(root, "skipme"))
	mustWriteFile(t, filepath.Join(root, "skipme", "e.txt"), "e")

	mustMkdir(t, filepath.Join(root, ".git"))
	mustWriteFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main")

	if err := os.Symlink(filepath.Join(root, "a.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	var got []string
	err := walkRepo(root, map[string]bool{"skipme": true}, func(path string, d fs.DirEntry) error {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walkRepo: %v", err)
	}
	sort.Strings(got)

	want := []string{".git/HEAD", "a.txt", "sub/b.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("walkRepo visited = %v, want %v", got, want)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
