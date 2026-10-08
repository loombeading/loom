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
	"strings"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func runClose(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	reason := fs.String(flagReason, "", `reason to record, or "-" to read from stdin`)
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	summaryFlag := fs.String(flagSummary, "", `summary to record before closing, or "-" to read from stdin`)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() == 0 {
		output.WriteError(stderr, "lm close requires at least one ID")
		return 1
	}
	if err := rejectMultipleStdin(fs, flagSummary, flagReason); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	summarySet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == flagSummary {
			summarySet = true
		}
	})

	summaryVal, err := readCloseSummary(summarySet, fs.NArg(), *summaryFlag, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	reasonVal, err := readBodyFlag(*reason, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	db, cfg, err := openDB(os.Getenv, *actorFlag)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = db.Close() }()

	ctx := domain.WithStaleIDNotice(context.Background(), stderr)
	n, err := domain.LoadShortIDs(ctx, db.SQL)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	req := closeRequest{
		summarySet: summarySet,
		summary:    summaryVal,
		reason:     reasonVal,
		actor:      actor(*actorFlag, os.Getenv),
		now:        storage.NowRFC3339Milli(),
	}

	exit := 0
	newlyReadyIDs := map[string]bool{}
	for _, raw := range fs.Args() {
		code, fatal := closeAndReport(ctx, db, req, raw, n, newlyReadyIDs, stdout, stderr)
		if fatal {
			return 1
		}
		exit = max(exit, code)
	}

	if err := writeNewlyReady(ctx, db.SQL, stdout, n, newlyReadyIDs, cfg.NamespaceDefault); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	return exit
}

func readCloseSummary(summarySet bool, nargs int, summaryFlag string, stdin io.Reader) (string, error) {
	if !summarySet {
		return "", nil
	}
	if nargs > 1 {
		return "", errors.New("lm close --summary accepts only one ID at a time")
	}
	summaryVal, err := readBodyFlag(summaryFlag, stdin)
	if err != nil {
		return "", err
	}
	if summaryVal == "" {
		return "", errors.New("lm close --summary requires a non-empty summary")
	}
	return summaryVal, nil
}

type closeRequest struct {
	summarySet bool
	summary    string
	reason     string
	actor      string
	now        string
}

type closeResult struct {
	namespace         string
	newly             []string
	autoResolvedGates []idAndNamespace
}

func closeAndReport(ctx context.Context, db *storage.DB, req closeRequest, raw string, n domain.ShortIDs, newlyReadyIDs map[string]bool, stdout, stderr io.Writer) (int, bool) {
	id, err := domain.ResolveID(ctx, db.SQL, raw)
	if err != nil {
		output.WriteRejected(stdout, raw, err.Error())
		return 1, false
	}
	res, err := closeInTx(ctx, db, req, id)
	if err != nil {
		if reportIfBusy(stderr, db.BusyTimeoutMS, err) {
			return 1, true
		}
		if !reportIfReadOnly(stderr, raw, err) {
			writeRejected(stdout, stderr, raw, err)
		}
		return 1, false
	}
	fmt.Fprintf(stdout, "- Closed: %s\n", domain.DisplayID(res.namespace, id, n))
	for _, g := range res.autoResolvedGates {
		fmt.Fprintf(stdout, "- Auto-resolved: %s\n", domain.DisplayID(g.Namespace, g.ID, n))
	}
	derivedOpen, err := derivedOpenDisplayIDs(ctx, db.SQL, id, n)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1, true
	}
	if len(derivedOpen) > 0 {
		fmt.Fprintf(stdout, "- Derived (open): %s\n", strings.Join(derivedOpen, ", "))
	}
	for _, nid := range res.newly {
		newlyReadyIDs[nid] = true
	}
	return 0, false
}

func closeInTx(ctx context.Context, db *storage.DB, req closeRequest, id string) (closeResult, error) {
	var res closeResult
	err := db.WithWrite(ctx, func(tx *sql.Tx) error {
		var err error
		res.namespace, err = namespaceOf(ctx, tx, id)
		if err != nil {
			return err
		}
		res.newly, err = domain.NewlyReadyDiff(ctx, tx, func() error {
			var cerr error
			res.autoResolvedGates, cerr = closeBeadTx(ctx, tx, req, id)
			return cerr
		})
		return err
	})
	return res, err
}

func closeBeadTx(ctx context.Context, tx *sql.Tx, req closeRequest, id string) ([]idAndNamespace, error) {
	if req.summarySet {
		if _, err := domain.UpdateBead(ctx, tx, domain.UpdateInput{
			ID:       id,
			Priority: -1,
			Summary:  req.summary,
			Reason:   req.reason,
			Actor:    req.actor,
			Now:      req.now,
		}); err != nil {
			return nil, err
		}
	}
	if err := domain.CloseBead(ctx, tx, domain.CloseInput{
		ID:     id,
		Reason: req.reason,
		Actor:  req.actor,
		Now:    req.now,
	}); err != nil {
		return nil, err
	}
	gateIDs, err := domain.AutoResolveGatesForBead(ctx, tx, id, req.actor, req.now)
	if err != nil {
		return nil, err
	}
	var gates []idAndNamespace
	for _, gateID := range gateIDs {
		gateNamespace, err := namespaceOf(ctx, tx, gateID)
		if err != nil {
			return nil, err
		}
		gates = append(gates, idAndNamespace{ID: gateID, Namespace: gateNamespace})
	}
	return gates, nil
}

func derivedOpenDisplayIDs(ctx context.Context, q domain.Querier, id string, n domain.ShortIDs) ([]string, error) {
	sent, err := domain.SentLinks(ctx, q, id, false)
	if err != nil {
		return nil, err
	}
	var derivedIDs []string
	for _, l := range sent {
		if l.Type == domain.DiscoveredFromDepType {
			derivedIDs = append(derivedIDs, l.DependsOnID)
		}
	}
	if len(derivedIDs) == 0 {
		return nil, nil
	}

	resolver, err := domain.NewAliasResolverFor(ctx, q, derivedIDs)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, did := range derivedIDs {
		b, err := domain.GetBead(ctx, q, did)
		if errors.Is(err, domain.ErrBeadNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if b.Status != domain.StatusOpen {
			continue
		}
		display := domain.DisplayID(b.Namespace, b.ID, n)
		if alias, hasParent, aerr := resolver.Alias(did); aerr != nil {
			return nil, aerr
		} else if hasParent {
			display = alias
		}
		out = append(out, display)
	}
	return out, nil
}
