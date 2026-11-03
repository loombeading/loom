// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func runCoefficient(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runGroupDispatch("coefficient", args, stdin, stdout, stderr, []groupSub{
		{"show", runCoefficientShow},
		{"set", runCoefficientSet},
	}, nil, "lm coefficient requires a subcommand (show, set)")
}

func runCoefficientShow(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("coefficient show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() != 0 {
		output.WriteError(stderr, "lm coefficient show takes no arguments")
		fmt.Fprintln(stderr, "See: lm help coefficient show")
		return 1
	}

	db, _, err := openDB(os.Getenv, "")
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = db.Close() }()

	rows, err := db.SQL.QueryContext(context.Background(), `SELECT key, value, value_set_at FROM coefficients ORDER BY key`)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = rows.Close() }()
	fmt.Fprintln(stdout, "## Coefficients")
	n := 0
	for rows.Next() {
		var key, value, setAt string
		if err := rows.Scan(&key, &value, &setAt); err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
		fmt.Fprintf(stdout, "- %s = %s (set %s)\n", output.EscapeInline(key), output.EscapeInline(value), setAt)
		n++
	}
	if err := rows.Err(); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	if n == 0 {
		fmt.Fprintln(stdout, "(none)")
	}
	return 0
}

func runCoefficientSet(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("coefficient set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	reason := fs.String(flagReason, "", `reason to record, or "-" to read from stdin`)
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() != 2 {
		output.WriteError(stderr, "lm coefficient set requires <key> <value>")
		fmt.Fprintln(stderr, "See: lm help coefficient set")
		return 1
	}
	reasonVal, err := readBodyFlag(*reason, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	db, _, err := openDB(os.Getenv, *actorFlag)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	key, value := fs.Arg(0), fs.Arg(1)
	actorVal := actor(*actorFlag, os.Getenv)
	now := storage.NowRFC3339Milli()
	var prev string
	err = db.WithWrite(ctx, func(tx *sql.Tx) error {
		var serr error
		prev, serr = domain.SetCoefficient(ctx, tx, key, value, reasonVal, actorVal, now)
		return serr
	})
	if err != nil {
		if reportIfReadOnly(stderr, key, err) {
			return 1
		}
		output.WriteError(stderr, err.Error())
		return 1
	}

	fmt.Fprintf(stdout, "- Coefficient set: %s = %s (was %s)\n", output.EscapeInline(key), output.EscapeInline(value), output.EscapeInline(prev))
	return 0
}
