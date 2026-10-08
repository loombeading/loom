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
	"slices"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func runRework(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runGroupDispatch("rework", args, stdin, stdout, stderr, []groupSub{
		{cmdAdd, runReworkAdd},
	}, nil, "lm rework requires a subcommand (add)")
}

func runReworkAdd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rework add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	cause := fs.String("cause", "", "why the Bead was bounced back (ci, review, followup)")
	reason := fs.String(flagReason, "", `reason to record, or "-" to read from stdin`)
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() != 1 {
		output.WriteError(stderr, "lm rework add requires exactly one ID")
		fmt.Fprintln(stderr, "See: lm help rework add")
		return 1
	}
	if *cause == "" {
		output.WriteError(stderr, "--cause is required (ci, review, followup)")
		return 1
	}
	validCause := slices.Contains(domain.ReworkCauses, *cause)
	if !validCause {
		output.WriteError(stderr, fmt.Sprintf("invalid --cause %q (want ci, review, followup)", *cause))
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

	ctx := domain.WithStaleIDNotice(context.Background(), stderr)
	now := storage.NowRFC3339Milli()
	actorVal := actor(*actorFlag, os.Getenv)
	n, err := domain.LoadShortIDs(ctx, db.SQL)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	raw := fs.Arg(0)
	var id, namespace string
	err = db.WithWrite(ctx, func(tx *sql.Tx) error {
		var rerr error
		id, rerr = domain.ResolveID(ctx, tx, raw)
		if rerr != nil {
			return rerr
		}
		namespace, rerr = namespaceOf(ctx, tx, id)
		if rerr != nil {
			return rerr
		}
		return domain.AddRework(ctx, tx, domain.AddReworkInput{
			BeadID: id,
			Cause:  *cause,
			Reason: reasonVal,
			Actor:  actorVal,
			Now:    now,
		})
	})
	if err != nil {
		if reportIfReadOnly(stderr, raw, err) {
			return 1
		}
		output.WriteRejected(stdout, raw, err.Error())
		return 1
	}

	fmt.Fprintf(stdout, "- Rework recorded: %s (cause: %s)\n",
		domain.DisplayID(namespace, id, n), *cause)
	return 0
}
