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
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

var errHoldDryRun = errors.New("hold cancel: dry run")

func runHold(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runGroupDispatch("hold", args, stdin, stdout, stderr, []groupSub{
		{"cancel", runHoldCancel},
	}, nil, "lm hold requires a subcommand (cancel)")
}

func runHoldCancel(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hold cancel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	dryRun := fs.Bool("dry-run", false, "list what would be cancelled without writing")
	namespaceFlag := fs.String(flagNamespace, "", "filter by namespace")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() != 0 {
		output.WriteError(stderr, "lm hold cancel takes no arguments")
		fmt.Fprintln(stderr, "See: lm help hold cancel")
		return 1
	}
	namespaceSet := false
	fs.Visit(func(f *flag.Flag) { namespaceSet = namespaceSet || f.Name == flagNamespace })

	db, cfg, err := openDB(os.Getenv, "")
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	n, err := domain.LoadShortIDs(ctx, db.SQL)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	namespace, err := domain.ResolveNamespaceForFilter(*namespaceFlag, namespaceSet, os.Getenv, cfg.NamespaceDefault)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	clock, err := domain.Now(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	in := domain.HoldCancelInput{
		Filter:    domain.ReadyFilter{Namespace: namespace, Priority: -1},
		Actor:     actor("", os.Getenv),
		Now:       storage.NowRFC3339Milli(),
		At:        domain.FormatTime(clock),
		Clock:     clock,
		DisplayID: func(b domain.Bead) string { return domain.DisplayID(b.Namespace, b.ID, n) },
	}

	var res domain.HoldCancelResult
	err = db.WithWrite(ctx, func(tx *sql.Tx) error {
		var herr error
		res, herr = domain.HoldCancelRun(ctx, tx, in)
		if herr == nil && *dryRun {
			return errHoldDryRun
		}
		return herr
	})
	if err != nil && !errors.Is(err, errHoldDryRun) {
		if reportIfBusy(stderr, db.BusyTimeoutMS, err) || reportIfReadOnly(stderr, "hold cancel", err) {
			return 1
		}
		output.WriteError(stderr, err.Error())
		return 1
	}

	if *dryRun {
		fmt.Fprintln(stdout, "Dry run: nothing written")
	}
	if res.Mode != domain.TimeAdjustOn {
		output.WriteSkipped(stdout, res.Mode)
	}
	if err := writeHoldCancelled(ctx, db.SQL, stdout, n, res); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	newly := make(map[string]bool, len(res.Newly))
	for _, id := range res.Newly {
		newly[id] = true
	}
	if err := writeNewlyReady(ctx, db.SQL, stdout, n, newly, cfg.NamespaceDefault); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	return 0
}

func writeHoldCancelled(ctx context.Context, q domain.Querier, w io.Writer, n domain.ShortIDs, res domain.HoldCancelResult) error {
	beads := make([]domain.Bead, len(res.Cancelled))
	since := make([]string, len(res.Cancelled))
	ids := make([]string, len(res.Cancelled))
	for i, c := range res.Cancelled {
		beads[i], since[i], ids[i] = c.Bead, domain.FormatTime(c.HeldSince), c.Bead.ID
	}
	resolver, err := domain.NewAliasResolverFor(ctx, q, ids)
	if err != nil {
		return err
	}
	rows, err := beadRowsUnsorted(beads, n, resolver)
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].Status = domain.StatusCancelled
	}
	output.WriteHoldCancelled(w, rows, since)
	return nil
}
