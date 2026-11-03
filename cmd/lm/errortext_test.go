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

type errorTextViolation struct {
	pos  token.Position
	text string
}

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
			for _, v := range errorTextViolations(fset, f) {
				t.Errorf("%s: fixed error text %q contains a backtick or \"|\" (escaped to \\` or \\| for the user)", v.pos, v.text)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
}

func TestErrorTextViolationsCoversCompositeLiterals(t *testing.T) {
	src := "package p\n" +
		"func f(v int) {\n" +
		"\t_ = &ValidationError{Msg: \"use `lm x`\"}\n" +
		"\t_ = domain.ValidationError{Msg: fmt.Sprintf(\"a | %s\", v)}\n" +
		"\t_ = &ValidationError{Msg: \"plain\"}\n" +
		"\t_ = &Other{Msg: \"use `lm y`\"}\n" +
		"\t_ = fmt.Errorf(\"use `lm z`\")\n" +
		"\t_ = fmt.Errorf(\"%w\", &ValidationError{Msg: \"b | c\"})\n" +
		"}\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synthetic.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := errorTextViolations(fset, f)
	want := []string{"use `lm x`", "a | %s", "use `lm z`", "b | c"}
	if len(got) != len(want) {
		t.Fatalf("got %d violations %v, want %d %v", len(got), got, len(want), want)
	}
	for i, w := range want {
		if got[i].text != w {
			t.Errorf("violation %d: got %q, want %q", i, got[i].text, w)
		}
	}
}

func errorTextViolations(fset *token.FileSet, f *ast.File) []errorTextViolation {
	var out []errorTextViolation
	seen := map[token.Pos]bool{}
	scan := func(root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || seen[lit.Pos()] {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if strings.ContainsAny(v, "`|") {
				seen[lit.Pos()] = true
				out = append(out, errorTextViolation{pos: fset.Position(lit.Pos()), text: v})
			}
			return true
		})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if isFlaggedErrorCall(x) {
				for _, arg := range x.Args {
					scan(arg)
				}
			}
		case *ast.CompositeLit:
			if isErrorTypeName(x.Type) {
				for _, elt := range x.Elts {
					scan(elt)
				}
			}
		}
		return true
	})
	return out
}

func isErrorTypeName(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return strings.HasSuffix(t.Name, "Error")
	case *ast.SelectorExpr:
		return strings.HasSuffix(t.Sel.Name, "Error")
	}
	return false
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
