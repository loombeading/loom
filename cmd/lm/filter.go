// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"io"

	"github.com/loombeading/loom/internal/config"
	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

type filterFlags struct {
	parent        string
	updatedBefore string
	updatedAfter  string
}

func (ff *filterFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&ff.parent, flagParent, "", "filter to direct children of this parent Bead ID")
	fs.StringVar(&ff.updatedBefore, flagUpdatedBefore, "", "filter to Beads updated before this time (RFC3339 or YYYY-MM-DD)")
	fs.StringVar(&ff.updatedAfter, flagUpdatedAfter, "", "filter to Beads updated after this time (RFC3339 or YYYY-MM-DD)")
}

type resolvedFilter struct {
	parentID      string
	parentDisplay string
	updatedBefore string
	updatedAfter  string
}

func resolveFilterFlags(ctx context.Context, q domain.Querier, ff filterFlags, n domain.ShortIDs, stderr io.Writer) (resolvedFilter, bool) {
	var rf resolvedFilter
	if ff.parent != "" {
		id, display, err := domain.ResolveParentFilter(ctx, q, ff.parent, n)
		if err != nil {
			output.WriteError(stderr, err.Error())
			return resolvedFilter{}, false
		}
		rf.parentID, rf.parentDisplay = id, display
	}
	if ff.updatedBefore != "" {
		t, err := domain.ParseFilterTime(ff.updatedBefore)
		if err != nil {
			output.WriteError(stderr, err.Error())
			return resolvedFilter{}, false
		}
		rf.updatedBefore = t
	}
	if ff.updatedAfter != "" {
		t, err := domain.ParseFilterTime(ff.updatedAfter)
		if err != nil {
			output.WriteError(stderr, err.Error())
			return resolvedFilter{}, false
		}
		rf.updatedAfter = t
	}
	return rf, true
}

func namespaceWasSet(fs *flag.FlagSet) bool {
	namespaceSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == flagNamespace {
			namespaceSet = true
		}
	})
	return namespaceSet
}

func namespaceConds(namespace string) []output.Cond {
	var conds []output.Cond
	if namespace != "" {
		conds = append(conds, output.Cond{Key: flagNamespace, Value: namespace})
	}
	return conds
}

func resolveDirAndLimit(env storage.Env, fs *flag.FlagSet, limit *int) (string, *config.Config, error) {
	dir, cfg, err := resolveDirAndConfig(env)
	if err != nil {
		return "", nil, err
	}
	limitSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == flagLimit {
			limitSet = true
		}
	})
	if !limitSet {
		*limit = cfg.ListLimitOr(*limit)
	}
	return dir, cfg, nil
}

func resolverFor(ctx context.Context, q domain.Querier, beads []domain.Bead, extraIDs ...string) (*domain.AliasResolver, error) {
	ids := make([]string, len(beads), len(beads)+len(extraIDs))
	for i, b := range beads {
		ids[i] = b.ID
	}
	ids = append(ids, extraIDs...)
	return domain.NewAliasResolverFor(ctx, q, ids)
}

func writeFilterHeader(stdout io.Writer, conds []output.Cond, rf resolvedFilter, ff filterFlags) {
	conds = filterConds(conds, rf, ff)
	output.WriteFilter(stdout, conds)
}

func writeBeadListFooter(stdout io.Writer, rows []output.BeadRow, total int) {
	output.WriteTruncated(stdout, len(rows), total)
	output.WriteBeadList(stdout, rows)
}

func filterConds(conds []output.Cond, rf resolvedFilter, ff filterFlags) []output.Cond {
	if rf.parentDisplay != "" {
		conds = append(conds, output.Cond{Key: flagParent, Value: rf.parentDisplay})
	}
	if rf.updatedBefore != "" {
		conds = append(conds, output.Cond{Key: "updated_before", Value: ff.updatedBefore})
	}
	if rf.updatedAfter != "" {
		conds = append(conds, output.Cond{Key: "updated_after", Value: ff.updatedAfter})
	}
	return conds
}
