// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"io"
	"os"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
)

func runSearch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	all := fs.Bool(flagAll, false, "include closed/cancelled Beads")
	namespaceFlag := fs.String(flagNamespace, "", "filter by namespace")
	limit := fs.Int(flagLimit, output.DefaultLimit, "max rows to show (0 for unlimited)")
	var ff filterFlags
	ff.bind(fs)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	namespaceSet := namespaceWasSet(fs)

	rest := fs.Args()
	if len(rest) == 0 {
		output.WriteError(stderr, "search requires exactly one word")
		return 1
	}
	if len(rest) > 1 {
		output.WriteError(stderr, "search accepts exactly one word, not multiple (no implicit AND)")
		return 1
	}
	query := rest[0]

	dir, cfg, err := resolveDirAndLimit(os.Getenv, fs, limit)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	namespace, err := domain.ResolveNamespaceForFilter(*namespaceFlag, namespaceSet, os.Getenv, cfg.NamespaceDefault)
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

	ctx := domain.WithStaleIDNotice(context.Background(), stderr)
	n, err := domain.LoadShortIDs(ctx, db.SQL)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	rf, ok := resolveFilterFlags(ctx, db.SQL, ff, n, stderr)
	if !ok {
		return 1
	}

	filter := domain.SearchFilter{
		Query:         query,
		All:           *all,
		Namespace:     namespace,
		ParentID:      rf.parentID,
		UpdatedBefore: rf.updatedBefore,
		UpdatedAfter:  rf.updatedAfter,
		Limit:         *limit,
	}

	beads, total, err := domain.SearchBeads(ctx, db.SQL, filter)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	resolver, err := resolverFor(ctx, db.SQL, beads)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	rows, err := beadRowsUnsorted(beads, n, resolver)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	conds := []output.Cond{{Key: "query", Value: query}}
	if namespace != "" {
		conds = append(conds, output.Cond{Key: flagNamespace, Value: namespace})
	}
	if !*all {
		conds = append(conds, output.Cond{Key: flagStatus, Value: "unfinished"})
	}
	conds = filterConds(conds, rf, ff)
	output.WriteFilter(stdout, conds)

	output.WriteTruncated(stdout, len(rows), total)

	output.WriteBeadList(stdout, rows)
	return 0
}
