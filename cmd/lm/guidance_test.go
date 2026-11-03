// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/output"
)

type guidanceCase struct {
	marker   string
	setup    func(t *testing.T) string
	plain    []string
	followed []string
	full     string
}

func guidanceCases() map[string]guidanceCase {
	return map[string]guidanceCase{
		output.HistoryTruncatedLine: {
			marker: "Truncated: history values cut",
			setup: func(t *testing.T) string {
				t.Helper()
				id := mustCreateBead(t, "--title", "t")
				if _, errS, code := runCmd(t, []string{"update", "--title", "t2", "--reason", strings.Repeat("r", 90) + "END", id}, ""); code != 0 {
					t.Fatalf("update: exit code = %d, stderr = %q", code, errS)
				}
				return id
			},
			plain:    []string{"show"},
			followed: []string{"show", "--full"},
			full:     strings.Repeat("r", 90) + "END",
		},
		output.CompactedLineFormat: {
			marker: "Compacted:",
			setup: func(t *testing.T) string {
				t.Helper()
				writeBeadsConfig(t, `{"compaction":{"retention_days":1}}`)
				id := mustCreateBead(t, "--title", "t", "--summary", "S1", "--description", "long body text")
				if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
					t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
				}
				t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")
				return id
			},
			plain:    []string{"show"},
			followed: []string{"show", "--full"},
			full:     "long body text",
		},
	}
}

func guidanceLiterals(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, dir := range []string{".", filepath.Join("..", "..", "internal", "output")} {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), p, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err == nil && strings.Contains(s, " to see ") {
					found = append(found, s)
				}
				return true
			})
		}
	}
	return found
}

func TestGuidanceLinesAreAllInTable(t *testing.T) {
	cases := guidanceCases()
	lits := guidanceLiterals(t)
	if len(lits) == 0 {
		t.Fatal("found no guidance literals; the scan is broken")
	}
	for _, s := range lits {
		if _, ok := cases[s]; !ok {
			t.Errorf("guidance %q is not in guidanceCases; add a row proving its flag removes the truncation", s)
		}
	}
}

func TestGuidanceFlagRemovesTruncation(t *testing.T) {
	for line, c := range guidanceCases() {
		t.Run(c.marker, func(t *testing.T) {
			initBeadsDir(t)
			id := c.setup(t)

			out, errS, code := runCmd(t, append(append([]string{}, c.plain...), id), "")
			if code != 0 {
				t.Fatalf("%v: exit code = %d, stderr = %q", c.plain, code, errS)
			}
			if !strings.Contains(out, c.marker) {
				t.Fatalf("%v output = %q, want the guidance %q", c.plain, out, line)
			}

			out, errS, code = runCmd(t, append(append([]string{}, c.followed...), id), "")
			if code != 0 {
				t.Fatalf("%v: exit code = %d, stderr = %q", c.followed, code, errS)
			}
			if strings.Contains(out, c.marker) {
				t.Errorf("%v output = %q, want no %q after following the guidance", c.followed, out, c.marker)
			}
			if !strings.Contains(out, c.full) {
				t.Errorf("%v output = %q, want the full text %q", c.followed, out, c.full)
			}
		})
	}
}
