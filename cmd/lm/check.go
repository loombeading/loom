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
)

const checkUnreadable = 2

func runCheck(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runGroupDispatch("check", args, stdin, stdout, stderr,
		[]groupSub{{name: "gate-cycles", run: withStdin(runCheckGateCycles)}},
		nil, "lm check requires a subcommand")
}

func runCheckGateCycles(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check gate-cycles", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	if err := fs.Parse(args); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() != 0 {
		output.WriteError(stderr, "lm check gate-cycles takes no positional arguments")
		return 1
	}

	report, err := readGateCycles()
	if err != nil {
		output.WriteError(stderr, err.Error())
		return checkUnreadable
	}
	writeCheckSection(stdout, "Cycles", report.Cycles)
	fmt.Fprintln(stdout)
	writeCheckSection(stdout, "No owner", report.NoOwner)
	if report.Empty() {
		return 0
	}
	return 1
}

func readGateCycles() (domain.GateCycleReport, error) {
	db, _, err := openDB(os.Getenv, "")
	if err != nil {
		return domain.GateCycleReport{}, err
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	tx, err := db.SQL.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.GateCycleReport{}, err
	}
	defer func() { _ = tx.Rollback() }()
	return domain.GateCycles(ctx, tx)
}

func writeCheckSection(w io.Writer, title string, lines []string) {
	fmt.Fprintf(w, "## %s\n", title)
	if len(lines) == 0 {
		fmt.Fprintln(w, "(none)")
	}
	for _, l := range lines {
		fmt.Fprintf(w, "- %s\n", l)
	}
}
