// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/storage"
)

type readCountingReader struct{ reads int }

func (r *readCountingReader) Read([]byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

func TestEDD53NoSubcommandReadsStdinWithoutDash(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")
	cases := [][]string{
		{"where"},
		{"list"},
		{"ready"},
		{"blocked"},
		{"gate"},
		{"ahead"},
		{"show", a},
		{"search", "a"},
		{"export"},
		{"create", "--title", "c", "--description", "d"},
		{"update", a, "--reason", "r"},
		{"update", a, "--claim"},
		{"dep", "add", a, b},
		{"dep", "remove", a, b},
		{"gate", "sweep"},
		{"close", a, "--reason", "r"},
	}
	for _, args := range cases {
		in := &readCountingReader{}
		var out, errBuf bytes.Buffer
		code := run(args, in, &out, &errBuf)
		if code != 0 {
			t.Errorf("%v: exit code = %d, stderr = %q", args, code, errBuf.String())
		}
		if in.reads != 0 {
			t.Errorf("%v read stdin %d time(s) without a '-' flag value", args, in.reads)
		}
	}
}

func TestEDD53NoProcessSpawnAPIInLmSources(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: could not determine source location")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	banned := map[string][]string{
		"os":      {"StartProcess"},
		"syscall": {"Exec", "ForkExec", "StartProcess"},
	}
	skip := filepath.Join(repoRoot, "cmd", "harness")
	scanned := 0
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.Walk(filepath.Join(repoRoot, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if path == skip {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scanned++
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if slices.Contains(banned[pkg.Name], sel.Sel.Name) {
					t.Errorf("%s uses %s.%s; lm must not launch external processes", path, pkg.Name, sel.Sel.Name)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if scanned == 0 {
		t.Fatal("no lm source files were scanned")
	}
}

func holdWriteLock(t *testing.T, dbDir, id string, busyTimeoutMS int) {
	t.Helper()
	holder, err := storage.Open(context.Background(), dbDir, storage.OpenOptions{
		Env:           func(string) string { return "" },
		Actor:         "holder",
		BusyTimeoutMS: busyTimeoutMS,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	holding := make(chan struct{})
	release := make(chan struct{})
	holderErr := make(chan error, 1)
	go func() {
		holderErr <- holder.WithWrite(context.Background(), func(tx *sql.Tx) error {
			close(holding)
			<-release
			_, err := tx.ExecContext(context.Background(), `UPDATE beads SET title = title WHERE id = ?`, id)
			return err
		})
	}()
	<-holding
	t.Cleanup(func() {
		close(release)
		if err := <-holderErr; err != nil {
			t.Errorf("holder: %v", err)
		}
		_ = holder.Close()
	})
}

func TestEDD6BusyFixedLineOnEveryListedWriteCommand(t *testing.T) {
	const busyTimeoutMS = 200
	want := "Busy: lock wait exceeded 200ms\n"
	cases := []struct {
		name  string
		args  func(a, b string) []string
		stdin string
	}{
		{"create", func(a, b string) []string { return []string{"create", "--title", "x"} }, ""},
		{"update", func(a, b string) []string { return []string{"update", a, "--reason", "r"} }, ""},
		{"close", func(a, b string) []string { return []string{"close", a, "--reason", "r"} }, ""},
		{"dep add", func(a, b string) []string { return []string{"dep", "add", a, b} }, ""},
		{"dep remove", func(a, b string) []string { return []string{"dep", "remove", a, b} }, ""},
		{"import", func(a, b string) []string { return []string{"import", "-"} }, ""},
		{"ready --claim", func(a, b string) []string { return []string{"ready", "--claim"} }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			initBeadsDir(t)
			writeBeadsConfig(t, fmt.Sprintf(`{"db":{"busy_timeout_ms":%d}}`, busyTimeoutMS))
			a := mustCreateBead(t, "--title", "a")
			b := mustCreateBead(t, "--title", "b")
			holdWriteLock(t, beadsDir(t), a, busyTimeoutMS)

			_, stderr, code := runCmd(t, c.args(a, b), c.stdin)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (stderr = %q)", code, stderr)
			}
			if stderr != want {
				t.Fatalf("stderr = %q, want %q", stderr, want)
			}
		})
	}
}

func TestEDD6BusyStopsBatchLoop(t *testing.T) {
	const busyTimeoutMS = 200
	for _, verb := range []string{"update", "close"} {
		t.Run(verb, func(t *testing.T) {
			initBeadsDir(t)
			writeBeadsConfig(t, fmt.Sprintf(`{"db":{"busy_timeout_ms":%d}}`, busyTimeoutMS))
			a := mustCreateBead(t, "--title", "a")
			b := mustCreateBead(t, "--title", "b")
			bAlias := mustDisplayAlias(t, b)
			holdWriteLock(t, beadsDir(t), a, busyTimeoutMS)

			stdout, stderr, code := runCmd(t, []string{verb, a, b, "--reason", "r"}, "")
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (stderr = %q)", code, stderr)
			}
			if n := strings.Count(stderr, "Busy: lock wait exceeded 200ms\n"); n != 1 || strings.Count(stderr, "\n") != 1 {
				t.Fatalf("stderr = %q, want exactly one Busy line", stderr)
			}
			if strings.Contains(stdout, bAlias) || strings.Contains(stderr, bAlias) {
				t.Fatalf("the second ID %s was reported after the Busy abort (stdout = %q, stderr = %q)", bAlias, stdout, stderr)
			}
		})
	}
}

func TestEDD6ReadOnlyRejectsEveryWriteOnStderrAndWritesNothing(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")
	gate := mustGateCreateCmd(t, []string{a}, "wait", "--kind", "external", "--resolver", "r")
	before, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export: exit code = %d, stderr = %q", code, errS)
	}

	makeDBReadOnly(t)

	writes := [][]string{
		{"create", "--title", "x"},
		{"update", a, "--reason", "r"},
		{"update", a, "--claim"},
		{"close", b, "--reason", "r"},
		{"dep", "add", a, b},
		{"dep", "remove", a, b},
		{"cost", "add", a, "--in", "1", "--out", "1"},
		{"gate", "create", "--kind", "external", "--resolver", "r", "--subject", "s", "--blocks", b},
		{"gate", "sweep"},
		{"gate", "resolve", gate, "--reason", "r"},
		{"ready", "--claim"},
		{"import", "-"},
		{"coefficient", "set", "due_lead", "48h", "--reason", "r"},
		{"hold", "cancel"},
		{"migrate"},
	}
	for _, args := range writes {
		out, errS, code := runCmd(t, args, "")
		if code != 1 {
			t.Errorf("%v on a read-only DB: exit code = %d, want 1 (stdout = %q, stderr = %q)", args, code, out, errS)
		}
		if !strings.Contains(errS, "read-only database") {
			t.Errorf("%v on a read-only DB: stderr = %q, want it to say the database is read-only", args, errS)
		}
	}

	if err := os.Chmod(beadsDBPath(t), 0o644); err != nil {
		t.Fatal(err)
	}
	after, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export after: exit code = %d, stderr = %q", code, errS)
	}
	if after != before {
		t.Errorf("export changed after rejected writes:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestEDD6ReadOnlyHasNoEnvOrFlagSwitch(t *testing.T) {
	initBeadsDir(t)
	t.Setenv("LM_READONLY", "1")

	out, errS, code := runCmd(t, []string{"where"}, "")
	if code != 0 {
		t.Fatalf("where: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Readonly:") {
		t.Errorf("where = %q, want no Readonly line on a writable DB", out)
	}
	if _, errS, code := runCmd(t, []string{"create", "--title", "t"}, ""); code != 0 {
		t.Errorf("create with LM_READONLY=1: exit code = %d, stderr = %q, want a normal write", code, errS)
	}
	_, errS, code = runCmd(t, []string{"where", "--readonly"}, "")
	if code == 0 {
		t.Errorf("where --readonly: exit code = 0, want a flag error")
	}
	if !strings.Contains(errS, "flag provided but not defined") {
		t.Errorf("where --readonly: stderr = %q, want %q", errS, "flag provided but not defined")
	}
}

func TestEDD6WhereReportsImmutableMode(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	initBeadsDir(t)
	dir := beadsDir(t)
	_ = os.Remove(storage.DBPath(dir) + "-wal")
	_ = os.Remove(storage.DBPath(dir) + "-shm")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	out, errS, code := runCmd(t, []string{"where"}, "")
	if code != 0 {
		t.Fatalf("where: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Readonly: yes (mode=immutable)") {
		t.Errorf("where = %q, want %q", out, "- Readonly: yes (mode=immutable)")
	}
	if _, errS, code := runCmd(t, []string{"list"}, ""); code != 0 {
		t.Errorf("list on an immutable DB: exit code = %d, stderr = %q", code, errS)
	}
}

func TestEDD6LmDependenciesHaveNoNetworkClient(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: runs go list")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: could not determine source location")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	absent := []string{"net/http", "net/rpc", "net/smtp", "crypto/tls"}
	for _, goos := range []string{"linux", "darwin"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			t.Run(goos+"_"+goarch, func(t *testing.T) {
				cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", `{{.ImportPath}} {{join .Imports " "}}`, "./cmd/lm")
				cmd.Dir = repoRoot
				cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("go list: %v\n%s", err, stderr.String())
				}
				var importersOfNet, importersOfExec []string
				for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
					fields := strings.Fields(line)
					if len(fields) == 0 {
						continue
					}
					if slices.Contains(absent, fields[0]) {
						t.Errorf("lm depends on %s", fields[0])
					}
					if slices.Contains(fields[1:], "net") {
						importersOfNet = append(importersOfNet, fields[0])
					}
					if slices.Contains(fields[1:], "os/exec") {
						importersOfExec = append(importersOfExec, fields[0])
					}
				}
				if want := []string{"github.com/google/uuid"}; !slices.Equal(importersOfNet, want) {
					t.Errorf("direct importers of net = %v, want %v", importersOfNet, want)
				}
				if want := []string{"modernc.org/libc"}; !slices.Equal(importersOfExec, want) {
					t.Errorf("direct importers of os/exec = %v, want %v", importersOfExec, want)
				}
			})
		}
	}
}
