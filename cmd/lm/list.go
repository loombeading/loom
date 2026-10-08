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

func runList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	all := fs.Bool(flagAll, false, "include closed/cancelled Beads")
	status := fs.String(flagStatus, "", "filter by status")
	beadType := fs.String(flagType, "", "filter by type")
	priority := fs.Int(flagPriority, -1, "filter by priority (1-4); -1 means no filter")
	claimedBy := fs.String("claimed-by", "", "filter by claimed_by")
	label := fs.String(flagLabel, "", "filter by label")
	namespaceFlag := fs.String(flagNamespace, "", "filter by namespace")
	limit := fs.Int(flagLimit, output.DefaultLimit, "max rows to show (0 for unlimited)")
	sortFlag := fs.String("sort", domain.SortUpdated, "row order: updated (default, updated_at desc) or priority")
	claimExpired := fs.Bool("claim-expired", false, "only in_progress Beads whose claim has expired")
	var ff filterFlags
	ff.bind(fs)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	namespaceSet := namespaceWasSet(fs)

	if err := validatePriority(*priority); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	if *sortFlag != domain.SortPriority && *sortFlag != domain.SortUpdated {
		output.WriteError(stderr, "--sort must be \"priority\" or \"updated\"")
		return 1
	}

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

	var claimExpiredAt string
	if *claimExpired {
		if claimExpiredAt, err = domain.ClaimClockString(os.Getenv); err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
	}

	filter := domain.ListFilter{
		ClaimExpiredAt: claimExpiredAt,
		All:            *all,
		Status:         *status,
		Type:           *beadType,
		ClaimedBy:      *claimedBy,
		Label:          *label,
		Namespace:      namespace,
		ParentID:       rf.parentID,
		UpdatedBefore:  rf.updatedBefore,
		UpdatedAfter:   rf.updatedAfter,
		Limit:          *limit,
		Sort:           *sortFlag,
		Priority:       *priority,
		Now:            effNow(),
	}

	beads, eff, total, err := domain.ListBeadsEffective(ctx, db.SQL, filter)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	resolver, err := resolverFor(ctx, db.SQL, beads)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	var rows []output.BeadRow
	if *sortFlag == domain.SortUpdated {
		rows, err = beadRowsUnsorted(beads, n, resolver)
	} else {
		rows, err = beadRows(beads, n, resolver)
		if err == nil {
			err = withEffective(ctx, db.SQL, rows, eff, n)
		}
	}
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	conds := namespaceConds(namespace)
	switch {
	case *status != "":
		conds = append(conds, output.Cond{Key: flagStatus, Value: *status})
	case !*all:
		conds = append(conds, output.Cond{Key: flagStatus, Value: "unfinished"})
	}
	writeFilterHeader(stdout, conds, rf, ff)
	writeBeadListFooter(stdout, output.BuildTree(rows), total)
	return 0
}
