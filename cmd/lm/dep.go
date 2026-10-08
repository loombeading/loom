// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func runDep(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runGroupDispatch("dep", args, stdin, stdout, stderr, []groupSub{
		{cmdAdd, runDepAdd},
		{"remove", runDepRemove},
	}, nil, "lm dep requires a subcommand (add, remove)")
}

func linkPhrase(depType string) string {
	switch depType {
	case domain.BlocksDepType:
		return "is blocked by"
	case domain.ParentChildDepType:
		return "is child of"
	case domain.DiscoveredFromDepType:
		return "is discovered from"
	case domain.DuplicatesDepType:
		return "duplicates"
	default:
		return depType
	}
}

type depFlags struct {
	depType string
	reason  string
	actor   string

	allowPriorityChange bool
	dryRun              bool
}

func parseDepFlags(name string, args []string, stderr io.Writer) (*flag.FlagSet, depFlags, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	var f depFlags
	fs.StringVar(&f.depType, flagType, domain.BlocksDepType, "link type (blocks|parent-child|discovered-from|duplicates)")
	fs.StringVar(&f.reason, flagReason, "", `reason to record, or "-" to read from stdin`)
	fs.StringVar(&f.actor, flagActor, "", actorFlagUsage)
	fs.BoolVar(&f.allowPriorityChange, "allow-priority-change", false, "apply a link that raises the effective priority of a Bead (ignored by remove)")
	fs.BoolVar(&f.dryRun, "dry-run", false, "print the effective priority changes without writing anything")
	err := fs.Parse(reorderArgs(fs, args))
	return fs, f, err
}

func warnDiscoveredFromFanout(db *storage.DB, stderr io.Writer, beadID, beadNamespace, dependsOnID, dependsNamespace, depType string, shortLen domain.ShortIDs) {
	if depType != domain.DiscoveredFromDepType {
		return
	}
	n, err := domain.CountActiveLinks(context.Background(), db.SQL, dependsOnID, depType)
	if err != nil || n != domain.DiscoveredFromFanoutWarnThreshold+1 {
		return
	}
	fmt.Fprintf(stderr, "警告: %s の discovered-from 派生数が閾値(%d)を超えた（現在%d件）。仕様の詰め直しか根本原因の起票を検討する\n",
		domain.DisplayID(dependsNamespace, dependsOnID, shortLen), domain.DiscoveredFromFanoutWarnThreshold, n)
}

func runDepChange(sub, verb string, op func(ctx context.Context, tx *sql.Tx, actor, beadID, dependsOnID, depType, occurredAt, reason string) error, afterOp func(db *storage.DB, stderr io.Writer, beadID, beadNamespace, dependsOnID, dependsNamespace, depType string, shortLen domain.ShortIDs), args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs, f, err := parseDepFlags("dep "+sub, args, stderr)
	if err != nil {
		return helpExitCode(err)
	}
	depType, reason, actorFlag := f.depType, f.reason, f.actor
	if fs.NArg() != 2 {
		output.WriteError(stderr, fmt.Sprintf("lm dep %s requires <ID> <depends-on-ID>", sub))
		fmt.Fprintf(stderr, "See: lm help dep %s\n", sub)
		return 1
	}
	if depType == "related" {
		output.WriteError(stderr, "--type related was removed; use blocks for ordering, discovered-from for derived work, or mention the ID in the body/--reason for anything else")
		return 1
	}
	if !domain.ValidDepTypes[depType] {
		output.WriteError(stderr, fmt.Sprintf("invalid --type %q", depType))
		return 1
	}

	reasonVal, err := readBodyFlag(reason, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	db, cfg, err := openDB(os.Getenv, actorFlag)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = db.Close() }()

	ctx := domain.WithStaleIDNotice(context.Background(), stderr)
	now := storage.NowRFC3339Milli()
	actorVal := actor(actorFlag, os.Getenv)
	n, err := domain.LoadShortIDs(ctx, db.SQL)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	beadRaw, dependsRaw := fs.Arg(0), fs.Arg(1)
	var beadID, dependsOnID, beadNamespace, dependsNamespace string
	var changes []string
	var raises bool
	var blocked bytes.Buffer
	err = db.WithWrite(ctx, func(tx *sql.Tx) error {
		var rerr error
		beadID, rerr = domain.ResolveID(ctx, tx, beadRaw)
		if rerr != nil {
			return rerr
		}
		dependsOnID, rerr = domain.ResolveID(ctx, tx, dependsRaw)
		if rerr != nil {
			return rerr
		}
		beadNamespace, rerr = namespaceOf(ctx, tx, beadID)
		if rerr != nil {
			return rerr
		}
		dependsNamespace, rerr = namespaceOf(ctx, tx, dependsOnID)
		if rerr != nil {
			return rerr
		}
		track := depType == domain.BlocksDepType || depType == domain.ParentChildDepType
		var before map[string]domain.EffectivePriority
		if track {
			before, rerr = domain.RaisedPriorities(ctx, tx, effNow())
		}
		apply := func() error { return op(ctx, tx, actorVal, beadID, dependsOnID, depType, now, reasonVal) }
		var dropped map[string]bool
		if rerr == nil && track && sub == cmdAdd {
			dropped, rerr = droppedFromReady(ctx, tx, apply)
		} else if rerr == nil {
			rerr = apply()
		}
		if rerr == nil && track {
			changes, raises, rerr = priorityChanges(ctx, tx, before, n)
		}
		if rerr == nil && len(dropped) > 0 {
			rerr = writeNewlyBlocked(ctx, tx, &blocked, n, dropped, cfg.NamespaceDefault)
		}
		if rerr != nil {
			return rerr
		}
		if f.dryRun {
			return errDepDryRun
		}
		if raises && sub == cmdAdd && !f.allowPriorityChange {
			return errDepRaises
		}
		return nil
	})
	if errors.Is(err, errDepDryRun) {
		fmt.Fprintln(stdout, "Dry run: nothing written")
		writePriorityChanges(stdout, changes)
		_, _ = blocked.WriteTo(stdout)
		return 0
	}
	if errors.Is(err, errDepRaises) {
		output.WriteError(stderr, "link would raise effective priority; rerun with --allow-priority-change to apply")
		writePriorityChanges(stderr, changes)
		return 1
	}
	if err != nil {
		if reportIfBusy(stderr, db.BusyTimeoutMS, err) {
			return 1
		}
		output.WriteError(stderr, errorWithHint(err))
		return 1
	}

	if afterOp != nil {
		afterOp(db, stderr, beadID, beadNamespace, dependsOnID, dependsNamespace, depType, n)
	}

	fmt.Fprintf(stdout, "- %s: %s %s %s (%s)\n",
		verb, domain.DisplayID(beadNamespace, beadID, n), linkPhrase(depType), domain.DisplayID(dependsNamespace, dependsOnID, n), depType)
	writePriorityChanges(stdout, changes)
	_, _ = blocked.WriteTo(stdout)
	return 0
}

var (
	errDepDryRun = errors.New("dep dry run")
	errDepRaises = errors.New("dep raises effective priority")
)

func priorityChanges(ctx context.Context, tx *sql.Tx, before map[string]domain.EffectivePriority, n domain.ShortIDs) ([]string, bool, error) {
	after, err := domain.RaisedPriorities(ctx, tx, effNow())
	type change struct {
		bead     domain.Bead
		old, new int
	}
	ids := slices.Collect(maps.Keys(after))
	for id := range before {
		if _, ok := after[id]; !ok {
			ids = append(ids, id)
		}
	}
	var cs []change
	raises := false
	for _, id := range ids {
		b, gerr := domain.GetBead(ctx, tx, id)
		err = errors.Join(err, gerr)
		c := change{bead: b, old: b.Priority, new: b.Priority}
		if e, ok := before[id]; ok {
			c.old = e.Priority
		}
		if e, ok := after[id]; ok {
			c.new = e.Priority
		}
		if c.old == c.new {
			continue
		}
		raises = raises || c.new < c.old
		cs = append(cs, c)
	}
	err = errors.Join(err, domain.FillSources(ctx, tx, effNow(), after, ids))
	slices.SortFunc(cs, func(a, b change) int {
		return cmp.Or(cmp.Compare(a.new, b.new), cmp.Compare(a.bead.CreatedAt, b.bead.CreatedAt), cmp.Compare(a.bead.ID, b.bead.ID))
	})
	shown := make([]string, 0, len(cs)*2)
	for _, c := range cs {
		shown = append(shown, c.bead.ID, after[c.bead.ID].Source)
	}
	shown = slices.DeleteFunc(shown, func(id string) bool { return id == "" })
	resolver, rerr := domain.NewAliasResolverFor(ctx, tx, shown)
	err = errors.Join(err, rerr)
	if err != nil {
		return nil, false, err
	}
	lines := make([]string, 0, len(cs))
	for _, c := range cs {
		disp, derr := sourceDisplayID(ctx, tx, resolver, c.bead.ID, n)
		err = errors.Join(err, derr)
		line := fmt.Sprintf("- %s P%d→P%d", disp, c.old, c.new)
		if src := after[c.bead.ID].Source; src != "" {
			sd, derr := sourceDisplayID(ctx, tx, resolver, src, n)
			err = errors.Join(err, derr)
			line += " (via " + sd + ")"
		}
		lines = append(lines, line)
	}
	return lines, raises, err
}

func writePriorityChanges(w io.Writer, lines []string) {
	fmt.Fprintln(w, "\n## Priority changes")
	if len(lines) == 0 {
		fmt.Fprintln(w, "(none)")
		return
	}
	fmt.Fprintln(w, strings.Join(lines, "\n"))
}

func runDepAdd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runDepChange(cmdAdd, "Linked", domain.UpsertLink, warnDiscoveredFromFanout, args, stdin, stdout, stderr)
}

func runDepRemove(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runDepChange("remove", "Unlinked", domain.RemoveLink, nil, args, stdin, stdout, stderr)
}
