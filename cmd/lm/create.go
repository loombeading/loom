// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

const depthUsage = "reasoning depth (1-5): " +
	"1 = one-shot extraction or routine step; " +
	"2 = verify premises on the real thing, then apply a known procedure; " +
	"3 = choose among options within one topic; " +
	"4 = multi-step reasoning across topics (freeze the spec, then implement); " +
	"5 = autonomous loop with self-correction"

func runCreate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	title := fs.String("title", "", "Bead title (required)")
	description := fs.String("description", "", `Bead description, or "-" to read from stdin`)
	summary := fs.String(flagSummary, "", `Bead summary, or "-" to read from stdin; shown instead of the description once compaction applies`)
	priority := fs.Int(flagPriority, domain.DefaultPriority, "priority (1-4, 1 is highest)")
	fs.IntVar(priority, "p", domain.DefaultPriority, "shorthand for --priority")
	beadType := fs.String(flagType, domain.DefaultBeadType, "task type (task/gate)")
	namespaceFlag := fs.String(flagNamespace, "", "namespace to file the new Bead under")
	parent := fs.String(flagParent, "", "parent Bead ID (parent-child link)")
	discoveredFrom := fs.String("discovered-from", "", "Bead ID this Bead was discovered from (discovered-from link)")
	noMilestone := fs.String("no-milestone", "", "reason this Bead belongs to no milestone (required without --parent or --discovered-from when config create.require_milestone is true)")
	depth := fs.Int("reasoning-depth", 0, depthUsage)
	severity := fs.Int("severity", domain.DefaultSeverity, "severity (1-4): 1 = outage, 2 = degraded, 3 = normal, 4 = minor")
	due := fs.String("due", "", "due date (RFC 3339)")
	expedite := fs.String("expedite", "", "interrupt span (e.g. 2h, 1d); requires --reason")
	redetectKey := fs.String("redetect-key", "", "re-detection key; when an unfinished Bead in the namespace has it, advance its last_redetected_at and print '- Redetected: <ID>' instead of creating")
	reason := fs.String(flagReason, "", `reason to record, or "-" to read from stdin`)
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	var labels, blockedBy, externalRef stringSlice
	fs.Var(&labels, flagLabel, "label to attach (repeatable)")
	fs.Var(&blockedBy, "blocked-by", "Bead ID this Bead is blocked by (repeatable)")
	fs.Var(&externalRef, "external-ref", "external reference (e.g. PR URL); not interpreted or validated (repeatable)")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}

	if *title == "" {
		output.WriteError(stderr, "--title is required")
		return 1
	}
	sched, err := parseScheduleFlags(*severity, true, *due, *expedite)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	if *beadType == domain.BeadTypeGate && !hasKindLabel(labels) {
		output.WriteError(stderr, "lm create --type gate には kind: ラベルが要る（--label kind:human、kind:external、kind:adjudicate、kind:confirm のいずれか）。lm gate create の利用を検討する")
		return 1
	}

	if err := rejectMultipleStdin(fs, "description", flagReason, flagSummary); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	desc, err := readBodyFlag(*description, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	reasonVal, err := readBodyFlag(*reason, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	summaryVal, err := readBodyFlag(*summary, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	dir, cfg, err := resolveDirAndConfig(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	namespace, err := domain.ResolveNamespaceForCreate(*namespaceFlag, os.Getenv, cfg.NamespaceDefaultOr(domain.DefaultNamespace))
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

	now := storage.NowRFC3339Milli()
	clock, err := domain.Now(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	actorVal := actor(*actorFlag, os.Getenv)

	ctx := domain.WithStaleIDNotice(context.Background(), stderr)
	var newID, seenID string
	var blocked bytes.Buffer
	err = db.WithWrite(ctx, func(tx *sql.Tx) error {
		if *redetectKey != "" {
			id, err := domain.RedetectKey(ctx, tx, namespace, *redetectKey, actorVal, reasonVal, now)
			if err != nil || id != "" {
				seenID = id
				return err
			}
		}
		create := func() error {
			id, err := domain.CreateBead(ctx, tx, domain.CreateInput{
				Title:          *title,
				Description:    desc,
				Priority:       *priority,
				BeadType:       *beadType,
				Labels:         labels,
				Namespace:      namespace,
				ExternalRefs:   externalRef,
				Summary:        summaryVal,
				Parent:         *parent,
				DiscoveredFrom: *discoveredFrom,
				BlockedBy:      blockedBy,
				Depth:          *depth,

				Severity:    *severity,
				Due:         sched.due,
				ExpediteFor: sched.expediteFor,
				Clock:       clock,
				RedetectKey: *redetectKey,

				RequireMilestone: cfg.RequireMilestone(),
				ShortLen:         cfg.ShortLenOr(domain.DefaultShortLen),
				NoMilestone:      *noMilestone,

				Actor:  actorVal,
				Reason: reasonVal,
				Now:    now,
			})
			newID = id
			return err
		}
		if *parent == "" && len(blockedBy) == 0 {
			return create()
		}
		dropped, err := droppedFromReady(ctx, tx, create)
		if err != nil || len(dropped) == 0 {
			return err
		}
		n, err := domain.LoadShortIDs(ctx, tx)
		if err != nil {
			return err
		}
		return writeNewlyBlocked(ctx, tx, &blocked, n, dropped, cfg.NamespaceDefault)
	})
	if err != nil {
		if reportIfBusy(stderr, db.BusyTimeoutMS, err) || reportIfGateTerminal(stderr, "new", err) {
			return 1
		}
		output.WriteError(stderr, err.Error())
		return 1
	}

	n, err := domain.LoadShortIDs(context.Background(), db.SQL)
	shownID := newID
	if seenID != "" {
		shownID = seenID
	}
	var resolver *domain.AliasResolver
	if err == nil {
		resolver, err = domain.NewAliasResolverFor(context.Background(), db.SQL, []string{shownID})
	}
	var displayID string
	if err == nil {
		displayID, err = aliasOrDisplayID(resolver, namespace, shownID, n)
	}
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	if seenID != "" {
		_, _ = fmt.Fprintf(stdout, "- Redetected: %s\n", displayID)
		return 0
	}
	output.WriteCreated(stdout, output.BeadRow{
		ID:       displayID,
		Title:    *title,
		Status:   domain.StatusOpen,
		Priority: *priority,
		Type:     *beadType,
	})
	if *beadType != domain.BeadTypeGate {
		writeSimilar(context.Background(), stdout, stderr, db.SQL, namespace, *title, newID, n)
	}
	_, _ = blocked.WriteTo(stdout)
	if *discoveredFrom != "" {
		srcID, _ := domain.ResolveID(context.Background(), db.SQL, *discoveredFrom)
		srcNamespace, _ := namespaceOf(context.Background(), db.SQL, srcID)
		warnDiscoveredFromFanout(db, stderr, newID, namespace, srcID, srcNamespace, domain.DiscoveredFromDepType, n)
	}
	return 0
}

func writeSimilar(ctx context.Context, stdout, stderr io.Writer, q domain.Querier, namespace, title, newID string, n domain.ShortIDs) {
	lines, err := similarLines(ctx, q, namespace, title, newID, n)
	if err != nil {
		output.WriteError(stderr, "similar beads: "+err.Error())
		return
	}
	if len(lines) > 0 {
		_, _ = fmt.Fprintf(stdout, "\n## Similar\n%s\n", strings.Join(lines, "\n"))
	}
}

func similarLines(ctx context.Context, q domain.Querier, namespace, title, newID string, n domain.ShortIDs) ([]string, error) {
	matches, err := domain.SimilarBeads(ctx, q, namespace, title, newID)
	if err != nil {
		return nil, err
	}
	beads := make([]domain.Bead, len(matches))
	ids := make([]string, len(matches))
	for i, m := range matches {
		beads[i] = m.Bead
		ids[i] = m.Bead.ID
	}
	resolver, err := domain.NewAliasResolverFor(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	rows, err := beadRowsUnsorted(beads, n, resolver)
	if err != nil {
		return nil, err
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = fmt.Sprintf("%s  match=%d/%d", output.BeadLine(r), matches[i].K, matches[i].N)
	}
	return lines, nil
}

func hasKindLabel(labels []string) bool {
	for _, l := range labels {
		if strings.HasPrefix(l, "kind:") {
			return true
		}
	}
	return false
}
