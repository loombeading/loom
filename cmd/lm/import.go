// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/importer"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func runImport(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	dryRun := fs.Bool("dry-run", false, "run the merge and report the result without writing anything")
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() != 1 {
		output.WriteError(stderr, "lm import requires exactly one <path> (or \"-\" for stdin)")
		return 1
	}
	path := fs.Arg(0)

	var r io.Reader
	if path == "-" {
		r = stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
		defer func() { _ = f.Close() }()
		r = f
	}

	dir, cfg, err := resolveDirAndConfig(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	db, err := openDBAt(dir, os.Getenv, *actorFlag, cfg)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := storage.NowRFC3339Milli()
	actorVal := actor(*actorFlag, os.Getenv)
	var sum *importer.Summary
	writeErr := db.WithWrite(ctx, func(tx *sql.Tx) error {
		var ierr error
		sum, ierr = importer.Run(ctx, tx, r, importer.Options{Actor: actorVal, Now: now, ShortLen: cfg.ShortLenOr(domain.DefaultShortLen)})
		if ierr != nil {
			return ierr
		}
		if *dryRun {
			return errDryRun
		}
		return nil
	})
	if writeErr != nil && !errors.Is(writeErr, errDryRun) {
		if reportIfBusy(stderr, db.BusyTimeoutMS, writeErr) {
			return 1
		}
		output.WriteError(stderr, writeErr.Error())
		return 1
	}

	if *dryRun {
		fmt.Fprintln(stdout, "Dry run: no changes written")
	}
	fmt.Fprintf(stdout, "Imported: %d bead(s), %d dependency(ies), %d token_cost(s)\n",
		sum.NewBeads, sum.NewDeps, sum.NewTokenCosts)
	fmt.Fprintf(stdout, "Updated: %d bead(s)\n", sum.UpdatedBeads)
	if sum.SkippedUnknown > 0 {
		fmt.Fprintf(stdout, "Skipped: %d line(s) with unknown _type\n", sum.SkippedUnknown)
	}
	for _, line := range sum.CycleLines {
		fmt.Fprintln(stdout, line)
	}
	for _, line := range sum.ParentLines {
		fmt.Fprintln(stdout, line)
	}
	fmt.Fprintf(stdout, "Dangling: %d link(s)\n", sum.Dangling)
	fmt.Fprintln(stdout)

	if *dryRun {
		fmt.Fprintln(stdout, output.NewlyReadyHeading)
		fmt.Fprintln(stdout, "(none)")
	} else {
		ids := make(map[string]bool, len(sum.NewlyReady))
		for _, id := range sum.NewlyReady {
			ids[id] = true
		}
		n, err := domain.LoadShortIDs(ctx, db.SQL)
		if err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
		if err := writeNewlyReady(ctx, db.SQL, stdout, n, ids, cfg.NamespaceDefault); err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
	}

	return 0
}

var errDryRun = errors.New("dry run: rolled back")
