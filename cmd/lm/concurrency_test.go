// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/loombeading/loom/internal/config"
	"github.com/loombeading/loom/internal/storage"
)

const concurrentWorkerCount = 12

func buildBDBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binPath := filepath.Join(dir, "lm")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: could not determine source location")
	}
	pkgDir := filepath.Dir(thisFile)

	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binPath, ".")
	cmd.Dir = pkgDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build lm: %v\n%s", err, out)
	}
	return binPath
}

func subprocessEnv(dbDir string) []string {
	return append(os.Environ(), "LM_DIR="+dbDir, "XDG_CONFIG_HOME="+filepath.Dir(dbDir))
}

func initBeadsDirViaBinary(t *testing.T, binPath, dbDir string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binPath, "init")
	cmd.Env = subprocessEnv(dbDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lm init: %v\n%s", err, out)
	}
}

func runBinary(t *testing.T, binPath, beadsDir string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binPath, args...)
	cmd.Env = subprocessEnv(beadsDir)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err == nil {
		return outBuf.String(), errBuf.String(), 0
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return outBuf.String(), errBuf.String(), exitErr.ExitCode()
	}
	t.Fatalf("exec %v: %v (stderr: %s)", args, err, errBuf.String())
	return "", "", -1
}

func concurrentRun(t *testing.T, binPath, beadsDir string, n int, argsFor func(i int) []string) ([]string, []string, []int) {
	t.Helper()
	cmds := make([]*exec.Cmd, n)
	outs := make([]bytes.Buffer, n)
	errs := make([]bytes.Buffer, n)

	for i := range n {
		cmd := exec.CommandContext(t.Context(), binPath, argsFor(i)...)
		cmd.Env = subprocessEnv(beadsDir)
		cmd.Stdout = &outs[i]
		cmd.Stderr = &errs[i]
		cmds[i] = cmd
	}
	for i := range n {
		if err := cmds[i].Start(); err != nil {
			t.Fatalf("start worker %d: %v", i, err)
		}
	}

	stdouts := make([]string, n)
	stderrs := make([]string, n)
	codes := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			err := cmds[i].Wait()
			stdouts[i] = outs[i].String()
			stderrs[i] = errs[i].String()
			if err == nil {
				codes[i] = 0
				return
			}
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
				codes[i] = exitErr.ExitCode()
				return
			}
			t.Errorf("worker %d: %v", i, err)
		}(i)
	}
	wg.Wait()
	return stdouts, stderrs, codes
}

func TestConcurrentUpdateClaimExactlyOneWins(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: builds lm and execs it concurrentWorkerCount times")
	}

	binPath := buildBDBinary(t)
	dbDir := filepath.Join(t.TempDir(), storage.BeadsDirName)
	initBeadsDirViaBinary(t, binPath, dbDir)

	out, _, code := runBinary(t, binPath, dbDir, "create", "--title", "contended bead")
	if code != 0 {
		t.Fatalf("create: exit code = %d, stdout = %q", code, out)
	}
	id := mustParseCreatedID(t, out)

	_, _, codes := concurrentRun(t, binPath, dbDir, concurrentWorkerCount, func(i int) []string {
		return []string{"update", id, "--claim", "--actor", fmt.Sprintf("worker-%d", i)}
	})

	wins := 0
	for _, c := range codes {
		if c == 0 {
			wins++
		} else if c != 1 {
			t.Errorf("worker exit code = %d, want 0 or 1", c)
		}
	}
	if wins != 1 {
		t.Fatalf("exactly-one-winner claim: %d/%d workers succeeded (want exactly 1); codes=%v", wins, concurrentWorkerCount, codes)
	}

	showOut, _, code := runBinary(t, binPath, dbDir, "show", id)
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(showOut, `status: "in_progress"`) {
		t.Fatalf("show after concurrent claim = %q, want status in_progress", showOut)
	}
}

func TestConcurrentReadyClaimExactlyOneWins(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: builds lm and execs it concurrentWorkerCount times")
	}

	binPath := buildBDBinary(t)
	dbDir := filepath.Join(t.TempDir(), storage.BeadsDirName)
	initBeadsDirViaBinary(t, binPath, dbDir)

	out, _, code := runBinary(t, binPath, dbDir, "create", "--title", "single ready bead")
	if code != 0 {
		t.Fatalf("create: exit code = %d, stdout = %q", code, out)
	}
	id := mustParseCreatedID(t, out)

	stdouts, stderrs, codes := concurrentRun(t, binPath, dbDir, concurrentWorkerCount, func(i int) []string {
		return []string{"ready", "--claim", "--actor", fmt.Sprintf("worker-%d", i)}
	})

	claims := 0
	for i, c := range codes {
		if c != 0 {
			t.Fatalf("worker %d: ready --claim exit code = %d, stderr = %q", i, c, stderrs[i])
		}
		if strings.Contains(stdouts[i], "Claimed: "+id) {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("exactly-one-winner ready --claim: %d/%d workers claimed %s (want exactly 1); stdouts=%v", claims, concurrentWorkerCount, id, stdouts)
	}
}

func TestBusyTimeoutExceededReportsFixedLine(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: builds lm")
	}

	binPath := buildBDBinary(t)
	dbDir := filepath.Join(t.TempDir(), storage.BeadsDirName)
	initBeadsDirViaBinary(t, binPath, dbDir)

	const busyTimeoutMS = 200
	cfgDir := filepath.Join(filepath.Dir(dbDir), "loom")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"db":{"busy_timeout_ms":%d}}`, busyTimeoutMS)
	if err := os.WriteFile(filepath.Join(cfgDir, config.FileName), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	out, _, code := runBinary(t, binPath, dbDir, "create", "--title", "busy target")
	if code != 0 {
		t.Fatalf("create: exit code = %d, stdout = %q", code, out)
	}
	id := mustParseCreatedID(t, out)

	holder, err := storage.Open(context.Background(), dbDir, storage.OpenOptions{
		Env:           func(string) string { return "" },
		Actor:         "holder",
		BusyTimeoutMS: busyTimeoutMS,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = holder.Close() }()

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
	defer func() {
		close(release)
		if err := <-holderErr; err != nil {
			t.Errorf("holder: %v", err)
		}
	}()

	start := time.Now()
	_, stderr, code := runBinary(t, binPath, dbDir, "update", id, "--reason", "contended write")
	elapsed := time.Since(start)

	if code != 1 {
		t.Fatalf("lm update while busy: exit code = %d, want 1 (stderr = %q)", code, stderr)
	}
	want := fmt.Sprintf("Busy: lock wait exceeded %dms\n", busyTimeoutMS)
	if stderr != want {
		t.Fatalf("lm update while busy: stderr = %q, want %q", stderr, want)
	}
	if elapsed < busyTimeoutMS*time.Millisecond {
		t.Fatalf("lm update while busy returned after %v, want at least busy_timeout (%dms)", elapsed, busyTimeoutMS)
	}
}

func TestGateSweepBusyTimeoutExceededReportsFixedLineInProcess(t *testing.T) {
	initBeadsDir(t)
	dbDir := beadsDir(t)

	const busyTimeoutMS = 200
	writeBeadsConfig(t, fmt.Sprintf(`{"db":{"busy_timeout_ms":%d}}`, busyTimeoutMS))

	target := mustCreateBead(t, "--title", "gate target")
	_ = mustGateCreateCmd(t, []string{target}, "CI green")

	holder, err := storage.Open(context.Background(), dbDir, storage.OpenOptions{
		Env:           func(string) string { return "" },
		Actor:         "holder",
		BusyTimeoutMS: busyTimeoutMS,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = holder.Close() }()

	holding := make(chan struct{})
	release := make(chan struct{})
	holderErr := make(chan error, 1)
	go func() {
		holderErr <- holder.WithWrite(context.Background(), func(tx *sql.Tx) error {
			close(holding)
			<-release
			_, err := tx.ExecContext(context.Background(), `UPDATE beads SET title = title WHERE id = ?`, target)
			return err
		})
	}()
	<-holding
	defer func() {
		close(release)
		if err := <-holderErr; err != nil {
			t.Errorf("holder: %v", err)
		}
	}()

	start := time.Now()
	_, stderr, code := runCmd(t, []string{"gate", "sweep"}, "")
	elapsed := time.Since(start)

	if code != 1 {
		t.Fatalf("lm gate sweep while busy: exit code = %d, want 1 (stderr = %q)", code, stderr)
	}
	want := fmt.Sprintf("Busy: lock wait exceeded %dms\n", busyTimeoutMS)
	if stderr != want {
		t.Fatalf("lm gate sweep while busy: stderr = %q, want %q", stderr, want)
	}
	if elapsed < busyTimeoutMS*time.Millisecond {
		t.Fatalf("lm gate sweep while busy returned after %v, want at least busy_timeout (%dms)", elapsed, busyTimeoutMS)
	}
}
