// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var docRefPattern = regexp.MustCompile(`\b(PRD|EDD)\b|[0-9]章|Phase [0-9]`)

func assertNoDocRefs(t *testing.T, label, out string) {
	t.Helper()
	if m := docRefPattern.FindString(out); m != "" {
		t.Errorf("%s: output contains a design-document reference (%q): %s", label, m, out)
	}
}

func TestRegistryCoverageDispatch(t *testing.T) {
	for _, c := range registry {
		t.Run(c.name, func(t *testing.T) {
			args := []string{c.name, "--help"}
			_, errS, code := runCmd(t, args, "")
			if strings.Contains(errS, "unknown subcommand") {
				t.Errorf("%q: dispatch reports unknown subcommand (stderr = %q)", c.name, errS)
			}
			if code != 0 {
				t.Errorf("%q --help: exit code = %d, stderr = %q", c.name, code, errS)
			}

			for _, s := range c.sub {
				subArgs := []string{c.name, s.name, "--help"}
				_, subErrS, subCode := runCmd(t, subArgs, "")
				if strings.Contains(subErrS, "unknown") {
					t.Errorf("%q: sub-dispatch reports unknown (stderr = %q)", strings.Join(subArgs, " "), subErrS)
				}
				if subCode != 0 {
					t.Errorf("%v: exit code = %d, stderr = %q", subArgs, subCode, subErrS)
				}
			}
		})
	}
}

func TestRegistryCoverageHelp(t *testing.T) {
	out, _, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("help: exit code = %d", code)
	}

	for _, c := range registry {
		if len(c.sub) == 0 {
			want := "- " + c.name + " —"
			if !strings.Contains(out, want) {
				t.Errorf("lm help output missing line for %q (want a line starting %q)", c.name, want)
			}
			continue
		}
		for _, s := range c.sub {
			want := "- " + c.name + " " + s.name + " —"
			if !strings.Contains(out, want) {
				t.Errorf("lm help output missing line for %q (want a line starting %q)", c.name+" "+s.name, want)
			}
		}
	}
}

func TestRegistryCoverageHelpIncludesHelpAndCompletion(t *testing.T) {
	out, _, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("help: exit code = %d", code)
	}
	if !strings.Contains(out, helpCommandLine) {
		t.Errorf("lm help output missing help's own line (want %q)", helpCommandLine)
	}
	if !strings.Contains(out, completionCommandLine) {
		t.Errorf("lm help output missing completion's own line (want %q)", completionCommandLine)
	}
}

func TestRegistryCoverageCompletion(t *testing.T) {
	zshOut, _, code := runCmd(t, []string{"completion", "zsh"}, "")
	if code != 0 {
		t.Fatalf("completion zsh: exit code = %d", code)
	}
	bashOut, _, code := runCmd(t, []string{"completion", "bash"}, "")
	if code != 0 {
		t.Fatalf("completion bash: exit code = %d", code)
	}

	for _, c := range registry {
		if !strings.Contains(zshOut, "'"+c.name+":") && !strings.Contains(zshOut, c.name+")") {
			t.Errorf("zsh completion missing command %q", c.name)
		}
		if !strings.Contains(bashOut, c.name) {
			t.Errorf("bash completion missing command %q", c.name)
		}
		for _, s := range c.sub {
			if !strings.Contains(zshOut, "'"+s.name+":") && !strings.Contains(zshOut, s.name+")") {
				t.Errorf("zsh completion missing subcommand %q %q", c.name, s.name)
			}
			if !strings.Contains(bashOut, s.name) {
				t.Errorf("bash completion missing subcommand %q %q", c.name, s.name)
			}
		}
	}
}

func TestRegistryCoverageCompletionIncludesHelpAndCompletion(t *testing.T) {
	zshOut, _, code := runCmd(t, []string{"completion", "zsh"}, "")
	if code != 0 {
		t.Fatalf("completion zsh: exit code = %d", code)
	}
	bashOut, _, code := runCmd(t, []string{"completion", "bash"}, "")
	if code != 0 {
		t.Fatalf("completion bash: exit code = %d", code)
	}

	for _, name := range []string{"help", "completion"} {
		if !strings.Contains(zshOut, "'"+name+":") && !strings.Contains(zshOut, name+")") {
			t.Errorf("zsh completion missing command %q", name)
		}
		if !strings.Contains(bashOut, name) {
			t.Errorf("bash completion missing command %q", name)
		}
	}
}

func TestRegistryEveryDispatchNameHasEntry(t *testing.T) {
	h := handlers()
	reg := registry
	if len(h) != len(reg) {
		t.Fatalf("handlers() has %d entries, registry has %d", len(h), len(reg))
	}
	for _, c := range reg {
		if _, ok := h[c.name]; !ok {
			t.Errorf("registry entry %q missing from handlers()", c.name)
		}
	}
}

func TestHelpAndCompletionHaveNoDocReferences(t *testing.T) {
	out, _, code := runCmd(t, []string{"help"}, "")
	if code != 0 {
		t.Fatalf("help: exit code = %d", code)
	}
	assertNoDocRefs(t, "lm help", out)

	for _, c := range registry {
		out, _, code := runCmd(t, []string{"help", c.name}, "")
		if code != 0 {
			t.Errorf("lm help %s: exit code = %d", c.name, code)
		}
		assertNoDocRefs(t, "lm help "+c.name, out)

		for _, s := range c.sub {
			out, _, code := runCmd(t, []string{"help", c.name, s.name}, "")
			if code != 0 {
				t.Errorf("lm help %s %s: exit code = %d", c.name, s.name, code)
			}
			assertNoDocRefs(t, "lm help "+c.name+" "+s.name, out)
		}
	}

	zshOut, _, code := runCmd(t, []string{"completion", "zsh"}, "")
	if code != 0 {
		t.Fatalf("completion zsh: exit code = %d", code)
	}
	assertNoDocRefs(t, "lm completion zsh", zshOut)

	bashOut, _, code := runCmd(t, []string{"completion", "bash"}, "")
	if code != 0 {
		t.Fatalf("completion bash: exit code = %d", code)
	}
	assertNoDocRefs(t, "lm completion bash", bashOut)
}

func TestNoOldUnavailableEnvVarNameRemains(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	self, err := filepath.Abs("registry_test.go")
	if err != nil {
		t.Fatalf("resolve self path: %v", err)
	}
	oldName := "LM_MODELS" + "_UNAVAILABLE"
	skipNames := map[string]bool{".git": true, "node_modules": true, ".loom": true}
	err = walkRepo(root, skipNames, func(path string, d fs.DirEntry) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if abs == self {
			return nil
		}
		ext := filepath.Ext(d.Name())
		if ext != ".go" && ext != ".md" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), oldName) {
			t.Errorf("%s still contains the old env var name %q (renamed to LM_UNAVAILABLE_MODELS)", path, oldName)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

var prefixNamingExclusions = map[string]bool{
	"internal/storage/schema.go": true,
}

var prefixNamingOldSpellings = []string{
	"LM_PREFIX",
	"--prefix",
	`Filter: prefix=`,
}

func TestNoOldPrefixNamingRemains(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	self, err := filepath.Abs("registry_test.go")
	if err != nil {
		t.Fatalf("resolve self path: %v", err)
	}
	skipNames := map[string]bool{".git": true, "node_modules": true, ".loom": true}
	err = walkRepo(root, skipNames, func(path string, d fs.DirEntry) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if abs == self {
			return nil
		}
		ext := filepath.Ext(d.Name())
		if ext != ".go" && ext != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		if prefixNamingExclusions[filepath.ToSlash(rel)] {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, old := range prefixNamingOldSpellings {
			if strings.Contains(string(b), old) {
				t.Errorf("%s still contains the old name %q (renamed to namespace: LM_NAMESPACE / --namespace / Filter: namespace=)", path, old)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

var xNamespaceOldSpellingPattern = regexp.MustCompile(`"x_[a-zA-Z_]+_at"|x_unknown:`)

var xNamespaceExclusions = map[string]bool{}

func TestNoXNamespaceJSONLKeysRemain(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	self, err := filepath.Abs("registry_test.go")
	if err != nil {
		t.Fatalf("resolve self path: %v", err)
	}
	skipNames := map[string]bool{".git": true, "node_modules": true, ".loom": true}
	err = walkRepo(root, skipNames, func(path string, d fs.DirEntry) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if abs == self {
			return nil
		}
		ext := filepath.Ext(d.Name())
		if ext != ".go" && ext != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		if xNamespaceExclusions[filepath.ToSlash(rel)] {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := xNamespaceOldSpellingPattern.FindString(string(b)); m != "" {
			t.Errorf("%s still contains the removed x_ namespace spelling %q (JSONL per-field timestamps are now nested under \"_set_at\"; lm show's x_unknown front matter key is now unknown)", rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}
