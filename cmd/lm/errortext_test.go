// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestErrorTextHasNoEscapedChars(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	for _, dir := range []string{"cmd", "internal"} {
		root := filepath.Join(repoRoot, dir)
		err := walkRepo(root, map[string]bool{"testdata": true}, func(path string, d fs.DirEntry) error {
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isFlaggedErrorCall(call) {
					return true
				}
				for _, arg := range call.Args {
					ast.Inspect(arg, func(n ast.Node) bool {
						lit, ok := n.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							return true
						}
						v, err := strconv.Unquote(lit.Value)
						if err != nil {
							return true
						}
						if strings.ContainsAny(v, "`|") {
							t.Errorf("%s: fixed error text %q contains a backtick or \"|\" (escaped to \\` or \\| for the user)", fset.Position(lit.Pos()), v)
						}
						return true
					})
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
}

func isFlaggedErrorCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		if !ok {
			return false
		}
		switch {
		case pkg.Name == "output" && fn.Sel.Name == "WriteError":
			return true
		case pkg.Name == "fmt" && fn.Sel.Name == "Errorf":
			return true
		case pkg.Name == "errors" && fn.Sel.Name == "New":
			return true
		}
	case *ast.Ident:
		if fn.Name == "WriteError" {
			return true
		}
	}
	return false
}
