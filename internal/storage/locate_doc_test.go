// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	fallbackLineRE = regexp.MustCompile("未設定(なら|のときは|の場合は)")
	staleDefaultRE = regexp.MustCompile("未設定(なら|のときは|の場合は)\\s*`(\\./)?\\.loom`")
)

func locateVia(t *testing.T, vars map[string]string) (string, string) {
	t.Helper()
	dir, via, err := Locate(func(k string) string { return vars[k] })
	if err != nil {
		t.Fatalf("Locate(%v): %v", vars, err)
	}
	return dir, via
}

func TestDocsDescribeLocateFallback(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local", "share", xdgAppName), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, via := locateVia(t, map[string]string{"HOME": home})
	if via != "default" {
		t.Fatalf("via = %q, want default", via)
	}
	rel, err := filepath.Rel(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	homeDefault := "~/" + filepath.ToSlash(rel)

	xdgHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(xdgHome, xdgAppName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, via := locateVia(t, map[string]string{"HOME": home, "XDG_DATA_HOME": xdgHome}); via != "XDG_DATA_HOME" {
		t.Fatalf("via = %q, want XDG_DATA_HOME", via)
	}
	xdgDefault := "$XDG_DATA_HOME/" + xdgAppName

	root := filepath.Join("..", "..")
	for _, doc := range []string{"README.md", filepath.Join("skills", "loom", "docs", "env.md")} {
		b, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		var found int
		for i, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, "`LM_DIR`") || !fallbackLineRE.MatchString(line) {
				continue
			}
			found++
			if staleDefaultRE.MatchString(line) {
				t.Errorf("%s:%d: names ./.loom as the LM_DIR default, but Locate does not", doc, i+1)
			}
			for _, want := range []string{xdgDefault, homeDefault} {
				if !strings.Contains(line, want) {
					t.Errorf("%s:%d: LM_DIR fallback does not mention %s", doc, i+1, want)
				}
			}
		}
		if found == 0 {
			t.Errorf("%s: no line describes the LM_DIR fallback", doc)
		}
	}
}
