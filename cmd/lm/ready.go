// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"flag"
	"io"
	"os"
	"time"

	"github.com/loombeading/loom/internal/config"
	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func writeNewlyReady(ctx context.Context, q domain.Querier, w io.Writer, n domain.ShortIDs, ids map[string]bool, defaultNamespace string) error {
	conds, rows, err := newlyRows(ctx, q, n, ids, defaultNamespace)
	if err != nil {
		return err
	}
	output.WriteNewlyReady(w, conds, rows)
	return nil
}

func writeNewlyBlocked(ctx context.Context, q domain.Querier, w io.Writer, n domain.ShortIDs, ids map[string]bool, defaultNamespace string) error {
	conds, rows, err := newlyRows(ctx, q, n, ids, defaultNamespace)
	if err != nil {
		return err
	}
	output.WriteNewlyBlocked(w, conds, rows)
	return nil
}

func droppedFromReady(ctx context.Context, q domain.Querier, fn func() error) (map[string]bool, error) {
	before, err := domain.ReadyIDSet(ctx, q)
	if err != nil {
		return nil, err
	}
	if err := fn(); err != nil {
		return nil, err
	}
	after, err := domain.ReadyIDSet(ctx, q)
	if err != nil {
		return nil, err
	}
	for id := range after {
		delete(before, id)
	}
	return before, nil
}

func newlyRows(ctx context.Context, q domain.Querier, n domain.ShortIDs, ids map[string]bool, defaultNamespace string) ([]output.Cond, []output.BeadRow, error) {
	namespace, err := domain.ResolveNamespaceForFilter("", false, os.Getenv, defaultNamespace)
	if err != nil {
		return nil, nil, err
	}
	beads := make([]domain.Bead, 0, len(ids))
	canonicalIDs := make([]string, 0, len(ids))
	for id := range ids {
		b, err := domain.GetBead(ctx, q, id)
		if err != nil {
			return nil, nil, err
		}
		if namespace != "" && b.Namespace != namespace {
			continue
		}
		beads = append(beads, b)
		canonicalIDs = append(canonicalIDs, id)
	}
	resolver, err := domain.NewAliasResolverFor(ctx, q, canonicalIDs)
	if err != nil {
		return nil, nil, err
	}
	rows, err := beadRows(beads, n, resolver)
	if err != nil {
		return nil, nil, err
	}
	eff, err := domain.RaisedPriorities(ctx, q, effNow())
	if err != nil {
		return nil, nil, err
	}
	if err := withEffective(ctx, q, rows, eff, n); err != nil {
		return nil, nil, err
	}
	return namespaceConds(namespace), rows, nil
}

func effNow() string {
	t, err := domain.Now(os.Getenv)
	if err != nil {
		t = time.Now()
	}
	return domain.FormatTime(t)
}

func withEffective(ctx context.Context, q domain.Querier, rows []output.BeadRow, eff map[string]domain.EffectivePriority, n domain.ShortIDs) error {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.CanonicalID
	}
	expired, err := domain.ExpiredExpedites(ctx, q, effNow())
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].ExpediteExpired = expired[rows[i].CanonicalID]
	}
	sources, err := effectiveSources(ctx, q, eff, ids, n)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}
	for i := range rows {
		if src, ok := sources[rows[i].CanonicalID]; ok {
			rows[i].EffPriority = eff[rows[i].CanonicalID].Priority
			rows[i].EffSource = src
		}
	}
	output.SortBeads(rows)
	return nil
}

func effectiveSources(ctx context.Context, q domain.Querier, eff map[string]domain.EffectivePriority, ids []string, n domain.ShortIDs) (map[string]string, error) {
	if err := domain.FillSources(ctx, q, effNow(), eff, ids); err != nil {
		return nil, err
	}
	var sources []string
	for _, id := range ids {
		if e := eff[id]; e.Source != "" && e.Source != domain.SourceExpedite {
			sources = append(sources, e.Source)
		}
	}
	if len(sources) == 0 && !hasExpediteSource(eff, ids) {
		return map[string]string{}, nil
	}
	resolver, err := domain.NewAliasResolverFor(ctx, q, sources)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		e := eff[id]
		if e.Source == "" {
			continue
		}
		if e.Source == domain.SourceExpedite {
			out[id] = domain.SourceExpedite
			continue
		}
		display, err := sourceDisplayID(ctx, q, resolver, e.Source, n)
		if err != nil {
			return nil, err
		}
		out[id] = display
	}
	return out, nil
}

func hasExpediteSource(eff map[string]domain.EffectivePriority, ids []string) bool {
	for _, id := range ids {
		if eff[id].Source == domain.SourceExpedite {
			return true
		}
	}
	return false
}

func sourceDisplayID(ctx context.Context, q domain.Querier, resolver *domain.AliasResolver, id string, n domain.ShortIDs) (string, error) {
	src, err := domain.GetBead(ctx, q, id)
	if err != nil {
		return "", err
	}
	return aliasOrDisplayID(resolver, src.Namespace, src.ID, n)
}

func beadRowsForIDs(ctx context.Context, q domain.Querier, n domain.ShortIDs, ids []string) ([]output.BeadRow, error) {
	beads := make([]domain.Bead, 0, len(ids))
	for _, id := range ids {
		b, err := domain.GetBead(ctx, q, id)
		if err != nil {
			return nil, err
		}
		beads = append(beads, b)
	}
	resolver, err := domain.NewAliasResolverFor(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	return beadRows(beads, n, resolver)
}

type readyFlags struct {
	beadType      string
	priority      int
	claimedBy     string
	label         string
	namespaceFlag string
	limit         int
	namespaceSet  bool
	filter        filterFlags

	claim    bool
	actor    string
	claimTTL time.Duration
}

func parseReadyFlags(name string, args []string, stderr io.Writer) (*flag.FlagSet, readyFlags, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	var rf readyFlags
	fs.StringVar(&rf.beadType, flagType, "", "filter by type")
	fs.IntVar(&rf.priority, flagPriority, -1, "filter by priority (1-4); -1 means no filter")
	fs.StringVar(&rf.claimedBy, "claimed-by", "", "filter by claimed_by")
	fs.StringVar(&rf.label, flagLabel, "", "filter by label")
	fs.StringVar(&rf.namespaceFlag, flagNamespace, "", "filter by namespace")
	fs.IntVar(&rf.limit, flagLimit, output.DefaultLimit, "max rows to show (0 for unlimited)")
	if name == cmdReady {
		fs.BoolVar(&rf.claim, "claim", false, "claim the first Ready Bead in the same transaction as the enumeration")
		fs.StringVar(&rf.actor, flagActor, "", actorFlagUsage+" (only with --claim)")
		fs.DurationVar(&rf.claimTTL, "claim-ttl", 0, "with --claim, claim period after which another actor may take the claim over (e.g. 30m)")
	}
	rf.filter.bind(fs)
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return fs, rf, err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == flagNamespace {
			rf.namespaceSet = true
		}
	})
	return fs, rf, nil
}

func resolveReadyFilter(ctx context.Context, q domain.Querier, rf readyFlags, n domain.ShortIDs, defaultNamespace string, stderr io.Writer) (domain.ReadyFilter, resolvedFilter, string, bool) {
	if err := validatePriority(rf.priority); err != nil {
		output.WriteError(stderr, err.Error())
		return domain.ReadyFilter{}, resolvedFilter{}, "", false
	}
	namespace, err := domain.ResolveNamespaceForFilter(rf.namespaceFlag, rf.namespaceSet, os.Getenv, defaultNamespace)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return domain.ReadyFilter{}, resolvedFilter{}, "", false
	}
	filt, ok := resolveFilterFlags(ctx, q, rf.filter, n, stderr)
	if !ok {
		return domain.ReadyFilter{}, resolvedFilter{}, "", false
	}
	f := domain.ReadyFilter{
		Type:          rf.beadType,
		ClaimedBy:     rf.claimedBy,
		Label:         rf.label,
		Namespace:     namespace,
		ParentID:      filt.parentID,
		UpdatedBefore: filt.updatedBefore,
		UpdatedAfter:  filt.updatedAfter,
		Limit:         rf.limit,
		Priority:      rf.priority,
		Now:           effNow(),
	}
	return f, filt, namespace, true
}

func openReadyDB(fs *flag.FlagSet, rf *readyFlags, actorFlag string) (*storage.DB, *config.Config, error) {
	dir, cfg, err := resolveDirAndLimit(os.Getenv, fs, &rf.limit)
	if err != nil {
		return nil, nil, err
	}
	db, err := openDBAt(dir, os.Getenv, actorFlag, cfg)
	if err != nil {
		return nil, nil, err
	}
	return db, cfg, nil
}

func beadRows(beads []domain.Bead, n domain.ShortIDs, resolver *domain.AliasResolver) ([]output.BeadRow, error) {
	rows, err := beadRowsUnsorted(beads, n, resolver)
	if err != nil {
		return nil, err
	}
	output.SortBeads(rows)
	return rows, nil
}

func beadRowsUnsorted(beads []domain.Bead, n domain.ShortIDs, resolver *domain.AliasResolver) ([]output.BeadRow, error) {
	rows := make([]output.BeadRow, len(beads))
	for i, b := range beads {
		alias, hasParent, err := resolver.Alias(b.ID)
		if err != nil {
			return nil, err
		}
		if !hasParent {
			alias = ""
		}
		parent, _ := resolver.Parent(b.ID)
		rows[i] = output.BeadRow{
			ID:                domain.DisplayID(b.Namespace, b.ID, n),
			Alias:             alias,
			Title:             b.Title,
			Status:            b.Status,
			Priority:          b.Priority,
			Type:              b.BeadType,
			ClaimedBy:         nsOrEmpty(b.ClaimedBy),
			CreatedAt:         b.CreatedAt,
			CanonicalID:       b.ID,
			ParentCanonicalID: parent,
			ExternalRefs:      b.ExternalRefs,
		}
	}
	return rows, nil
}

func runReady(args []string, stdout, stderr io.Writer) int {
	fs, rf, err := parseReadyFlags(cmdReady, args, stderr)
	if err != nil {
		return helpExitCode(err)
	}
	claim := rf.claim
	actorFlag := rf.actor
	fieldSet := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { fieldSet[f.Name] = true })
	if errMsg := claimFlagError(fieldSet, rf.claimTTL, claim); errMsg != "" {
		output.WriteError(stderr, errMsg)
		return 1
	}
	db, cfg, err := openReadyDB(fs, &rf, actorFlag)
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

	if claim {
		return runReadyClaim(ctx, db, n, f, filt, namespace, rf, actorFlag, stdout, stderr)
	}

	beads, eff, total, err := domain.ReadyBeadsEffective(ctx, db.SQL, f)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	resolver, err := resolverFor(ctx, db.SQL, beads)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	rows, err := beadRows(beads, n, resolver)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
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

func runReadyClaim(ctx context.Context, db *storage.DB, n domain.ShortIDs, f domain.ReadyFilter, filt resolvedFilter, namespace string, rf readyFlags, actorFlag string, stdout, stderr io.Writer) int {
	actorVal := actor(actorFlag, os.Getenv)
	now := storage.NowRFC3339Milli()
	clock, err := domain.Now(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	claimF := f
	claimF.Limit = 1

	var claimed *domain.Bead
	var eff map[string]domain.EffectivePriority
	err = db.WithWrite(ctx, func(tx *sql.Tx) error {
		beads, e, _, err := domain.ReadyBeadsEffective(ctx, tx, claimF)
		eff = e
		if err != nil {
			return err
		}
		if len(beads) == 0 {
			return nil
		}
		id := beads[0].ID
		in := domain.UpdateInput{ID: id, Claim: true, Priority: -1, Actor: actorVal, Now: now, Clock: clock, ClaimTTL: rf.claimTTL}
		if _, err := domain.UpdateBead(ctx, tx, in); err != nil {
			return err
		}
		b, err := domain.GetBead(ctx, tx, id)
		if err != nil {
			return err
		}
		claimed = &b
		return nil
	})
	if err != nil {
		if reportIfBusy(stderr, db.BusyTimeoutMS, err) {
			return 1
		}
		output.WriteError(stderr, err.Error())
		return 1
	}

	conds := namespaceConds(namespace)
	writeFilterHeader(stdout, conds, filt, rf.filter)

	if claimed == nil {
		writeBeadListFooter(stdout, nil, 0)
		return 0
	}

	resolver, err := domain.NewAliasResolverFor(ctx, db.SQL, []string{claimed.ID})
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	rows, err := beadRows([]domain.Bead{*claimed}, n, resolver)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	if err := withEffective(ctx, db.SQL, rows, eff, n); err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	output.WriteClaimed(stdout, domain.DisplayID(claimed.Namespace, claimed.ID, n))
	output.WriteBeadList(stdout, rows)
	return 0
}
