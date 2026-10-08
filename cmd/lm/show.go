// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/estimate"
	"github.com/loombeading/loom/internal/output"
)

func runShow(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	allDeps := fs.Bool("all-deps", false, "include removed dependency links")
	full := fs.Bool("full", false, "show the full description instead of its compacted summary")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	if fs.NArg() != 1 {
		output.WriteError(stderr, "lm show requires exactly one ID")
		fmt.Fprintln(stderr, "See: lm help show")
		return 1
	}
	input := fs.Arg(0)

	now, err := domain.Now(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	dir, cfg, err := resolveDirAndConfig(os.Getenv)
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
	id, err := domain.ResolveID(ctx, db.SQL, input)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	bead, err := domain.GetBead(ctx, db.SQL, id)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	alias, err := domain.Alias(ctx, db.SQL, id)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	n, err := domain.LoadShortIDs(ctx, db.SQL)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	links, err := domain.Links(ctx, db.SQL, id, *allDeps)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	sentLinks, err := domain.SentLinks(ctx, db.SQL, id, *allDeps)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	history, err := domain.AuditHistory(ctx, db.SQL, id)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	tokenCost, err := domain.GetTokenCostTotals(ctx, db.SQL, id)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	tokenCostTotal, err := domain.GetTokenCostTotal(ctx, db.SQL, id)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	effective, raised, err := domain.EffectivePriorityOf(ctx, db.SQL, domain.FormatTime(now), id)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	var effectiveSource string
	if raised && effective.Source == domain.SourceExpedite {
		effectiveSource = domain.SourceExpedite
	} else if raised {
		resolver, err := domain.NewAliasResolverFor(ctx, db.SQL, []string{effective.Source})
		if err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
		effectiveSource, err = sourceDisplayID(ctx, db.SQL, resolver, effective.Source, n)
		if err != nil {
			output.WriteError(stderr, err.Error())
			return 1
		}
	}
	displayID := domain.DisplayID(bead.Namespace, bead.ID, n)
	writeFrontMatter(stdout, frontMatterFields{
		id:               bead.ID,
		alias:            alias,
		title:            bead.Title,
		status:           bead.Status,
		priority:         bead.Priority,
		effPriority:      effective.Priority,
		effSource:        effectiveSource,
		beadType:         bead.BeadType,
		labels:           bead.Labels,
		claimedBy:        bead.ClaimedBy,
		claimExpiresAt:   bead.ClaimExpiresAt,
		externalRefs:     bead.ExternalRefs,
		depth:            bead.Depth,
		createdAt:        bead.CreatedAt,
		updatedAt:        bead.UpdatedAt,
		closedAt:         bead.ClosedAt,
		severity:         bead.Severity,
		dueAt:            bead.DueAt,
		expediteUntil:    bead.ExpediteUntil,
		expediteReason:   bead.ExpediteReason,
		redetectKey:      bead.RedetectKey,
		lastRedetectedAt: bead.LastRedetectedAt,
		revivedAt:        bead.RevivedAt,
		tokenCost:        tokenCost.Total(),
		tokenCostTotal:   tokenCostTotal,
	})
	heading := output.Escape(displayID)
	if bead.ExpediteUntil.Valid && bead.ExpediteUntil.String <= domain.FormatTime(now) && !domain.IsTerminalStatus(bead.Status) {
		heading += " expedite-expired"
	}
	fmt.Fprintf(stdout, "# %s\n\n", heading)

	pickup, err := pickupLine(ctx, db.SQL, bead, now, n)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	output.WritePickup(stdout, pickup)

	retentionDays := cfg.RetentionDaysOr(output.DefaultRetentionDays)
	hasSummary := bead.Summary.Valid && bead.Summary.String != ""
	compacted := !*full && output.Compacted(bead.Status, bead.ClosedAt.Valid, bead.ClosedAt.String, hasSummary, retentionDays, now)
	if hasSummary && !compacted {
		fmt.Fprintln(stdout, "## Summary")
		fmt.Fprintln(stdout)
		fmt.Fprint(stdout, output.EscapeBlock(bead.Summary.String))
		fmt.Fprintln(stdout)
	}
	output.WriteDescription(stdout, compacted, bead.Description.Valid, bead.Description.String, bead.Summary.String, retentionDays)

	targets, err := linkTargetsFor(ctx, db.SQL, n, links, sentLinks, history)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	fmt.Fprintln(stdout, "## Dependencies")
	rows := append(depRows(links, targets, false), depRows(sentLinks, targets, true)...)
	output.WriteDeps(stdout, rows)
	fmt.Fprintln(stdout)

	if childrenLine := output.ChildrenLine(childStatusCounts(sentLinks, targets)); childrenLine != "" {
		fmt.Fprintln(stdout, "## Children")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, childrenLine)
		fmt.Fprintln(stdout)
	}

	if derivedRows := discoveredFromSentRows(sentLinks, targets); len(derivedRows) > 0 {
		fmt.Fprintln(stdout, "## Derived")
		fmt.Fprintln(stdout)
		output.WriteDerived(stdout, derivedRows)
		fmt.Fprintln(stdout)
	}

	fmt.Fprintln(stdout, "## History")
	output.WriteHistory(stdout, historyRows(history, targets))

	return 0
}

type frontMatterFields struct {
	id               string
	alias            string
	title            string
	status           string
	priority         int
	effPriority      int
	effSource        string
	beadType         string
	labels           []string
	claimedBy        sql.NullString
	claimExpiresAt   sql.NullString
	externalRefs     []string
	depth            sql.NullInt64
	createdAt        string
	updatedAt        string
	closedAt         sql.NullString
	severity         int
	dueAt            sql.NullString
	expediteUntil    sql.NullString
	expediteReason   sql.NullString
	redetectKey      sql.NullString
	lastRedetectedAt sql.NullString
	revivedAt        sql.NullString
	tokenCost        int
	tokenCostTotal   int
}

func writeFrontMatter(w io.Writer, f frontMatterFields) {
	fmt.Fprintln(w, "---")
	fmt.Fprintf(w, "id: %s\n", output.QuoteYAML(f.id))
	fmt.Fprintf(w, "alias: %s\n", stringOrNull(f.alias))
	fmt.Fprintf(w, "title: %s\n", output.QuoteYAML(f.title))
	fmt.Fprintf(w, "status: %s\n", output.QuoteYAML(f.status))
	fmt.Fprintf(w, "priority: %d\n", f.priority)
	fmt.Fprintf(w, "effective_priority: %d\n", f.effPriority)
	fmt.Fprintf(w, "effective_priority_source: %s\n", stringOrNull(f.effSource))
	fmt.Fprintf(w, "type: %s\n", output.QuoteYAML(f.beadType))
	fmt.Fprintf(w, "labels: %s\n", quoteYAMLStringList(f.labels))
	fmt.Fprintf(w, "claimed_by: %s\n", nsOrNull(f.claimedBy))
	fmt.Fprintf(w, "claim_expires_at: %s\n", nsOrNull(f.claimExpiresAt))
	fmt.Fprintf(w, "external_refs: %s\n", quoteYAMLStringList(f.externalRefs))
	fmt.Fprintf(w, "reasoning_depth: %s\n", niOrNull(f.depth))
	fmt.Fprintf(w, "created_at: %s\n", output.QuoteYAML(f.createdAt))
	fmt.Fprintf(w, "updated_at: %s\n", output.QuoteYAML(f.updatedAt))
	fmt.Fprintf(w, "closed_at: %s\n", nsOrNull(f.closedAt))
	fmt.Fprintf(w, "severity: %d\n", f.severity)
	fmt.Fprintf(w, "due_at: %s\n", nsOrNull(f.dueAt))
	fmt.Fprintf(w, "expedite_until: %s\n", nsOrNull(f.expediteUntil))
	fmt.Fprintf(w, "expedite_reason: %s\n", nsOrNull(f.expediteReason))
	fmt.Fprintf(w, "redetect_key: %s\n", nsOrNull(f.redetectKey))
	fmt.Fprintf(w, "last_redetected_at: %s\n", nsOrNull(f.lastRedetectedAt))
	fmt.Fprintf(w, "revived_at: %s\n", nsOrNull(f.revivedAt))
	fmt.Fprintf(w, "token_cost: %d\n", f.tokenCost)
	fmt.Fprintf(w, "token_cost_total: %d\n", f.tokenCostTotal)
	fmt.Fprintln(w, "---")
}

func quoteYAMLStringList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = output.QuoteYAML(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func niOrNull(v sql.NullInt64) string {
	if !v.Valid {
		return "null"
	}
	return strconv.FormatInt(v.Int64, 10)
}

func nsOrNull(v sql.NullString) string {
	if !v.Valid {
		return "null"
	}
	return output.QuoteYAML(v.String)
}

func stringOrNull(s string) string {
	if s == "" {
		return "null"
	}
	return output.QuoteYAML(s)
}

type linkTarget struct {
	Dangling     bool
	Display      string
	Status       string
	BeadType     string
	Priority     int
	Title        string
	ExternalRefs []string
}

func linkTargetsFor(ctx context.Context, q domain.Querier, n domain.ShortIDs, links, sentLinks []domain.DepRow, history []domain.AuditRow) (map[string]linkTarget, error) {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, l := range links {
		add(l.DependsOnID)
	}
	for _, l := range sentLinks {
		add(l.DependsOnID)
	}
	for _, h := range history {
		if h.Kind != domain.AuditKindField && h.Kind != domain.AuditKindMerge {
			continue
		}
		if !h.Field.Valid || h.Field.String != domain.FieldDependency || !h.NewValue.Valid {
			continue
		}
		var dv dependencyValue
		if err := json.Unmarshal([]byte(h.NewValue.String), &dv); err == nil {
			add(dv.DependsOnID)
		}
	}
	if len(ids) == 0 {
		return map[string]linkTarget{}, nil
	}

	resolver, err := domain.NewAliasResolverFor(ctx, q, ids)
	if err != nil {
		return nil, err
	}

	out := make(map[string]linkTarget, len(ids))
	for _, id := range ids {
		b, err := domain.GetBead(ctx, q, id)
		if errors.Is(err, domain.ErrBeadNotFound) {
			out[id] = linkTarget{Dangling: true, Display: id}
			continue
		}
		if err != nil {
			return nil, err
		}
		alias, hasParent, err := resolver.Alias(id)
		if err != nil {
			return nil, err
		}
		display := domain.DisplayID(b.Namespace, b.ID, n)
		if hasParent {
			display = alias
		}
		out[id] = linkTarget{
			Display:      display,
			Status:       b.Status,
			BeadType:     b.BeadType,
			Priority:     b.Priority,
			Title:        b.Title,
			ExternalRefs: b.ExternalRefs,
		}
	}
	return out, nil
}

type dependencyValue struct {
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
	Removed     bool   `json:"removed"`
}

func depRows(links []domain.DepRow, targets map[string]linkTarget, sent bool) []output.DepRow {
	rows := make([]output.DepRow, len(links))
	for i, l := range links {
		t := targets[l.DependsOnID]
		rows[i] = output.DepRow{
			Type:         l.Type,
			Sent:         sent,
			Dangling:     t.Dangling,
			Display:      t.Display,
			Status:       t.Status,
			BeadType:     t.BeadType,
			Priority:     t.Priority,
			Title:        t.Title,
			ExternalRefs: t.ExternalRefs,
			Removed:      l.Removed,
		}
	}
	return rows
}

func childStatusCounts(sentLinks []domain.DepRow, targets map[string]linkTarget) []output.ChildStatusCount {
	counts := []output.ChildStatusCount{
		{Status: domain.StatusOpen},
		{Status: domain.StatusInProgress},
		{Status: domain.StatusClosed},
		{Status: domain.StatusCancelled},
	}
	index := make(map[string]int, len(counts))
	for i, c := range counts {
		index[c.Status] = i
	}
	for _, l := range sentLinks {
		if l.Type != domain.ParentChildDepType || l.Removed {
			continue
		}
		t, ok := targets[l.DependsOnID]
		if !ok || t.Dangling {
			continue
		}
		if i, ok := index[t.Status]; ok {
			counts[i].Count++
		}
	}
	return counts
}

func discoveredFromSentRows(sentLinks []domain.DepRow, targets map[string]linkTarget) []output.DepRow {
	var rows []output.DepRow
	for _, l := range sentLinks {
		if l.Type != domain.DiscoveredFromDepType || l.Removed {
			continue
		}
		t := targets[l.DependsOnID]
		rows = append(rows, output.DepRow{
			Dangling:     t.Dangling,
			Display:      t.Display,
			Status:       t.Status,
			BeadType:     t.BeadType,
			Priority:     t.Priority,
			Title:        t.Title,
			ExternalRefs: t.ExternalRefs,
		})
	}
	return rows
}

func historyRows(history []domain.AuditRow, targets map[string]linkTarget) []output.HistoryRow {
	if len(history) == 0 {
		return nil
	}

	rows := make([]output.HistoryRow, 0, len(history))
	start := 0

	first := history[0]
	if first.Kind == domain.AuditKindField && first.Origin == domain.OriginLocal {
		end := 1
		for end < len(history) &&
			history[end].Kind == domain.AuditKindField &&
			history[end].Origin == domain.OriginLocal &&
			history[end].OccurredAt == first.OccurredAt &&
			history[end].Actor == first.Actor {
			end++
		}
		if end > 1 {
			fields := make([]string, end)
			for i, h := range history[:end] {
				fields[i] = nsOrEmpty(h.Field)
			}
			rows = append(rows, output.HistoryRow{
				Time:    first.OccurredAt,
				Actor:   first.Actor,
				Created: strings.Join(fields, ", "),
			})
			start = end
		}
	}

	for _, h := range history[start:] {
		rows = append(rows, historyRow(h, targets))
	}
	return rows
}

func historyRow(h domain.AuditRow, targets map[string]linkTarget) output.HistoryRow {
	r := output.HistoryRow{
		Time:  h.OccurredAt,
		Actor: h.Actor,
		Kind:  h.Kind,
	}
	if h.Field.Valid {
		r.HasField = true
		r.Field = h.Field.String
	}
	if h.NewValue.Valid {
		r.HasValue = true
		value := h.NewValue.String
		if h.Field.Valid && h.Field.String == domain.FieldDependency {
			var dv dependencyValue
			if err := json.Unmarshal([]byte(h.NewValue.String), &dv); err == nil {
				display := dv.DependsOnID
				if t, ok := targets[dv.DependsOnID]; ok {
					display = t.Display
				}
				value = fmt.Sprintf("%s ← %s", dv.Type, display)
			}
		}
		r.Value, r.Cut = output.TruncateValue(value)
	}
	if h.Reason.Valid && h.Reason.String != "" {
		r.HasReason = true
		var cut bool
		r.Reason, cut = output.TruncateValue(h.Reason.String)
		r.Cut = r.Cut || cut
	}
	return r
}

func nsOrEmpty(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func pickupLine(ctx context.Context, q domain.Querier, bead domain.Bead, now time.Time, n domain.ShortIDs) (string, error) {
	if bead.Status != domain.StatusOpen || bead.BeadType == domain.BeadTypeGate {
		return "", nil
	}

	ready, ahead, interrupts, err := domain.PickupAhead(ctx, q, bead.ID, now, releaseWindowHours)
	if err != nil {
		return "", err
	}
	if !ready {
		return waitingLine(ctx, q, bead.ID, n)
	}

	throughput, err := domain.ThroughputInput(ctx, q, "")
	if err != nil {
		return "", err
	}
	closedTotal, closedWithPR := closeVelocityCounts(throughput, now, releaseWindowHours)
	etas := estimate.ReleaseETA(now, releaseWindowHours, closedTotal, closedWithPR,
		[]estimate.ReleaseWindow{{Remaining: ahead, Created: interrupts}})
	return formatPickup(ahead, etas[0]), nil
}

func formatPickup(ahead int, r estimate.ReleaseETAResult) string {
	if r.Skip {
		return output.PickupNextLine
	}
	if r.EarlyDiverges && r.LateDiverges {
		return fmt.Sprintf(output.PickupOverrunFormat, ahead)
	}
	early, late := output.PickupDivergesLabel, output.PickupDivergesLabel
	if !r.EarlyDiverges {
		early = r.Early.In(jst).Format("2006-01-02 15:04 JST")
	}
	if !r.LateDiverges {
		late = r.Late.In(jst).Format("2006-01-02 15:04 JST")
	}
	return fmt.Sprintf(output.PickupEtaFormat, ahead, early, late)
}

func waitingLine(ctx context.Context, q domain.Querier, id string, n domain.ShortIDs) (string, error) {
	blockers, err := domain.ActiveBlockers(ctx, q, id)
	if err != nil {
		return "", err
	}
	ids := make([]string, len(blockers))
	for i, bl := range blockers {
		ids[i] = bl.ID
	}
	resolver, err := domain.NewAliasResolverFor(ctx, q, ids)
	if err != nil {
		return "", err
	}
	displays := make([]string, len(blockers))
	for i, bl := range blockers {
		displays[i], err = sourceDisplayID(ctx, q, resolver, bl.ID, n)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf(output.WaitingLineFormat, strings.Join(displays, ", ")), nil
}
