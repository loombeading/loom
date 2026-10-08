// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"io"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
)

func runBlocked(args []string, stdout, stderr io.Writer) int {
	fs, rf, err := parseReadyFlags("blocked", args, stderr)
	if err != nil {
		return helpExitCode(err)
	}

	db, cfg, err := openReadyDB(fs, &rf, "")
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
	f, filt, namespace, ok := resolveReadyFilter(ctx, db.SQL, rf, n, cfg.NamespaceDefault, stderr)
	if !ok {
		return 1
	}

	blocked, eff, total, err := domain.BlockedBeadsEffective(ctx, db.SQL, f)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	beads := make([]domain.Bead, len(blocked))
	for i, b := range blocked {
		beads[i] = b.Bead
	}

	var blockerIDs []string
	for _, b := range blocked {
		for _, bl := range b.Blockers {
			if bl.Text != "" {
				continue
			}
			blockerIDs = append(blockerIDs, bl.ID)
		}
	}
	resolver, err := resolverFor(ctx, db.SQL, beads, blockerIDs...)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	rows, err := beadRowsUnsorted(beads, n, resolver)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	blockedByRow := make(map[string][]string, len(blocked))
	for _, b := range blocked {
		row, err := blockedByIDs(resolver, b.Blockers, n)
		if err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
		blockedByRow[b.ID] = row
	}
	for i := range rows {
		rows[i].BlockedBy = blockedByRow[rows[i].CanonicalID]
	}
	if err := withEffective(ctx, db.SQL, rows, eff, n); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	conds := namespaceConds(namespace)
	writeFilterHeader(stdout, conds, filt, rf.filter)
	writeBeadListFooter(stdout, rows, total)
	return 0
}

func blockedByIDs(resolver *domain.AliasResolver, blockers []domain.Blocker, n domain.ShortIDs) ([]string, error) {
	ids := make([]string, len(blockers))
	for i, bl := range blockers {
		if bl.Text != "" {
			ids[i] = bl.Text
			continue
		}
		id, err := aliasOrDisplayID(resolver, bl.Namespace, bl.ID, n)
		if err != nil {
			return nil, err
		}
		if bl.IsGate {
			id = "gate:" + id
		}
		ids[i] = id
	}
	return ids, nil
}
