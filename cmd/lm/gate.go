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
	"strings"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/estimate"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
	"github.com/loombeading/loom/internal/waitlabel"
)

func runGate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runGroupDispatch(cmdGate, args, stdin, stdout, stderr, []groupSub{
		{"create", runGateCreate},
		{"resolve", runGateResolve},
		{"reject", runGateReject},
		{"sweep", withStdin(runGateSweep)},
	}, runGateBare, "lm gate requires a subcommand (create, resolve, reject, sweep)")
}

func runGateCreate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gate create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	subjectFlag := fs.String("subject", "", "wait subject, one line (required)")
	currentFlag := fs.String("current", "", "current situation")
	proposalFlag := fs.String("proposal", "", "proposed action")
	checkFlag := fs.String("check", "", "what happens once the wait is resolved")
	resolverFlag := fs.String("resolver", "", "who runs lm gate resolve (required for --kind external)")
	reason := fs.String(flagReason, "", `reason to record, or "-" to read from stdin`)
	namespaceFlag := fs.String(flagNamespace, "", "namespace to file the new Gate under")
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	kindFlag := fs.String("kind", "", "Gate kind: human|external|adjudicate|confirm (required)")
	adjudicatedBy := fs.String("adjudicated-by", "", "closed kind:adjudicate Gate that concluded a person must act (required for --kind human|confirm)")
	var blocks stringSlice
	fs.Var(&blocks, "blocks", "Bead ID this Gate blocks (repeatable; required, at least one)")
	var materials stringSlice
	fs.Var(&materials, "material", "decision material the Gate waits for, held as an external ref by the Bead that produces it (repeatable)")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}

	if len(blocks) == 0 {
		output.WriteError(stderr, "lm gate create requires at least one --blocks ID")
		return 1
	}
	if *subjectFlag == "" {
		output.WriteError(stderr, "--subject is required")
		return 1
	}
	if *kindFlag == "external" && *resolverFlag == "" {
		output.WriteError(stderr, "--resolver is required for --kind external")
		return 1
	}
	if *kindFlag == "" {
		output.WriteError(stderr, "--kind is required, must be one of: human, external, adjudicate, confirm")
		return 1
	}
	var labels []string
	switch *kindFlag {
	case "adjudicate":
		labels = []string{"kind:adjudicate"}
	case "human":
		labels = []string{"kind:human"}
	case "confirm":
		labels = []string{"kind:confirm"}
	case "external":
		labels = []string{"kind:external"}
	default:
		output.WriteError(stderr, fmt.Sprintf("invalid --kind %q, must be one of: human, external, adjudicate, confirm", *kindFlag))
		return 1
	}
	if *adjudicatedBy != "" {
		labels = append(labels, domain.AdjudicatedByPrefix+*adjudicatedBy)
	}
	for _, m := range materials {
		labels = append(labels, domain.MaterialPrefix+m)
	}

	reasonVal, err := readBodyFlag(*reason, stdin)
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

	ctx := domain.WithStaleIDNotice(context.Background(), stderr)
	now := storage.NowRFC3339Milli()
	actorVal := actor(*actorFlag, os.Getenv)

	var newID string
	err = db.WithWrite(ctx, func(tx *sql.Tx) error {
		id, err := domain.GateCreate(ctx, tx, domain.GateCreateInput{
			Blocks: blocks,
			Fields: domain.GateFields{
				Subject:  *subjectFlag,
				Current:  *currentFlag,
				Proposal: *proposalFlag,
				Check:    *checkFlag,
				Resolver: *resolverFlag,
			},
			Namespace: namespace,
			Labels:    labels,
			Actor:     actorVal,
			Reason:    reasonVal,
			Now:       now,
		})
		if err != nil {
			return err
		}
		newID = id
		return nil
	})
	if err != nil {
		if reportIfBusy(stderr, db.BusyTimeoutMS, err) || reportIfGateTerminal(stderr, "new", err) {
			return 1
		}
		output.WriteError(stderr, err.Error())
		return 1
	}

	n, err := domain.LoadShortIDs(ctx, db.SQL)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	blocking, err := domain.GateBlocking(ctx, db.SQL, newID)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	blockingIDs := make([]string, len(blocking))
	for i, bl := range blocking {
		blockingIDs[i] = bl.ID
	}
	resolver, err := domain.NewAliasResolverFor(ctx, db.SQL, blockingIDs)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	ids, err := blockingDisplayIDs(resolver, blocking, n)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	line := fmt.Sprintf("- Created: %s [gate/%s/P%d] gate: %s", domain.DisplayID(namespace, newID, n), domain.StatusOpen, domain.DefaultPriority, output.Escape(*subjectFlag))
	if len(ids) > 0 {
		line += "  → blocks: " + strings.Join(ids, ", ")
	}
	fmt.Fprintln(stdout, line)
	return 0
}

func runGateChange(sub, verb string, apply func(ctx context.Context, tx *sql.Tx, id, reason, actor, now string) ([]string, error), args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gate "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	reason := fs.String(flagReason, "", `reason to record (required), or "-" to read from stdin`)
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() == 0 {
		output.WriteError(stderr, fmt.Sprintf("lm gate %s requires at least one ID", sub))
		fmt.Fprintf(stderr, "See: lm help gate %s\n", sub)
		return 1
	}

	reasonVal, err := readBodyFlag(*reason, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	if reasonVal == "" {
		output.WriteError(stderr, "--reason is required for lm gate "+sub)
		return 1
	}

	db, cfg, err := openDB(os.Getenv, *actorFlag)
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

	exit := 0
	var cancelledIDs []string
	newlyReadyIDs := map[string]bool{}
	for _, raw := range fs.Args() {
		id, err := domain.ResolveID(ctx, db.SQL, raw)
		if err != nil {
			output.WriteRejected(stdout, raw, err.Error())
			exit = 1
			continue
		}

		var namespace string
		var cancelled []string
		var newly []string
		err = db.WithWrite(ctx, func(tx *sql.Tx) error {
			var perr error
			namespace, perr = namespaceOf(ctx, tx, id)
			if perr != nil {
				return perr
			}
			var derr error
			newly, derr = domain.NewlyReadyDiff(ctx, tx, func() error {
				var cerr error
				cancelled, cerr = apply(ctx, tx, id, reasonVal, actorVal, now)
				return cerr
			})
			return derr
		})
		if err != nil {
			if reportIfBusy(stderr, db.BusyTimeoutMS, err) {
				return 1
			}
			if reportIfReadOnly(stderr, raw, err) {
				exit = 1
				continue
			}
			writeRejected(stdout, stderr, raw, err)
			exit = 1
			continue
		}
		fmt.Fprintf(stdout, "- %s: %s\n", verb, domain.DisplayID(namespace, id, n))
		cancelledIDs = append(cancelledIDs, cancelled...)
		for _, nid := range newly {
			newlyReadyIDs[nid] = true
		}
	}

	if sub == "reject" {
		cancelledRows, err := beadRowsForIDs(ctx, db.SQL, n, cancelledIDs)
		if err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
		output.WriteCancelled(stdout, cancelledRows)
	}

	if err := writeNewlyReady(ctx, db.SQL, stdout, n, newlyReadyIDs, cfg.NamespaceDefault); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	return exit
}

func runGateResolve(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	apply := func(ctx context.Context, tx *sql.Tx, id, reason, actor, now string) ([]string, error) {
		err := domain.GateResolve(ctx, tx, domain.GateResolveInput{
			ID:     id,
			Reason: reason,
			Actor:  actor,
			Now:    now,
		})
		return nil, err
	}
	return runGateChange("resolve", "Resolved", apply, args, stdin, stdout, stderr)
}

func runGateReject(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	apply := func(ctx context.Context, tx *sql.Tx, id, reason, actor, now string) ([]string, error) {
		return domain.GateReject(ctx, tx, domain.GateRejectInput{
			ID:     id,
			Reason: reason,
			Actor:  actor,
			Now:    now,
		})
	}
	return runGateChange("reject", "Rejected", apply, args, stdin, stdout, stderr)
}

func runGateSweep(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gate sweep", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() != 0 {
		output.WriteError(stderr, "lm gate sweep takes no positional arguments")
		return 1
	}

	db, cfg, err := openDB(os.Getenv, *actorFlag)
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

	var newly []string
	var resolved []idAndNamespace
	err = db.WithWrite(ctx, func(tx *sql.Tx) error {
		var derr error
		newly, derr = domain.NewlyReadyDiff(ctx, tx, func() error {
			result, serr := domain.GateSweep(ctx, tx, actorVal, now)
			if serr != nil {
				return serr
			}
			for _, gateID := range result.Resolved {
				namespace, nerr := namespaceOf(ctx, tx, gateID)
				if nerr != nil {
					return nerr
				}
				resolved = append(resolved, idAndNamespace{ID: gateID, Namespace: namespace})
			}
			return nil
		})
		return derr
	})
	if err != nil {
		if reportIfBusy(stderr, db.BusyTimeoutMS, err) {
			return 1
		}
		output.WriteError(stderr, err.Error())
		return 1
	}

	if len(resolved) == 0 {
		return 0
	}

	for _, g := range resolved {
		fmt.Fprintf(stdout, "- Auto-resolved: %s\n", domain.DisplayID(g.Namespace, g.ID, n))
	}

	newlyReadyIDs := make(map[string]bool, len(newly))
	for _, nid := range newly {
		newlyReadyIDs[nid] = true
	}
	if err := writeNewlyReady(ctx, db.SQL, stdout, n, newlyReadyIDs, cfg.NamespaceDefault); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	return 0
}

type gateSection struct {
	heading string
	entries []domain.GateListEntry
	aux     func(e domain.GateListEntry, gateID string) ([]string, error)
}

func runGateBare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(cmdGate, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	_ = fs.Bool(flagAll, false, "deprecated: every kind is always shown")
	namespaceFlag := fs.String(flagNamespace, "", "filter by namespace")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	namespaceSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == flagNamespace {
			namespaceSet = true
		}
	})
	db, cfg, err := openDB(os.Getenv, "")
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = db.Close() }()

	namespace, err := domain.ResolveNamespaceForFilter(*namespaceFlag, namespaceSet, os.Getenv, cfg.NamespaceDefault)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	if err := writeGateList(context.Background(), db.SQL, namespace, stdout); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	return 0
}

func writeGateList(ctx context.Context, q domain.Querier, namespace string, stdout io.Writer) error {
	n, err := domain.LoadShortIDs(ctx, q)
	if err != nil {
		return err
	}
	entries, eff, err := domain.GateListEffective(ctx, q, effNow())
	if err != nil {
		return err
	}
	entries = filterGatesByNamespace(entries, namespace)
	output.WriteFilter(stdout, namespaceConds(namespace))
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "(none)")
		return nil
	}

	bySec := partitionGates(entries)
	viaByGate, graph, err := gateViaWaits(ctx, q)
	if err != nil {
		return err
	}
	progressByGate, err := gateWaitProgresses(ctx, q, bySec[gateSecExternal])
	if err != nil {
		return err
	}
	resolver, err := domain.NewAliasResolverFor(ctx, q, gateBlockingIDs(entries, viaByGate, progressByGate))
	if err != nil {
		return err
	}
	gateIDs := make([]string, len(entries))
	for i, e := range entries {
		gateIDs[i] = e.ID
	}
	effSources, err := effectiveSources(ctx, q, eff, gateIDs, n)
	if err != nil {
		return err
	}

	sections := []gateSection{
		{"## Needs you", bySec[gateSecHuman], gateHumanAux},
		{"## Blocked by other work", bySec[gateSecPrereq], gatePrereqAux(n)},
		{"## Waiting on judge", bySec[gateSecAdjudicate], gateHumanAux},
		{"## Opens automatically", bySec[gateSecExternal], gateAutoOpenAux(resolver, n, progressByGate)},
		{"## Unknown kind (fix labels)", bySec[gateSecUnknown], gateUnknownAux},
	}
	line := func(e domain.GateListEntry) (string, error) {
		return gateLine(e, resolver, graph, viaByGate[e.ID], eff, effSources, n)
	}
	return writeGateSections(stdout, sections, n, line)
}

func filterGatesByNamespace(entries []domain.GateListEntry, namespace string) []domain.GateListEntry {
	if namespace == "" {
		return entries
	}
	kept := entries[:0]
	for _, e := range entries {
		if e.Namespace == namespace {
			kept = append(kept, e)
		}
	}
	return kept
}

func partitionGates(entries []domain.GateListEntry) map[string][]domain.GateListEntry {
	bySec := make(map[string][]domain.GateListEntry)
	for _, e := range entries {
		sec := gateSectionOf(e)
		bySec[sec] = append(bySec[sec], e)
	}
	return bySec
}

func gateWaitProgresses(ctx context.Context, q domain.Querier, gates []domain.GateListEntry) (map[string][]domain.GateBlockerWait, error) {
	progressByGate := make(map[string][]domain.GateBlockerWait, len(gates))
	for _, e := range gates {
		progress, err := domain.GateWaitProgress(ctx, q, e.ID)
		if err != nil {
			return nil, err
		}
		progressByGate[e.ID] = progress
	}
	return progressByGate, nil
}

func gateBlockingIDs(entries []domain.GateListEntry, viaByGate map[string][]gateVia, progressByGate map[string][]domain.GateBlockerWait) []string {
	var ids []string
	for _, e := range entries {
		for _, bl := range e.Blocking {
			ids = append(ids, bl.ID)
		}
		for _, v := range viaByGate[e.ID] {
			ids = append(ids, v.via, v.beadID)
		}
		for _, b := range progressByGate[e.ID] {
			ids = append(ids, b.ID)
			for _, c := range b.Children {
				ids = append(ids, c.ID)
			}
		}
	}
	return ids
}

func writeGateSections(stdout io.Writer, sections []gateSection, n domain.ShortIDs, line func(domain.GateListEntry) (string, error)) error {
	printed := false
	for _, sec := range sections {
		if len(sec.entries) == 0 {
			continue
		}
		if printed {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintln(stdout, sec.heading)
		for _, e := range sec.entries {
			if err := writeGateEntry(stdout, sec, e, n, line); err != nil {
				return err
			}
		}
		printed = true
	}
	return nil
}

func writeGateEntry(stdout io.Writer, sec gateSection, e domain.GateListEntry, n domain.ShortIDs, line func(domain.GateListEntry) (string, error)) error {
	l, err := line(e)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, l)
	auxLines, err := sec.aux(e, domain.DisplayID(e.Namespace, e.ID, n))
	if err != nil {
		return err
	}
	for _, al := range auxLines {
		fmt.Fprintln(stdout, al)
	}
	return nil
}

const (
	gateSecHuman      = "human"
	gateSecPrereq     = "prereq"
	gateSecAdjudicate = "adjudicate"
	gateSecExternal   = "external"
	gateSecUnknown    = "unknown"
)

func gateSectionOf(e domain.GateListEntry) string {
	kind := domain.GateKindOf(e.Labels, e.Fields.Subject)
	if kind == domain.GateKindHuman && len(unknownWaitLabels(e.Labels)) > 0 {
		return gateSecUnknown
	}
	if len(gateProbeLabels(e.Labels, kind)) > 0 {
		return gateSecExternal
	}
	switch kind {
	case domain.GateKindHuman:
		if len(e.PrereqBlockers) > 0 {
			return gateSecPrereq
		}
		return gateSecHuman
	case domain.GateKindAdjudicate:
		return gateSecAdjudicate
	case domain.GateKindExternal:
		return gateSecExternal
	default:
		return gateSecUnknown
	}
}

func gateProbeLabels(labels []string, kind string) []string {
	var probes []string
	for _, l := range labels {
		if !waitlabel.IsProbe(l) {
			continue
		}
		if (kind == domain.GateKindAdjudicate || kind == domain.GateKindHuman) && waitlabel.WaitsOnPerson(l) {
			continue
		}
		if kind == domain.GateKindHuman && strings.HasPrefix(l, waitlabel.File.Prefix()) {
			continue
		}
		probes = append(probes, l)
	}
	return probes
}

func gateLine(e domain.GateListEntry, resolver *domain.AliasResolver, graph map[string]domain.EstimateBead, vias []gateVia, eff map[string]domain.EffectivePriority, effSources map[string]string, n domain.ShortIDs) (string, error) {
	gateID := domain.DisplayID(e.Namespace, e.ID, n)
	ids, err := blockingDisplayIDs(resolver, e.Blocking, n)
	if err != nil {
		return "", err
	}
	line := fmt.Sprintf("- %s [%s/%s/P%d] %s", gateID, domain.BeadTypeGate, domain.StatusOpen, e.Priority, output.Escape(strings.TrimPrefix(e.Title, "gate: ")))
	if len(ids) > 0 {
		line += "  → blocks: " + strings.Join(ids, ", ")
	}
	viaText, err := gateViaText(resolver, graph, vias, n)
	if err != nil {
		return "", err
	}
	if src, ok := effSources[e.ID]; ok {
		viaText += fmt.Sprintf("  eff=P%d(%s)", eff[e.ID].Priority, src)
	}
	return line + viaText, nil
}

type gateVia struct {
	beadID, via string
}

func gateViaWaits(ctx context.Context, q domain.Querier) (map[string][]gateVia, map[string]domain.EstimateBead, error) {
	graph, order, domainDeps, _, readyIDs, err := domain.EstimateInput(ctx, q, domain.ReadyFilter{Priority: -1})
	if err != nil {
		return nil, nil, err
	}
	beads, deps := toEstimateGraph(graph, domainDeps)
	_, gateWaiting, _ := estimate.ComputeCourses(beads, order, deps, readyIDs)
	return gateViasFrom(gateWaiting), graph, nil
}

func gateViasFrom(gateWaiting []estimate.GateWait) map[string][]gateVia {
	viaByGate := map[string][]gateVia{}
	for _, gw := range gateWaiting {
		for _, gid := range gw.GateIDs {
			if via := gw.Via[gid]; via != gw.BeadID {
				viaByGate[gid] = append(viaByGate[gid], gateVia{beadID: gw.BeadID, via: via})
			}
		}
	}
	return viaByGate
}

func gateViaText(resolver *domain.AliasResolver, graph map[string]domain.EstimateBead, vias []gateVia, n domain.ShortIDs) (string, error) {
	var viaOrder []string
	groups := map[string][]domain.Blocker{}
	for _, v := range vias {
		if _, ok := groups[v.via]; !ok {
			viaOrder = append(viaOrder, v.via)
			groups[v.via] = []domain.Blocker{{ID: v.via, Namespace: graph[v.via].Namespace}}
		}
		groups[v.via] = append(groups[v.via], domain.Blocker{ID: v.beadID, Namespace: graph[v.beadID].Namespace})
	}
	var b strings.Builder
	for _, via := range viaOrder {
		ids, err := blockingDisplayIDs(resolver, groups[via], n)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "  via %s: %s", ids[0], strings.Join(ids[1:], ", "))
	}
	return b.String(), nil
}

func gateHumanAux(e domain.GateListEntry, gateID string) ([]string, error) {
	check := e.Fields.Check
	if check == "" {
		check = "(--check に記載なし)"
	}
	return []string{
		"  - When resolved: " + output.Escape(check),
		fmt.Sprintf(`  - Run: lm gate resolve %s --reason "go"`, gateID),
	}, nil
}

func gatePrereqAux(n domain.ShortIDs) func(domain.GateListEntry, string) ([]string, error) {
	return func(e domain.GateListEntry, gateID string) ([]string, error) {
		check := e.Fields.Check
		if check == "" {
			check = "(--check に記載なし)"
		}
		ids := make([]string, len(e.PrereqBlockers))
		for i, bl := range e.PrereqBlockers {
			ids[i] = domain.DisplayID(bl.Namespace, bl.ID, n)
		}
		return []string{
			"  - When resolved: " + output.Escape(check),
			"  - Blocked by: " + strings.Join(ids, ", "),
		}, nil
	}
}

func gateAutoOpenAux(resolver *domain.AliasResolver, n domain.ShortIDs, progressByGate map[string][]domain.GateBlockerWait) func(domain.GateListEntry, string) ([]string, error) {
	return func(e domain.GateListEntry, gateID string) ([]string, error) {
		var lines []string
		for _, b := range progressByGate[e.ID] {
			id, err := aliasOrDisplayID(resolver, b.Namespace, b.ID, n)
			if err != nil {
				return nil, err
			}
			line := fmt.Sprintf("  - Waiting for: %s %s", id, b.Status)
			if len(b.Children) > 0 {
				done := 0
				var remaining []string
				for _, c := range b.Children {
					if domain.IsTerminalStatus(c.Status) {
						done++
						continue
					}
					cid, err := aliasOrDisplayID(resolver, c.Namespace, c.ID, n)
					if err != nil {
						return nil, err
					}
					remaining = append(remaining, cid+" "+c.Status)
				}
				suffix := fmt.Sprintf("（子 %d/%d 終端", done, len(b.Children))
				if len(remaining) > 0 {
					suffix += "・残り " + strings.Join(remaining, ", ")
				}
				line += suffix + "）"
			}
			lines = append(lines, line)
		}
		for _, l := range gateProbeLabels(e.Labels, domain.GateKindOf(e.Labels, e.Fields.Subject)) {
			lines = append(lines, "  - Waiting for: "+output.Escape(l))
		}
		return lines, nil
	}
}

func unknownWaitLabels(labels []string) []string {
	var unknown []string
	for _, l := range labels {
		if waitlabel.IsUnknown(l) {
			unknown = append(unknown, l)
		}
	}
	return unknown
}

func gateUnknownAux(e domain.GateListEntry, gateID string) ([]string, error) {
	if domain.GateKindOf(e.Labels, e.Fields.Subject) == domain.GateKindHuman {
		if unknown := unknownWaitLabels(e.Labels); len(unknown) > 0 {
			return []string{"  - Fix: unknown wait label: " + output.Escape(strings.Join(unknown, ", "))}, nil
		}
	}
	return []string{fmt.Sprintf("  - Fix: lm update %s --add-label kind:adjudicate|kind:external", gateID)}, nil
}

func blockingDisplayIDs(resolver *domain.AliasResolver, blocking []domain.Blocker, n domain.ShortIDs) ([]string, error) {
	ids := make([]string, len(blocking))
	for i, bl := range blocking {
		id, err := aliasOrDisplayID(resolver, bl.Namespace, bl.ID, n)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func aliasOrDisplayID(resolver *domain.AliasResolver, namespace, id string, n domain.ShortIDs) (string, error) {
	alias, hasParent, err := resolver.Alias(id)
	if err != nil {
		return "", err
	}
	if hasParent {
		return alias, nil
	}
	return domain.DisplayID(namespace, id, n), nil
}
