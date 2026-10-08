// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/estimate"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/waitlabel"
)

type aheadOpts struct {
	budget       float64
	hasBudget    bool
	calibration  bool
	allGateKinds bool
	weekUsed     float64
	weekUsedSet  bool
	weekResetAt  time.Time
	weekResetSet bool
}

func runAhead(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ahead", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = helpUsage(fs)
	budgetArg := fs.Int("budget", 0, "token budget to compare Ready/Course totals against (default: median of last 7 recorded token_costs days)")
	calibration := fs.Bool("calibration", false, "include the calibration table")
	namespaceFlag := fs.String(flagNamespace, "", "filter by namespace")
	allGateKinds := fs.Bool(flagAll, false, "count Beads held by a Gate of every kind in ## Milestones gate=, not just human/confirm or unkinded Gates")
	weekUsed := fs.Float64("week-used", 0, "weekly rate limit utilization in percent (pair with --week-reset)")
	weekReset := fs.String("week-reset", "", "RFC3339 time of the latest weekly rate limit reset (pair with --week-used)")
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return helpExitCode(err)
	}
	opts := aheadOpts{budget: float64(*budgetArg), calibration: *calibration, allGateKinds: *allGateKinds, weekUsed: *weekUsed}
	namespaceSet := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "budget":
			opts.hasBudget = true
		case flagNamespace:
			namespaceSet = true
		case "week-used":
			opts.weekUsedSet = true
		case "week-reset":
			opts.weekResetSet = true
		}
	})
	if opts.weekResetSet {
		t, perr := time.Parse(time.RFC3339, *weekReset)
		if perr != nil {
			output.WriteError(stderr, fmt.Sprintf("--week-reset: %v", perr))
			return 1
		}
		opts.weekResetAt = t
	}

	now, err := domain.Now(os.Getenv)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	db, cfg, err := openDB(os.Getenv, "")
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}

	namespace, err := domain.ResolveNamespaceForFilter(*namespaceFlag, namespaceSet, os.Getenv, cfg.NamespaceDefault)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	defer func() { _ = db.Close() }()

	out, err := aheadLines(context.Background(), db.SQL, now, namespace, opts)
	if err != nil {
		output.WriteError(stderr, err.Error())
		return 1
	}
	fmt.Fprintln(stdout, strings.Join(out, "\n"))
	return 0
}

type aheadView struct {
	ctx         context.Context
	q           domain.Querier
	n           domain.ShortIDs
	namespace   string
	opts        aheadOpts
	resolver    *domain.AliasResolver
	domainBeads map[string]domain.EstimateBead
	beads       map[string]estimate.Bead
	byTypePrio  map[estimate.TypePrioKey][]int
	byType      map[string][]int
	all         []int
	throughput  []domain.ThroughputBead
}

func aheadLines(ctx context.Context, q domain.Querier, now time.Time, namespace string, opts aheadOpts) ([]string, error) {
	n, err := domain.LoadShortIDs(ctx, q)
	if err != nil {
		return nil, err
	}

	gateEntries, gateEff, err := domain.GateListEffective(ctx, q, effNow())
	if err != nil {
		return nil, err
	}

	domainBeads, order, domainDeps, tokenByBeadID, readyIDs, err := domain.EstimateInput(ctx, q, domain.ReadyFilter{Priority: -1, Namespace: namespace})
	if err != nil {
		return nil, err
	}
	beads, deps := toEstimateGraph(domainBeads, domainDeps)
	byTypePrio, byType, all := estimate.BuildGroups(estimate.ClosedActuals(beads, tokenByBeadID))
	courses, gateWaiting, unclassified := estimate.ComputeCourses(beads, order, deps, readyIDs)

	openBeads, _, err := domain.ListBeads(ctx, q, domain.ListFilter{Priority: -1, Namespace: namespace})
	if err != nil {
		return nil, err
	}
	releases, inProgress := splitAheadBeads(openBeads)
	listed := append(append([]domain.Bead{}, releases...), inProgress...)
	resolver, err := domain.NewAliasResolverFor(ctx, q, aheadResolveIDs(listed, readyIDs, courses, gateWaiting, gateEntries, unclassified))
	if err != nil {
		return nil, err
	}
	throughput, err := domain.ThroughputInput(ctx, q, namespace)
	if err != nil {
		return nil, err
	}
	v := &aheadView{ctx: ctx, q: q, n: n, namespace: namespace, opts: opts, resolver: resolver, domainBeads: domainBeads, beads: beads, byTypePrio: byTypePrio, byType: byType, all: all, throughput: throughput}

	placed := aheadPlacement(readyIDs, courses, gateWaiting, gateEntries, opts.allGateKinds)
	listedRows, err := beadRowsUnsorted(listed, n, resolver)
	if err != nil {
		return nil, err
	}
	releasesSec, readyReleaseMarker := v.releasesSection(now, releases, listedRows[:len(releases)], newReleaseGraph(domainDeps), placed, gateWaiting, gateKindMap(gateEntries), gateLabelsMap(gateEntries), opts.allGateKinds)
	inProgressSec, inProgressTokens := v.inProgressSection(inProgress, listedRows[len(releases):])
	readySec, readyEst, readyTokens, err := v.readySection(readyIDs, readyReleaseMarker)
	if err != nil {
		return nil, err
	}
	courseSecs, courseTokens, courseBeads, err := v.courseSections(courses)
	if err != nil {
		return nil, err
	}
	keptGateWaiting, waitersByGate, gateWaitingTokens := v.keepGateWaiting(gateWaiting)
	gateWaitingSec, err := aheadGateWaitingSection(ctx, q, now, gateEntries, gateEff, waitersByGate, gateViasFrom(gateWaiting), domainBeads, resolver, n, func(id string) (float64, bool) {
		tokens, _, ok := v.estimate(id)
		return tokens, ok
	})
	if err != nil {
		return nil, err
	}
	unclassifiedSec, keptUnclassified, err := v.unclassifiedSection(unclassified)
	if err != nil {
		return nil, err
	}

	coursesTotalTokens := 0.0
	for _, t := range courseTokens {
		coursesTotalTokens += t
	}

	summarySec := appendHeading(nil, "## Summary")
	summarySec = append(summarySec, fmt.Sprintf("- In progress: %s tokens (%d beads)", estimate.FmtK(inProgressTokens), len(inProgress)))
	summarySec = append(summarySec, fmt.Sprintf("- Ready: %s tokens (%d beads)", estimate.FmtK(readyTokens), len(readyIDs)))
	summarySec = append(summarySec, fmt.Sprintf("- Courses: %s tokens (%d beads in %d courses)", estimate.FmtK(coursesTotalTokens), courseBeads, len(courseSecs)))
	summarySec = append(summarySec, fmt.Sprintf("- Gate waiting: %s tokens (%d beads)", estimate.FmtK(gateWaitingTokens), keptGateWaiting))
	if keptUnclassified > 0 {
		summarySec = append(summarySec, fmt.Sprintf("- 未分類: %d 件", keptUnclassified))
	}
	summarySec = append(summarySec, fmt.Sprintf("- Milestones: %d", len(releases)))

	budgetLines, batchSec, err := v.budgetLines(now, readyEst, readyTokens, len(readyIDs), inProgressTokens, courseTokens)
	if err != nil {
		return nil, err
	}
	summarySec = append(summarySec, budgetLines...)
	if opts.weekUsedSet || opts.weekResetSet {
		line, werr := v.weekLine()
		if werr != nil {
			return nil, werr
		}
		summarySec = append(summarySec, line)
	}

	calibSec, err := v.calibrationSection(order, tokenByBeadID)
	if err != nil {
		return nil, err
	}

	sections := [][]string{calibSec}
	for _, courseSec := range slices.Backward(courseSecs) {
		sections = append(sections, courseSec)
	}
	outlookSec := outlookSection(now, v.outlookInput(now, gateEff, readyIDs, inProgress, gateWaiting, gateEntries))
	sections = append(sections, unclassifiedSec, batchSec, readySec, inProgressSec, summarySec, outlookSec, releasesSec, gateWaitingSec)
	return assembleAhead(now, namespace, sections), nil
}

func assembleAhead(now time.Time, namespace string, sections [][]string) []string {
	out := []string{"# Ahead", "Now: " + now.Local().Format("2006-01-02 15:04:05 MST")}
	if conds := namespaceConds(namespace); len(conds) > 0 {
		var filterLine strings.Builder
		output.WriteFilter(&filterLine, conds)
		out = append(out, strings.TrimSuffix(filterLine.String(), "\n"))
	}
	for _, sec := range sections {
		if len(sec) == 0 {
			continue
		}
		out = appendHeading(out, sec[0])
		out = append(out, sec[1:]...)
	}
	return out
}

func splitAheadBeads(openBeads []domain.Bead) ([]domain.Bead, []domain.Bead) {
	var releases, inProgress []domain.Bead
	for _, b := range openBeads {
		if b.BeadType == domain.BeadTypeGate {
			continue
		}
		if b.Status == domain.StatusInProgress {
			inProgress = append(inProgress, b)
		}
		if len(releaseNames(b.Labels)) > 0 {
			releases = append(releases, b)
		}
	}
	return releases, inProgress
}

func aheadResolveIDs(listed []domain.Bead, readyIDs []string, courses [][]string, gateWaiting []estimate.GateWait, gateEntries []domain.GateListEntry, unclassified []string) []string {
	var allIDs []string
	for _, b := range listed {
		allIDs = append(allIDs, b.ID)
	}
	allIDs = append(allIDs, readyIDs...)
	for _, c := range courses {
		allIDs = append(allIDs, c...)
	}
	for _, gw := range gateWaiting {
		allIDs = append(allIDs, gw.BeadID)
		for _, via := range gw.Via {
			allIDs = append(allIDs, via)
		}
	}
	for _, e := range gateEntries {
		for _, bl := range e.Blocking {
			allIDs = append(allIDs, bl.ID)
		}
	}
	return append(allIDs, unclassified...)
}

type releaseGraph struct {
	childrenOf, blockersOf map[string][]string
}

func newReleaseGraph(deps []domain.EstimateDep) releaseGraph {
	g := releaseGraph{childrenOf: map[string][]string{}, blockersOf: map[string][]string{}}
	for _, d := range deps {
		switch d.Type {
		case domain.ParentChildDepType:
			g.childrenOf[d.DependsOnID] = append(g.childrenOf[d.DependsOnID], d.BeadID)
		case domain.BlocksDepType:
			g.blockersOf[d.BeadID] = append(g.blockersOf[d.BeadID], d.DependsOnID)
		}
	}
	return g
}

const (
	placeReady  = "ready"
	placeCourse = "course"
	placeGate   = "gate"
)

func aheadPlacement(readyIDs []string, courses [][]string, gateWaiting []estimate.GateWait, gateEntries []domain.GateListEntry, allGateKinds bool) map[string]string {
	gateKindByID := gateKindMap(gateEntries)
	placed := map[string]string{}
	for _, id := range readyIDs {
		placed[id] = placeReady
	}
	for _, c := range courses {
		for _, id := range c {
			placed[id] = placeCourse
		}
	}
	for _, gw := range gateWaiting {
		if allGateKinds || heldByPersonGate(gw.GateIDs, gateKindByID) {
			placed[gw.BeadID] = placeGate
		}
	}
	return placed
}

func heldByPersonGate(gateIDs []string, gateKindByID map[string]string) bool {
	for _, gid := range gateIDs {
		kind, known := gateKindByID[gid]
		if !known || kind == domain.GateKindHuman || kind == domain.GateKindUnknown {
			return true
		}
	}
	return false
}

func gateKindMap(gateEntries []domain.GateListEntry) map[string]string {
	m := make(map[string]string, len(gateEntries))
	for _, e := range gateEntries {
		m[e.ID] = domain.GateKindOf(e.Labels, e.Fields.Subject)
	}
	return m
}

func gateLabelsMap(gateEntries []domain.GateListEntry) map[string][]string {
	m := make(map[string][]string, len(gateEntries))
	for _, e := range gateEntries {
		m[e.ID] = e.Labels
	}
	return m
}

func gateWaitDates(labels []string) []string {
	prefix := waitlabel.Date.Prefix()
	var dates []string
	for _, l := range labels {
		if d, ok := strings.CutPrefix(l, prefix); ok {
			dates = append(dates, d)
		}
	}
	return dates
}

func releaseFloor(gateIDs map[string]bool, gateLabelsByID map[string][]string) time.Time {
	var floor time.Time
	raise := func(t time.Time) {
		if floor.IsZero() || t.After(floor) {
			floor = t
		}
	}
	afterPrefix := waitlabel.After.Prefix()
	for gid := range gateIDs {
		labels := gateLabelsByID[gid]
		for _, d := range gateWaitDates(labels) {
			if t, err := time.ParseInLocation("2006-01-02", d, jst); err == nil {
				raise(t)
			}
		}
		for _, l := range labels {
			if s, ok := strings.CutPrefix(l, afterPrefix); ok {
				if t, err := time.Parse(time.RFC3339, s); err == nil {
					raise(t)
				}
			}
		}
	}
	return floor
}

func heldByJudgeGate(gateIDs []string, gateKindByID map[string]string) bool {
	for _, gid := range gateIDs {
		if gateKindByID[gid] == domain.GateKindAdjudicate {
			return true
		}
	}
	return false
}

func heldByHumanGate(gateIDs []string, gateKindByID map[string]string) bool {
	for _, gid := range gateIDs {
		if gateKindByID[gid] == domain.GateKindHuman {
			return true
		}
	}
	return false
}

func (v *aheadView) estimate(id string) (float64, string, bool) {
	iss := v.beads[id]
	beadType := iss.BeadType
	if beadType == "" {
		beadType = "unknown"
	}
	return estimate.Estimate(beadType, iss.Priority, v.byTypePrio, v.byType, v.all)
}

func (v *aheadView) estLine(row output.BeadRow, id, suffix string) (string, float64, bool) {
	tokens, tag, ok := v.estimate(id)
	if !ok {
		return output.BeadLine(row) + "  est=n/a(no-data)" + suffix, 0, false
	}
	return fmt.Sprintf("%s  est=%s(%s)%s", output.BeadLine(row), estimate.FmtK(tokens), tag, suffix), tokens, true
}

func (v *aheadView) inNamespace(id string) bool {
	return v.namespace == "" || v.domainBeads[id].Namespace == v.namespace
}

func (v *aheadView) keepInNamespace(ids []string) []string {
	var kept []string
	for _, id := range ids {
		if v.inNamespace(id) {
			kept = append(kept, id)
		}
	}
	return kept
}

func (v *aheadView) rowFor(id string) (output.BeadRow, error) {
	b, err := domain.GetBead(v.ctx, v.q, id)
	if err != nil {
		return output.BeadRow{}, err
	}
	rows, err := beadRowsUnsorted([]domain.Bead{b}, v.n, v.resolver)
	if err != nil {
		return output.BeadRow{}, err
	}
	return rows[0], nil
}

func (v *aheadView) releasesSection(now time.Time, releases []domain.Bead, rows []output.BeadRow, g releaseGraph, placed map[string]string, gateWaiting []estimate.GateWait, gateKindByID map[string]string, gateLabelsByID map[string][]string, allGateKinds bool) ([]string, func(id string) string) {
	gateIDsByBead := make(map[string][]string, len(gateWaiting))
	for _, gw := range gateWaiting {
		gateIDsByBead[gw.BeadID] = gw.GateIDs
	}
	entries := make([]releaseEntry, len(releases))
	for i, b := range releases {
		self := milestoneSelfGates(b.ID, g, v.domainBeads, gateIDsByBead)
		selfHeld := len(self) > 0 && (allGateKinds || heldByPersonGate(self, gateKindByID))
		entries[i] = releaseEntry{bead: b, row: rows[i], stats: computeReleaseStats(b.ID, g, v.domainBeads, placed, selfHeld), selfGates: self}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].bead.Priority != entries[j].bead.Priority {
			return entries[i].bead.Priority < entries[j].bead.Priority
		}
		return entries[i].stats.remaining < entries[j].stats.remaining
	})

	sec := appendHeading(nil, "## Milestones")
	if len(entries) == 0 {
		return append(sec, "(none)"), func(string) string { return "" }
	}

	etaLines, etas, hasHumanGate, judge := v.releaseETAs(now, entries, g, gateIDsByBead, gateKindByID, gateLabelsByID)
	sec = append(sec, etaLines...)
	for i, e := range entries {
		line := fmt.Sprintf("%s  milestone=%s  %s", output.BeadLine(e.row), strings.Join(releaseNames(e.bead.Labels), ","), e.stats.String())
		abolished := hasAbolishedLabel(e.bead.Labels)
		if !etas[i].Skip && !abolished {
			line += "  " + formatReleaseDays(etas[i])
		}
		if !etas[i].Skip && !abolished {
			line += "  eta=" + formatReleaseETA(etas[i])
		}
		if judge[i] > 0 {
			line += fmt.Sprintf("  judge=%d", judge[i])
		}
		if abolished {
			line += "  廃止・棚卸し中"
		}
		if hasHumanGate[i] {
			line += "  Gate 待ち"
		}
		sec = append(sec, line)
	}
	frontReadyIDs := map[string]bool{}
	for _, id := range entries[0].stats.readyIDs {
		frontReadyIDs[id] = true
	}
	frontReleaseNames := strings.Join(releaseNames(entries[0].bead.Labels), ",")
	return sec, func(id string) string {
		if frontReadyIDs[id] {
			return "  milestone=" + frontReleaseNames
		}
		return ""
	}
}

func hasAbolishedLabel(labels []string) bool {
	for _, l := range labels {
		if strings.HasPrefix(l, domain.AbolishedByLabelPrefix) {
			return true
		}
	}
	return false
}

const releaseWindowHours = 72

func closeVelocityCounts(throughput []domain.ThroughputBead, now time.Time, windowHours float64) (int, int) {
	var closedTotal, closedWithPR int
	windowStart := now.Add(-time.Duration(windowHours * float64(time.Hour)))
	for _, tb := range throughput {
		if tb.Closed && !tb.ClosedAt.IsZero() && tb.ClosedAt.After(windowStart) && !tb.ClosedAt.After(now) {
			closedTotal++
			if tb.HasPR {
				closedWithPR++
			}
		}
	}
	return closedTotal, closedWithPR
}

func (v *aheadView) releaseETAs(now time.Time, entries []releaseEntry, g releaseGraph, gateIDsByBead map[string][]string, gateKindByID map[string]string, gateLabelsByID map[string][]string) ([]string, []estimate.ReleaseETAResult, []bool, []int) {
	windowStart := now.Add(-releaseWindowHours * time.Hour)
	createdAt := make(map[string]time.Time, len(v.throughput))
	for _, tb := range v.throughput {
		createdAt[tb.ID] = tb.CreatedAt
	}
	closedTotal, closedWithPR := closeVelocityCounts(v.throughput, now, releaseWindowHours)
	cLo := float64(closedWithPR) / releaseWindowHours
	cHi := float64(closedTotal) / releaseWindowHours
	headerLines := []string{fmt.Sprintf("- Window: %dh closed=%d(PR %d) → %.2f〜%.2f/h", releaseWindowHours, closedTotal, closedWithPR, cLo, cHi)}

	claimedOpen := map[string]bool{}
	claimedCreated := map[string]bool{}
	windows := make([]estimate.ReleaseWindow, len(entries))
	hasHumanGate := make([]bool, len(entries))
	judge := make([]int, len(entries))
	for i, e := range entries {
		var incRemaining, incCreated int
		floorGateIDs := map[string]bool{}
		if heldByHumanGate(e.selfGates, gateKindByID) {
			hasHumanGate[i] = true
		}
		if heldByJudgeGate(e.selfGates, gateKindByID) {
			judge[i]++
		}
		for _, gid := range e.selfGates {
			floorGateIDs[gid] = true
		}
		for _, id := range releaseDescendants(e.bead.ID, g, v.domainBeads) {
			b := v.domainBeads[id]
			unfinished := b.Status != domain.StatusClosed && b.Status != domain.StatusCancelled
			if unfinished && !claimedOpen[id] {
				claimedOpen[id] = true
				incRemaining++
			}
			if ca, ok := createdAt[id]; ok && ca.After(windowStart) && !ca.After(now) && !claimedCreated[id] {
				claimedCreated[id] = true
				incCreated++
			}
			if heldByHumanGate(gateIDsByBead[id], gateKindByID) {
				hasHumanGate[i] = true
			}
			if heldByJudgeGate(gateIDsByBead[id], gateKindByID) {
				judge[i]++
			}
			if unfinished {
				for _, gid := range gateIDsByBead[id] {
					floorGateIDs[gid] = true
				}
			}
		}
		windows[i] = estimate.ReleaseWindow{Remaining: incRemaining, Created: incCreated, Floor: releaseFloor(floorGateIDs, gateLabelsByID)}
	}
	etas := estimate.ReleaseETA(now, releaseWindowHours, closedTotal, closedWithPR, windows)
	return headerLines, etas, hasHumanGate, judge
}

func formatReleaseDays(r estimate.ReleaseETAResult) string {
	early, late := "見込み無し", "見込み無し"
	if !r.EarlyDaysNone {
		early = fmt.Sprintf("%.1f日", r.EarlyDays)
	}
	if !r.LateDaysNone {
		late = fmt.Sprintf("%.1f日", r.LateDays)
	}
	days := early + "〜" + late
	if r.EarlyDaysNone && r.LateDaysNone {
		days = "見込み無し"
	}
	return fmt.Sprintf("close/日=%.1f〜%.1f  残り日数=%s", r.ClosePerDayLo, r.ClosePerDayHi, days)
}

var jst = time.FixedZone("JST", 9*60*60)

func formatReleaseETA(r estimate.ReleaseETAResult) string {
	if r.EarlyDiverges && r.LateDiverges {
		return "発散"
	}
	early, late := "発散", "発散"
	if !r.EarlyDiverges {
		early = r.Early.In(jst).Format("2006-01-02 15:04 JST")
	}
	if !r.LateDiverges {
		late = r.Late.In(jst).Format("2006-01-02 15:04 JST")
	}
	return early + "〜" + late
}

func (v *aheadView) inProgressSection(inProgress []domain.Bead, rows []output.BeadRow) ([]string, float64) {
	sec := appendHeading(nil, "## In progress")
	if len(inProgress) == 0 {
		sec = append(sec, "(none)")
	}
	total := 0.0
	for i, b := range inProgress {
		line, tokens, _ := v.estLine(rows[i], b.ID, "")
		total += tokens
		sec = append(sec, line)
	}
	return sec, total
}

func (v *aheadView) readySection(readyIDs []string, releaseMarker func(id string) string) ([]string, []aheadEst, float64, error) {
	sec := appendHeading(nil, "## Ready")
	if len(readyIDs) == 0 {
		return append(sec, "(none)"), nil, 0, nil
	}
	var ests []aheadEst
	total := 0.0
	for _, id := range readyIDs {
		row, err := v.rowFor(id)
		if err != nil {
			return nil, nil, 0, err
		}
		line, tokens, ok := v.estLine(row, id, releaseMarker(id))
		sec = append(sec, line)
		if ok {
			total += tokens
			ests = append(ests, aheadEst{line: line, tokens: tokens})
		}
	}
	return sec, ests, total, nil
}

func (v *aheadView) courseSections(courses [][]string) ([][]string, []float64, int, error) {
	var secs [][]string
	var beads int
	courseTokens := make([]float64, len(courses))
	for ci, course := range courses {
		kept := v.keepInNamespace(course)
		if len(kept) == 0 {
			continue
		}
		sec := appendHeading(nil, fmt.Sprintf("## Course %d", ci+1))
		for _, id := range kept {
			row, rerr := v.rowFor(id)
			if rerr != nil {
				return nil, nil, 0, rerr
			}
			beads++
			line, tokens, _ := v.estLine(row, id, "")
			courseTokens[ci] += tokens
			sec = append(sec, line)
		}
		secs = append(secs, sec)
	}
	return secs, courseTokens, beads, nil
}

func (v *aheadView) keepGateWaiting(gateWaiting []estimate.GateWait) (int, map[string][]string, float64) {
	var kept int
	var tokens float64
	waitersByGate := map[string][]string{}
	for _, gw := range gateWaiting {
		if !v.inNamespace(gw.BeadID) {
			continue
		}
		kept++
		if t, _, ok := v.estimate(gw.BeadID); ok {
			tokens += t
		}
		for _, gid := range gw.GateIDs {
			waitersByGate[gid] = append(waitersByGate[gid], gw.BeadID)
		}
	}
	return kept, waitersByGate, tokens
}

func (v *aheadView) unclassifiedSection(unclassified []string) ([]string, int, error) {
	kept := v.keepInNamespace(unclassified)
	if len(kept) == 0 {
		return nil, 0, nil
	}
	sec := appendHeading(nil, "## 未分類")
	for _, id := range kept {
		row, err := v.rowFor(id)
		if err != nil {
			return nil, 0, err
		}
		sec = append(sec, output.BeadLine(row))
	}
	return sec, len(kept), nil
}

func (v *aheadView) budgetLines(now time.Time, readyEst []aheadEst, readyTokens float64, readyCount int, inProgressTokens float64, courseTokens []float64) ([]string, []string, error) {
	var lines []string
	budget, hasBudgetValue := v.opts.budget, v.opts.hasBudget
	if hasBudgetValue {
		lines = append(lines, aheadBudgetLine("- Budget", budget, readyTokens, courseTokens))
	} else {
		domainDays, derr := domain.DailyTokenTotals(v.ctx, v.q, now, v.namespace)
		if derr != nil {
			return nil, nil, derr
		}
		days := make([]estimate.DayTotal, len(domainDays))
		for i, d := range domainDays {
			days[i] = estimate.DayTotal{Date: d.Date, Tokens: d.Tokens}
		}
		autoBudget, usedDays, ok := estimate.AutoBudget(days, now)
		if !ok {
			return append(lines, "- Budget: n/a (fewer than 3 recorded days; pass --budget)"), nil, nil
		}
		label := fmt.Sprintf("- Budget (auto, median of last %d recorded days)", usedDays)
		lines = append(lines, aheadBudgetLine(label, autoBudget, readyTokens, courseTokens))
		budget = autoBudget
	}
	available := budget - inProgressTokens
	batch, batchTokens := aheadBatch(readyEst, available)
	sameAsReady := len(batch) == readyCount
	fold := ""
	if sameAsReady {
		fold = ", same as Ready"
	}
	lines = append(lines,
		fmt.Sprintf("- Reserved by In progress: %s tokens, available: %s", estimate.FmtK(inProgressTokens), estimate.FmtK(available)),
		fmt.Sprintf("- Batch: %s tokens (%d beads, estimated%s)", estimate.FmtK(batchTokens), len(batch), fold))
	if sameAsReady {
		return lines, nil, nil
	}
	if len(batch) == 0 {
		batch = []string{"(none)"}
	}
	return lines, append(appendHeading(nil, "## Batch"), batch...), nil
}

func (v *aheadView) weekLine() (string, error) {
	var entries []estimate.CostEntry
	if v.opts.weekUsedSet && v.opts.weekResetSet {
		domainEntries, err := domain.TokenCostEntries(v.ctx, v.q, v.namespace)
		if err != nil {
			return "", err
		}
		for _, e := range domainEntries {
			entries = append(entries, estimate.CostEntry{BeadID: e.BeadID, RecordedAt: e.RecordedAt, Tokens: e.Tokens})
		}
	}
	return aheadWeekLine(v.opts.weekUsed, v.opts.weekUsedSet, v.opts.weekResetAt, v.opts.weekResetSet, entries), nil
}

func appendRendered(sec, lines []string) []string {
	if len(lines) == 0 {
		return sec
	}
	sec = appendHeading(sec, lines[0])
	return append(sec, lines[1:]...)
}

func (v *aheadView) calibrationSection(order []string, tokenByBeadID map[string]int) ([]string, error) {
	if !v.opts.calibration {
		return nil, nil
	}
	sec := appendRendered(nil, estimate.RenderCalibration(v.byTypePrio, v.all))

	depthActuals := estimate.ClosedDepthActuals(v.beads, tokenByBeadID)
	byDepth, allDepth := estimate.BuildDepthGroups(depthActuals)
	sec = appendRendered(sec, estimate.RenderDepthCalibration(byDepth, allDepth))
	outliers := estimate.DepthOutliers(depthActuals, byDepth)
	for i := range outliers {
		outliers[i].Display = domain.DisplayID(v.domainBeads[outliers[i].BeadID].Namespace, outliers[i].BeadID, v.n)
	}
	sec = appendRendered(sec, estimate.RenderDepthOutliers(outliers))

	derivedMetrics, err := domain.DerivedMetrics(v.ctx, v.q, order)
	if err != nil {
		return nil, err
	}
	sec = appendRendered(sec, estimate.RenderReworkRate(v.reworkActuals(order, derivedMetrics)))
	return appendRendered(sec, estimate.RenderScopeGrowth(v.scopeGrowth(derivedMetrics))), nil
}

func (v *aheadView) reworkActuals(order []string, derivedMetrics map[string]domain.DerivedMetric) []estimate.ReworkActual {
	var actuals []estimate.ReworkActual
	for _, id := range order {
		iss := v.domainBeads[id]
		if iss.Status != domain.StatusClosed || iss.BeadType == domain.BeadTypeGate {
			continue
		}
		actuals = append(actuals, estimate.ReworkActual{BeadID: id, Depth: iss.Depth, Reworks: derivedMetrics[id].ReworkCount})
	}
	return actuals
}

func (v *aheadView) scopeGrowth(derivedMetrics map[string]domain.DerivedMetric) []estimate.ScopeGrowth {
	growth := make([]estimate.ScopeGrowth, 0, len(derivedMetrics))
	for id, m := range derivedMetrics {
		if m.ClaimedAt == "" {
			continue
		}
		growth = append(growth, estimate.ScopeGrowth{
			BeadID:             id,
			Display:            domain.DisplayID(v.domainBeads[id].Namespace, id, v.n),
			ClaimedAt:          m.ClaimedAt,
			DepsAfterClaim:     m.DepsAfterClaim,
			ChildrenAfterClaim: m.ChildrenAfterClaim,
		})
	}
	return growth
}

func aheadGateWaitingSection(ctx context.Context, q domain.Querier, now time.Time, entries []domain.GateListEntry, eff map[string]domain.EffectivePriority, waitersByGate map[string][]string, viaByGate map[string][]gateVia, graph map[string]domain.EstimateBead, resolver *domain.AliasResolver, n domain.ShortIDs, estimateOf func(id string) (float64, bool)) ([]string, error) {
	bySec := map[string][]domain.GateListEntry{}
	var gateIDs []string
	for _, e := range entries {
		if len(waitersByGate[e.ID]) == 0 {
			continue
		}
		sec := gateSectionOf(e)
		bySec[sec] = append(bySec[sec], e)
		gateIDs = append(gateIDs, e.ID)
	}
	sec := appendHeading(nil, "## Gate waiting")
	if len(gateIDs) == 0 {
		return append(sec, "(none)"), nil
	}
	effSources, err := effectiveSources(ctx, q, eff, gateIDs, n)
	if err != nil {
		return nil, err
	}

	folded := []struct{ key, label string }{
		{gateSecExternal, "Opens automatically"},
		{gateSecAdjudicate, "Waiting on judge"},
		{gateSecUnknown, "Unknown kind (fix labels)"},
		{gateSecPrereq, "Blocked by other work"},
	}
	for _, f := range folded {
		gates := bySec[f.key]
		if len(gates) == 0 {
			continue
		}
		waiting, est := gateWaitSummary(gates, waitersByGate, estimateOf)
		sec = append(sec, fmt.Sprintf("- %s: %d gates, waiting=%d est=%s", f.label, len(gates), waiting, est))
	}
	if stalled := stalledGateIDs(bySec[gateSecExternal], now, n); len(stalled) > 0 {
		sec = append(sec, fmt.Sprintf("- Stalled: %s (wait:date passed but still open)", strings.Join(stalled, ", ")))
	}

	if humanGates := bySec[gateSecHuman]; len(humanGates) > 0 {
		sec = appendHeading(sec, "### Needs you")
		for _, e := range humanGates {
			line, err := gateLine(e, resolver, graph, viaByGate[e.ID], eff, effSources, n)
			if err != nil {
				return nil, err
			}
			waiters := waitersByGate[e.ID]
			total, known := sumEstimates(waiters, estimateOf)
			est := "n/a(no-data)"
			if known {
				est = estimate.FmtK(total)
			}
			sec = append(sec, fmt.Sprintf("%s  waiting=%d est=%s", line, len(waiters), est))
		}
	}
	return sec, nil
}

func sumEstimates(ids []string, estimateOf func(id string) (float64, bool)) (float64, bool) {
	var total float64
	var known bool
	for _, id := range ids {
		if tokens, ok := estimateOf(id); ok {
			total += tokens
			known = true
		}
	}
	return total, known
}

func gateWaitSummary(gates []domain.GateListEntry, waitersByGate map[string][]string, estimateOf func(id string) (float64, bool)) (int, string) {
	seen := map[string]bool{}
	var uniq []string
	for _, e := range gates {
		for _, id := range waitersByGate[e.ID] {
			if !seen[id] {
				seen[id] = true
				uniq = append(uniq, id)
			}
		}
	}
	total, known := sumEstimates(uniq, estimateOf)
	est := "n/a(no-data)"
	if known {
		est = estimate.FmtK(total)
	}
	return len(uniq), est
}

func stalledGateIDs(gates []domain.GateListEntry, now time.Time, n domain.ShortIDs) []string {
	var ids []string
	for _, e := range gates {
		dates, any, dateOnly := onlyWaitDateLabels(e.Labels)
		if !dateOnly || len(dates) == 0 || !datesPassed(dates, any, now) {
			continue
		}
		ids = append(ids, domain.DisplayID(e.Namespace, e.ID, n))
	}
	return ids
}

func onlyWaitDateLabels(labels []string) ([]string, bool, bool) {
	var dates []string
	var any bool
	dateOnly := true
	for _, l := range labels {
		switch {
		case l == waitlabel.Any:
			any = true
		case strings.HasPrefix(l, waitlabel.Prefix+"date:"):
			dates = append(dates, strings.TrimPrefix(l, waitlabel.Prefix+"date:"))
		case waitlabel.IsProbe(l):
			return nil, false, false
		}
	}
	return dates, any, dateOnly
}

func datesPassed(dates []string, any bool, now time.Time) bool {
	passed := 0
	for _, d := range dates {
		t, err := time.ParseInLocation("2006-01-02", d, now.Location())
		if err != nil {
			continue
		}
		if !now.Before(t) {
			passed++
		}
	}
	if any {
		return passed > 0
	}
	return passed == len(dates)
}

func toEstimateGraph(domainBeads map[string]domain.EstimateBead, domainDeps []domain.EstimateDep) (map[string]estimate.Bead, []estimate.Dep) {
	beads := make(map[string]estimate.Bead, len(domainBeads))
	for id, b := range domainBeads {
		beads[id] = estimate.Bead{ID: b.ID, Title: b.Title, BeadType: b.BeadType, Priority: b.Priority, HasPrio: true, Status: b.Status, Depth: b.Depth}
	}
	deps := make([]estimate.Dep, len(domainDeps))
	for i, d := range domainDeps {
		deps[i] = estimate.Dep{BeadID: d.BeadID, DependsOnID: d.DependsOnID, Type: d.Type}
	}
	return beads, deps
}

func appendHeading(out []string, heading string) []string {
	if len(out) > 0 {
		out = append(out, "")
	}
	return append(out, heading)
}

func releaseNames(labels []string) []string {
	var names []string
	for _, l := range labels {
		if name, ok := strings.CutPrefix(l, "milestone:"); ok {
			names = append(names, name)
		}
	}
	return names
}

type releaseEntry struct {
	bead      domain.Bead
	row       output.BeadRow
	stats     releaseStats
	selfGates []string
}

type releaseStats struct {
	ready, wip, course, gate, remaining int
	readyIDs                            []string
}

func (s releaseStats) String() string {
	return fmt.Sprintf("ready=%d wip=%d course=%d gate=%d remaining=%d", s.ready, s.wip, s.course, s.gate, s.remaining)
}

func releaseDescendants(root string, g releaseGraph, beads map[string]domain.EstimateBead) []string {
	var ids []string
	seen := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		next := g.childrenOf[id]
		if b := beads[id]; b.BeadType != domain.BeadTypeGate && unfinishedBead(b) {
			for _, p := range g.blockersOf[id] {
				if pb, ok := beads[p]; ok && pb.BeadType != domain.BeadTypeGate && unfinishedBead(pb) {
					next = append(slices.Clip(next), p)
				}
			}
		}
		for _, c := range next {
			if !seen[c] {
				seen[c] = true
				queue = append(queue, c)
			}
		}
		if id == root || beads[id].BeadType == domain.BeadTypeGate {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func unfinishedBead(b domain.EstimateBead) bool {
	return b.Status != domain.StatusClosed && b.Status != domain.StatusCancelled
}

func milestoneSelfGates(root string, g releaseGraph, beads map[string]domain.EstimateBead, gateIDsByBead map[string][]string) []string {
	seen := map[string]bool{}
	var gates []string
	add := func(gid string) {
		if !seen[gid] {
			seen[gid] = true
			gates = append(gates, gid)
		}
	}
	for _, p := range g.blockersOf[root] {
		b, ok := beads[p]
		if !ok || !unfinishedBead(b) {
			continue
		}
		if b.BeadType == domain.BeadTypeGate {
			add(p)
			continue
		}
		for _, gid := range gateIDsByBead[p] {
			add(gid)
		}
	}
	return gates
}

func computeReleaseStats(root string, g releaseGraph, beads map[string]domain.EstimateBead, placed map[string]string, selfHeld bool) releaseStats {
	var st releaseStats
	done, total := 0, 0
	for _, id := range releaseDescendants(root, g, beads) {
		b := beads[id]
		total++
		switch placed[id] {
		case placeReady:
			st.ready++
			st.readyIDs = append(st.readyIDs, id)
		case placeCourse:
			st.course++
		case placeGate:
			st.gate++
		}
		switch b.Status {
		case domain.StatusInProgress:
			st.wip++
		case domain.StatusClosed, domain.StatusCancelled:
			done++
		}
	}
	if selfHeld {
		st.gate++
	}
	st.remaining = total - done
	return st
}

type aheadEst struct {
	line   string
	tokens float64
}

func aheadBatch(ready []aheadEst, available float64) ([]string, float64) {
	var lines []string
	total := 0.0
	for _, r := range ready {
		if total+r.tokens > available {
			continue
		}
		total += r.tokens
		lines = append(lines, r.line)
	}
	return lines, total
}

func aheadWeekLine(used float64, usedSet bool, resetAt time.Time, resetSet bool, entries []estimate.CostEntry) string {
	const label = "- 週制限の残り"
	if !usedSet || !resetSet {
		return label + ": n/a (pass both --week-used and --week-reset)"
	}
	if used <= 0 || used > 100 {
		return label + ": n/a (--week-used must be in (0, 100])"
	}
	perBead, beads, ok := estimate.PerBeadActual(entries, resetAt)
	if !ok || beads < estimate.MinSamples {
		return fmt.Sprintf("%s: n/a (fewer than %d beads with token_costs since --week-reset)", label, estimate.MinSamples)
	}
	spent := 0
	for _, e := range entries {
		if !e.RecordedAt.Before(resetAt) {
			spent += e.Tokens
		}
	}
	remaining := (100 - used) / used * float64(spent)
	return fmt.Sprintf("%s ≈ %d 件 (used %g%%, remaining %s tokens, median %s/bead over %d beads)",
		label, int(math.Floor(remaining/perBead)), used, estimate.FmtK(remaining), estimate.FmtK(perBead), beads)
}

func aheadBudgetLine(label string, budget, readyTokens float64, courseTokens []float64) string {
	remainingAfterReady := budget - readyTokens
	if remainingAfterReady < 0 {
		return fmt.Sprintf("%s: %s, remaining after Ready: %s, covers Ready partially (short by %s)",
			label, estimate.FmtK(budget), estimate.FmtK(remainingAfterReady), estimate.FmtK(-remainingAfterReady))
	}
	remaining := remainingAfterReady
	covered := 0
	shortCourseIdx := -1
	var shortfall float64
	for ci, tokens := range courseTokens {
		if remaining >= tokens {
			remaining -= tokens
			covered = ci + 1
			continue
		}
		shortCourseIdx = ci + 1
		shortfall = tokens - remaining
		break
	}
	if shortCourseIdx == -1 {
		return fmt.Sprintf("%s: %s, remaining after Ready: %s, covers all courses (%s left)",
			label, estimate.FmtK(budget), estimate.FmtK(remainingAfterReady), estimate.FmtK(remaining))
	}
	return fmt.Sprintf("%s: %s, remaining after Ready: %s, covers through Course %d (Course %d short by %s)",
		label, estimate.FmtK(budget), estimate.FmtK(remainingAfterReady), covered, shortCourseIdx, estimate.FmtK(shortfall))
}
