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
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func runUpdate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	claim := fs.Bool("claim", false, "claim: open -> in_progress")
	release := fs.Bool("release", false, "release: in_progress -> open")
	force := fs.Bool("force", false, "with --release, release a claim held by another actor; with --claim and --reason, claim past unfinished blocks dependencies")
	reopen := fs.Bool("reopen", false, "reopen: closed|cancelled -> open")
	status := fs.String(flagStatus, "", `set status; only "cancelled" is accepted`)
	title := fs.String("title", "", "new title")
	description := fs.String("description", "", `new description, or "-" to read from stdin`)
	summary := fs.String(flagSummary, "", `new summary, or "-" to read from stdin`)
	priority := fs.Int(flagPriority, -1, "new priority (1-4); -1 means unspecified, leaving priority unchanged")
	fs.IntVar(priority, "p", -1, "shorthand for --priority")
	beadType := fs.String(flagType, "", "new type")
	namespaceFlag := fs.String(flagNamespace, "", "new namespace")
	depth := fs.Int("reasoning-depth", -1, depthUsage+"; -1 means unspecified, leaving it unchanged")
	severity := fs.Int("severity", -1, "new severity (1-4); -1 means unspecified, leaving severity unchanged")
	due := fs.String("due", "", "due date (RFC 3339)")
	expedite := fs.String("expedite", "", "interrupt span (e.g. 2h, 1d); requires --reason")
	revive := fs.Bool("revive", false, "record revived_at = now when bringing a Bead back by hand; requires --reason")
	reason := fs.String(flagReason, "", `reason to record, or "-" to read from stdin`)
	actorFlag := fs.String(flagActor, "", actorFlagUsage)
	partial := fs.String("partial", "", "title of a follow-up Bead to create for the work this update's --external-ref doesn't cover; requires --external-ref")
	claimTTL := fs.Duration("claim-ttl", 0, "with --claim, claim period after which another actor may take the claim over (e.g. 30m)")
	var addLabels, removeLabels, addExternalRefs, removeExternalRefs, clearArgs stringSlice
	fs.Var(&addLabels, "add-label", "label to add (repeatable)")
	fs.Var(&removeLabels, "remove-label", "label to remove (repeatable)")
	fs.Var(&addExternalRefs, "external-ref", "external reference (e.g. PR URL) to add; not interpreted or validated (repeatable)")
	fs.Var(&removeExternalRefs, "remove-external-ref", "external reference to remove (repeatable)")
	fs.Var(&clearArgs, "clear", "clear a field's value (repeatable); one of description|summary|reasoning_depth|due|expedite")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if status != nil && *status != "" && *status != "cancelled" {
		output.WriteError(stderr, `--status only accepts "cancelled"; use lm close for closed, --claim/--release/--reopen for other transitions`)
		return 1
	}
	if fs.NArg() == 0 {
		output.WriteError(stderr, "lm update requires at least one ID")
		return 1
	}

	if err := rejectMultipleStdin(fs, "description", flagSummary, flagReason); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	desc, err := readBodyFlag(*description, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	summaryVal, err := readBodyFlag(*summary, stdin)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	fieldSet := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { fieldSet[f.Name] = true })

	sched, schedErr := parseScheduleFlags(*severity, fieldSet["severity"], *due, *expedite)
	clearCols, clearErr := parseClearFlags(clearArgs, fieldSet, *depth)
	if schedErr != nil {
		clearErr = schedErr.Error()
	}
	if clearErr == "" {
		clearErr = claimFlagError(fieldSet, *claimTTL, *claim)
	}
	if clearErr == "" {
		clearErr = partialFlagError(fieldSet, *partial, addExternalRefs)
	}
	if clearErr != "" {
		for _, raw := range fs.Args() {
			output.WriteRejected(stdout, raw, clearErr)
		}
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
	now := storage.NowRFC3339Milli()
	clock, err := domain.Now(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	actorVal := actor(*actorFlag, os.Getenv)
	n, err := domain.LoadShortIDs(ctx, db.SQL)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	base := domain.UpdateInput{
		Claim:              *claim,
		Release:            *release,
		Force:              *force,
		Reopen:             *reopen,
		Cancel:             *status == "cancelled",
		Clear:              clearCols,
		Reason:             reasonVal,
		Actor:              actorVal,
		Now:                now,
		Clock:              clock,
		ClaimTTL:           *claimTTL,
		Priority:           -1,
		Severity:           sched.severity,
		Due:                sched.due,
		ExpediteFor:        sched.expediteFor,
		Revive:             *revive,
		AddLabels:          addLabels,
		RemoveLabels:       removeLabels,
		AddExternalRefs:    addExternalRefs,
		RemoveExternalRefs: removeExternalRefs,
	}
	setGivenFields(&base, fieldSet, updateFieldValues{
		title: *title, description: desc, priority: *priority, beadType: *beadType,
		namespace: *namespaceFlag, summary: summaryVal, depth: *depth,
	})
	partialTitle := ""
	if fieldSet["partial"] {
		partialTitle = *partial
	}

	exit := 0
	newlyReadyIDs := map[string]bool{}
	for _, raw := range fs.Args() {
		newly, code, fatal := updateOne(ctx, db, stdout, stderr, raw, base, partialTitle, n)
		if fatal {
			return 1
		}
		if code != 0 {
			exit = code
		}
		for _, nid := range newly {
			newlyReadyIDs[nid] = true
		}
	}

	if base.Cancel {
		if err := writeNewlyReady(ctx, db.SQL, stdout, n, newlyReadyIDs, cfg.NamespaceDefault); err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
	}

	return exit
}

type updateFieldValues struct {
	title, description, beadType, namespace, summary string
	priority, depth                                  int
}

func setGivenFields(in *domain.UpdateInput, fieldSet map[string]bool, v updateFieldValues) {
	if fieldSet["title"] {
		in.Title = v.title
	}
	if fieldSet["description"] {
		in.Description = v.description
	}
	if fieldSet[flagPriority] || fieldSet["p"] {
		in.Priority = v.priority
	}
	if fieldSet[flagType] {
		in.Type = v.beadType
	}
	if fieldSet[flagNamespace] {
		in.Namespace = v.namespace
	}
	if fieldSet[flagSummary] {
		in.Summary = v.summary
	}
	if fieldSet["reasoning-depth"] {
		in.Depth = v.depth
	}
}

func parseClearFlags(clearArgs []string, fieldSet map[string]bool, depth int) ([]string, string) {
	clearFields := map[string]bool{}
	var clearErr string
	for _, c := range clearArgs {
		if c != "description" && c != flagSummary && c != "reasoning_depth" && c != "due" && c != "expedite" {
			clearErr = fmt.Sprintf(`unknown --clear field %q; want one of description, summary, reasoning_depth, due, expedite`, c)
			break
		}
		if flag := strings.ReplaceAll(c, "_", "-"); fieldSet[flag] {
			clearErr = fmt.Sprintf("--clear and --%s both given", flag)
			break
		}
		clearFields[c] = true
	}
	clearCols := make([]string, 0, len(clearFields))
	for col := range clearFields {
		clearCols = append(clearCols, col)
	}
	if clearErr == "" && fieldSet["reasoning-depth"] && depth == 0 {
		clearErr = fmt.Sprintf("reasoning_depth must be between %d and %d", domain.MinReasoningDepth, domain.MaxReasoningDepth)
	}
	return clearCols, clearErr
}

func claimFlagError(fieldSet map[string]bool, claimTTL time.Duration, claim bool) string {
	switch {
	case !fieldSet["claim-ttl"]:
		return ""
	case claimTTL <= 0:
		return "--claim-ttl must be a positive duration"
	case !claim:
		return "--claim-ttl requires --claim"
	}
	return ""
}

func partialFlagError(fieldSet map[string]bool, partial string, refs []string) string {
	switch {
	case !fieldSet["partial"]:
		return ""
	case partial == "":
		return "--partial requires a non-empty value"
	case len(refs) == 0:
		return "--partial requires --external-ref"
	}
	return ""
}

func updateOne(ctx context.Context, db *storage.DB, stdout, stderr io.Writer, raw string, base domain.UpdateInput, partialTitle string, n domain.ShortIDs) ([]string, int, bool) {
	id, err := domain.ResolveID(ctx, db.SQL, raw)
	if err != nil {
		output.WriteRejected(stdout, raw, err.Error())
		return nil, 1, false
	}
	in := base
	in.ID = id

	var namespace, partialID string
	var warnings, newly []string
	doUpdate := func(tx *sql.Tx) error {
		var err error
		warnings, err = domain.UpdateBead(ctx, tx, in)
		if err != nil {
			return err
		}
		if partialTitle == "" {
			return nil
		}
		pid, perr := domain.CreatePartial(ctx, tx, domain.PartialInput{
			SourceID: id,
			Title:    partialTitle,
			Actor:    in.Actor,
			Reason:   in.Reason,
			Now:      in.Now,
		})
		partialID = pid
		return perr
	}
	err = db.WithWrite(ctx, func(tx *sql.Tx) error {
		var perr error
		namespace, perr = namespaceOf(ctx, tx, id)
		if perr != nil {
			return perr
		}
		if !in.Cancel {
			return doUpdate(tx)
		}
		var derr error
		newly, derr = domain.NewlyReadyDiff(ctx, tx, func() error {
			return doUpdate(tx)
		})
		return derr
	})
	if err != nil {
		switch {
		case reportIfBusy(stderr, db.BusyTimeoutMS, err):
			return nil, 1, true
		case reportIfReadOnly(stderr, raw, err), reportIfGateTerminal(stderr, raw, err):
		default:
			writeRejected(stdout, stderr, raw, err)
		}
		return nil, 1, false
	}
	newNamespace := namespace
	if in.Namespace != "" {
		newNamespace = in.Namespace
	}
	if newNamespace != namespace {
		fmt.Fprintf(stdout, "- Updated: %s (was %s)\n", domain.DisplayID(newNamespace, id, n), domain.DisplayID(namespace, id, n))
	} else {
		fmt.Fprintf(stdout, "- Updated: %s\n", domain.DisplayID(newNamespace, id, n))
	}
	for _, w := range warnings {
		fmt.Fprintf(stderr, "warning: %s: %s\n", domain.DisplayID(newNamespace, id, n), w)
	}
	if partialID != "" {
		partialNamespace, nerr := namespaceOf(ctx, db.SQL, partialID)
		if nerr != nil {
			output.WriteError(stderr, nerr.Error())
			return nil, 1, true
		}
		fmt.Fprintf(stdout, "- Created: %s\n", domain.DisplayID(partialNamespace, partialID, n))
	}
	return newly, 0, false
}
