// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func runMigrate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() != 0 {
		output.WriteError(stderr, "lm migrate takes no positional arguments")
		return 1
	}

	dir, _, err := storage.Locate(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	result, err := storage.Migrate(context.Background(), dir, storage.OpenOptions{
		Env:   os.Getenv,
		Actor: actor(*actorFlag, os.Getenv),
	})
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	if result.Backup == "" {
		fmt.Fprintf(stdout, "- Schema: %d (already current)\n", result.To)
		return 0
	}
	fmt.Fprintf(stdout, "- Schema: %d -> %d\n- Backup: %s\n", result.From, result.To, result.Backup)
	return 0
}
