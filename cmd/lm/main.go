// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/loombeading/loom/internal/config"
	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
	"github.com/loombeading/loom/internal/xdg"
)

var version = "dev"

var readBuildInfo = debug.ReadBuildInfo

func resolveVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return version
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return version
}

func resolveVCS() (string, string) {
	commit, date := "-", "-"
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return commit, date
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if v := s.Value; v != "" {
				if len(v) > 9 {
					v = v[:9]
				}
				commit = v
			}
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				date = t.Format("2006-01-02")
			}
		}
	}
	return commit, date
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var outBuf, errBuf bytes.Buffer
	code := dispatch(args, stdin, &outBuf, &errBuf)

	decorate := !isHelpOrCompletion(args) && output.ColorEnabled(stdout, os.Getenv)
	writePlainOrDecorated(stdout, outBuf.String(), decorate)
	writePlainOrDecorated(stderr, errBuf.String(), decorate)
	return code
}

func isHelpOrCompletion(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "help", helpFlag, "-h", "completion":
		return true
	}
	for _, a := range args[1:] {
		if a == helpFlag || a == "-h" {
			return true
		}
		if a == "--" {
			break
		}
	}
	return false
}

func writePlainOrDecorated(w io.Writer, s string, decorate bool) {
	if s == "" {
		return
	}
	if decorate {
		s = output.Decorate(s)
	}
	fmt.Fprint(w, s)
}

func dispatch(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeHelp(stderr)
		return 1
	}

	switch args[0] {
	case "help":
		return runHelp(args[1:], stdin, stdout, stderr)
	case helpFlag, "-h":
		writeHelp(stdout)
		return 0
	case "completion":
		return runCompletion(args[1:], stdout, stderr)
	}

	if c, ok := handlers()[args[0]]; ok {
		return c.run(args[1:], stdin, stdout, stderr)
	}

	fmt.Fprintf(stderr, "Error: unknown subcommand `%s`\n", args[0])
	fmt.Fprintln(stderr, "See: lm help")
	return 1
}

func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	if err := fs.Parse(args); err != nil {
		return helpExitCode(err)
	}

	dir, _, err := storage.ResolveInitDir(os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", err)
		return 1
	}

	result, err := storage.Init(context.Background(), dir, storage.OpenOptions{
		Env:   os.Getenv,
		Actor: actor("", os.Getenv),
	})
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "- Database: %s\n", result.DBPath)
	return 0
}

func runWhere(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("where", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	if err := fs.Parse(args); err != nil {
		return helpExitCode(err)
	}

	dir, via, cfg, cfgPath, cfgFound, err := resolveDir(os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", err)
		return 1
	}
	db, err := openDBAt(dir, os.Getenv, "", cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	fmt.Fprintf(stdout, "- Database: %s\n", storage.DBPath(dir))
	fmt.Fprintf(stdout, "- Resolved via: %s\n", via)
	cfgState := "not found"
	if cfgFound {
		cfgState = "loaded"
	}
	fmt.Fprintf(stdout, "- Config: %s (%s)\n", cfgPath, cfgState)
	if db.ReadOnly {
		fmt.Fprintf(stdout, "- Readonly: yes (mode=%s)\n", db.Mode)
	}
	dbVersion, err := storage.UserVersion(context.Background(), db)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "- Schema: %d (binary expects %d)\n", dbVersion, storage.SchemaVersion)
	return 0
}

func resolveDir(env storage.Env) (string, string, *config.Config, string, bool, error) {
	dir, via, err := storage.Locate(env)
	if err != nil {
		return "", "", nil, "", false, err
	}
	cfgDir, _, err := xdg.ConfigHome(env, "loom")
	if err != nil {
		return "", "", nil, "", false, err
	}
	cfgPath := filepath.Join(cfgDir, config.FileName)
	cfg, cfgFound, err := config.Load(cfgPath)
	if err != nil {
		return "", "", nil, "", false, err
	}
	return dir, via, cfg, cfgPath, cfgFound, nil
}

func resolveDirAndConfig(env storage.Env) (string, *config.Config, error) {
	dir, via, cfg, cfgPath, cfgFound, err := resolveDir(env)
	_, _ = via, cfgPath
	_ = cfgFound
	return dir, cfg, err
}

func openDBAt(dir string, env storage.Env, actorFlag string, cfg *config.Config) (*storage.DB, error) {
	return storage.Open(context.Background(), dir, storage.OpenOptions{
		Env:           env,
		Actor:         actor(actorFlag, env),
		BusyTimeoutMS: cfg.BusyTimeoutMSOr(storage.DefaultBusyTimeoutMS),
	})
}

func openDB(env storage.Env, actorFlag string) (*storage.DB, *config.Config, error) {
	dir, cfg, err := resolveDirAndConfig(env)
	if err != nil {
		return nil, nil, err
	}
	db, err := openDBAt(dir, env, actorFlag, cfg)
	return db, cfg, err
}

func actor(flag string, env func(string) string) string {
	osUsername := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		osUsername = u.Username
	}
	return domain.ResolveActor(flag, env, osUsername)
}

func namespaceOf(ctx context.Context, q domain.Querier, id string) (string, error) {
	var namespace string
	err := q.QueryRowContext(ctx, `SELECT namespace FROM beads WHERE id = ?`, id).Scan(&namespace)
	return namespace, err
}

type idAndNamespace struct {
	ID        string
	Namespace string
}
