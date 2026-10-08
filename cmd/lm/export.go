// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
)

func runExport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == helpFlag) {
		fmt.Fprintln(stderr, "Usage: lm export [flags] <args>")
		fmt.Fprintln(stderr, commandDesc("export"))
		return 0
	}
	if len(args) != 0 {
		output.WriteError(stderr, "lm export takes no arguments")
		return 1
	}

	dir, cfg, err := resolveDirAndConfig(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	db, err := openDBAt(dir, os.Getenv, "", cfg)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = db.Close() }()

	if err := domain.Export(context.Background(), db.SQL, stdout); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	return 0
}
