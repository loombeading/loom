// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package waitlabel

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var literalAllowed = map[string][]string{
	"internal/domain/gate_wait.go": {"wait:pr-merged:", "wait:ci-green:"},
	"internal/domain/update.go":    {"wait:date:"},
}

var labelLiteralRE = regexp.MustCompile(`^wait:[a-z-]*(:|$)`)

func waitLiterals(root string) ([]string, error) {
	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/waitlabel/") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil || !labelLiteralRE.MatchString(v) {
				return true
			}
			if slices.Contains(literalAllowed[rel], v) {
				return true
			}
			found = append(found, rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line)+": "+v)
			return true
		})
		return nil
	})
	return found, err
}

func TestNoWaitLiteralOutsidePackage(t *testing.T) {
	found, err := waitLiterals(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s: build wait: labels with waitlabel.Kind (Label/Prefix) instead of a literal", f)
	}
}

func TestWaitLiteralsFindsLiteral(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cmd/x/x.go", "package x\nvar a = []string{\"--add-label\", \"wait:bogus:x\"}\nconst m = \"wait:date は日単位\"\n")
	write("cmd/x/x_test.go", "package x\nvar b = \"wait:date:t\"\n")
	write("internal/waitlabel/w.go", "package waitlabel\nconst P = \"wait:\"\n")
	write("internal/domain/update.go", "package domain\nconst U = \"wait:date:\"\nconst V = \"wait:pr-merged:\"\n")
	write(".worktrees/y/y.go", "package y\nvar c = \"wait:date:\"\n")
	found, err := waitLiterals(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(found, "\n")
	want := "cmd/x/x.go:2: wait:bogus:x\ninternal/domain/update.go:3: wait:pr-merged:"
	if got != want {
		t.Errorf("waitLiterals = %q, want %q", got, want)
	}
	write("cmd/z/z.go", "package z\nvar (\n")
	if _, err := waitLiterals(root); err == nil {
		t.Error("waitLiterals on an unparsable file returned no error")
	}
}
