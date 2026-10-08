// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package estimate

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"
)

const MinSamples = 3

type Bead struct {
	ID        string
	Title     string
	Namespace string
	BeadType  string
	Priority  int
	HasPrio   bool
	Status    string

	Depth int
}

type Dep struct {
	BeadID      string
	DependsOnID string
	Type        string
	Removed     bool
}

type Actual struct {
	BeadType string
	Priority int
	HasPrio  bool
	Tokens   int
}

func ClosedActuals(beads map[string]Bead, tokenByBeadID map[string]int) []Actual {
	var out []Actual
	for beadID, tokens := range tokenByBeadID {
		bead, ok := beads[beadID]
		if !ok || bead.Status != "closed" {
			continue
		}
		out = append(out, Actual{BeadType: bead.BeadType, Priority: bead.Priority, HasPrio: bead.HasPrio, Tokens: tokens})
	}
	return out
}

func Percentile(sortedValues []int, pct float64) (float64, bool) {
	if len(sortedValues) == 0 {
		return 0, false
	}
	if len(sortedValues) == 1 {
		return float64(sortedValues[0]), true
	}
	k := float64(len(sortedValues)-1) * (pct / 100.0)
	f := math.Floor(k)
	c := math.Ceil(k)
	if f == c {
		return float64(sortedValues[int(k)]), true
	}
	d0 := float64(sortedValues[int(f)]) * (c - k)
	d1 := float64(sortedValues[int(c)]) * (k - f)
	return d0 + d1, true
}

func median(values []int) float64 {
	sv := append([]int(nil), values...)
	slices.Sort(sv)
	n := len(sv)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return float64(sv[n/2])
	}
	return float64(sv[n/2-1]+sv[n/2]) / 2
}

type TypePrioKey struct {
	BeadType string
	Priority int
}

func BuildGroups(actuals []Actual) (map[TypePrioKey][]int, map[string][]int, []int) {
	byTypePrio := map[TypePrioKey][]int{}
	byType := map[string][]int{}
	all := make([]int, 0, len(actuals))
	for _, a := range actuals {
		key := TypePrioKey{a.BeadType, a.Priority}
		byTypePrio[key] = append(byTypePrio[key], a.Tokens)
		byType[a.BeadType] = append(byType[a.BeadType], a.Tokens)
		all = append(all, a.Tokens)
	}
	return byTypePrio, byType, all
}

func Estimate(beadType string, priority int, byTypePrio map[TypePrioKey][]int, byType map[string][]int, all []int) (float64, string, bool) {
	tpValues := byTypePrio[TypePrioKey{beadType, priority}]
	if len(tpValues) >= MinSamples {
		return median(tpValues), "type/prio", true
	}
	tValues := byType[beadType]
	if len(tValues) >= MinSamples {
		return median(tValues), "type", true
	}
	if len(all) > 0 {
		return median(all), "all", true
	}
	return 0, "", false
}

type CostEntry struct {
	BeadID     string
	RecordedAt time.Time
	Tokens     int
}

func PerBeadActual(entries []CostEntry, resetAt time.Time) (float64, int, bool) {
	perBead := map[string]int{}
	for _, e := range entries {
		if e.RecordedAt.Before(resetAt) {
			continue
		}
		perBead[e.BeadID] += e.Tokens
	}
	if len(perBead) == 0 {
		return 0, 0, false
	}
	totals := make([]int, 0, len(perBead))
	for _, t := range perBead {
		totals = append(totals, t)
	}
	return median(totals), len(totals), true
}

func FmtK(tokens float64) string {
	if tokens < 0 {
		return "-" + FmtK(-tokens)
	}
	n := pythonRound(tokens)
	if n < 1000 {
		return strconv.Itoa(int(n))
	}
	const units = "kMG"
	for i := 0; ; i++ {
		v := n / math.Pow(1000, float64(i+1))
		decimals := 2
		if v >= 100 {
			decimals = 0
		} else if v >= 10 {
			decimals = 1
		}
		scale := math.Pow(10, float64(decimals))
		r := pythonRound(v*scale) / scale
		if r >= 1000 && i < len(units)-1 {
			continue
		}
		if decimals > 0 && r >= 1000/scale {
			decimals--
		}
		return strconv.FormatFloat(r, 'f', decimals, 64) + units[i:i+1]
	}
}

func pythonRound(x float64) float64 {
	floor := math.Floor(x)
	diff := x - floor
	switch {
	case diff < 0.5:
		return floor
	case diff > 0.5:
		return floor + 1
	default:
		if math.Mod(floor, 2) == 0 {
			return floor
		}
		return floor + 1
	}
}

type GateWait struct {
	BeadID  string
	GateIDs []string
	Via     map[string]string
}

func ComputeCourses(beads map[string]Bead, order []string, deps []Dep, readyIDs []string) ([][]string, []GateWait, []string) {
	done := initialDone(beads, order, readyIDs)
	blockersOf, childrenOf := indexDeps(deps)

	var courses [][]string
	for {
		var candidates []string
		for _, id := range order {
			if pendingOpen(id, beads, done) && allDone(blockersOf[id], done) && allDone(childrenOf[id], done) {
				candidates = append(candidates, id)
			}
		}
		if len(candidates) == 0 {
			break
		}
		courses = append(courses, candidates)
		for _, id := range candidates {
			done[id] = true
		}
	}

	orderIdx := make(map[string]int, len(order))
	for i, id := range order {
		orderIdx[id] = i
	}
	var gateWaiting []GateWait
	var unclassified []string
	for _, id := range order {
		if !pendingOpen(id, beads, done) {
			continue
		}
		via := waitingGates(id, beads, blockersOf, childrenOf, done)
		if len(via) == 0 {
			unclassified = append(unclassified, id)
			continue
		}
		gateWaiting = append(gateWaiting, GateWait{BeadID: id, GateIDs: sortedGateIDs(via, orderIdx), Via: via})
	}

	return courses, gateWaiting, unclassified
}

func initialDone(beads map[string]Bead, order, readyIDs []string) map[string]bool {
	done := map[string]bool{}
	for _, id := range order {
		switch beads[id].Status {
		case "closed", "cancelled", "in_progress":
			done[id] = true
		}
	}
	for _, id := range readyIDs {
		done[id] = true
	}
	return done
}

func indexDeps(deps []Dep) (map[string][]string, map[string][]string) {
	blockersOf := map[string][]string{}
	childrenOf := map[string][]string{}
	for _, d := range deps {
		if d.Removed {
			continue
		}
		switch d.Type {
		case "blocks":
			blockersOf[d.BeadID] = append(blockersOf[d.BeadID], d.DependsOnID)
		case "parent-child":
			childrenOf[d.DependsOnID] = append(childrenOf[d.DependsOnID], d.BeadID)
		}
	}
	return blockersOf, childrenOf
}

func pendingOpen(id string, beads map[string]Bead, done map[string]bool) bool {
	bead := beads[id]
	return !done[id] && bead.Status == "open" && bead.BeadType != "gate"
}

func allDone(ids []string, done map[string]bool) bool {
	for _, id := range ids {
		if !done[id] {
			return false
		}
	}
	return true
}

func sortedGateIDs(via map[string]string, orderIdx map[string]int) []string {
	gateIDs := make([]string, 0, len(via))
	for gid := range via {
		gateIDs = append(gateIDs, gid)
	}
	slices.SortFunc(gateIDs, func(a, b string) int {
		return cmp.Or(cmp.Compare(orderIdx[a], orderIdx[b]), cmp.Compare(a, b))
	})
	return gateIDs
}

func waitingGates(id string, beads map[string]Bead, blockersOf, childrenOf map[string][]string, done map[string]bool) map[string]string {
	via := map[string]string{}
	visited := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, p := range slices.Concat(blockersOf[cur], childrenOf[cur]) {
			if done[p] {
				continue
			}
			if beads[p].BeadType == "gate" {
				if _, ok := via[p]; !ok {
					via[p] = cur
				}
				continue
			}
			if !visited[p] {
				visited[p] = true
				queue = append(queue, p)
			}
		}
	}
	return via
}

type DayTotal struct {
	Date   string
	Tokens int
}

const MinRecordedDays = 3

const MaxRecordedDays = 7

func AutoBudget(days []DayTotal, now time.Time) (float64, int, bool) {
	nowDay := now.UTC().Format("2006-01-02")
	var filtered []DayTotal
	for _, d := range days {
		if d.Date >= nowDay {
			continue
		}
		filtered = append(filtered, d)
	}
	if len(filtered) < MinRecordedDays {
		return 0, 0, false
	}
	slices.SortFunc(filtered, func(a, b DayTotal) int { return cmp.Compare(b.Date, a.Date) })
	if len(filtered) > MaxRecordedDays {
		filtered = filtered[:MaxRecordedDays]
	}
	tokens := make([]int, len(filtered))
	for i, d := range filtered {
		tokens[i] = d.Tokens
	}
	return median(tokens), len(filtered), true
}

func RenderCalibration(byTypePrio map[TypePrioKey][]int, all []int) []string {
	lines := []string{"## Calibration", "", "| type | priority | n | median | P25 | P75 |", "| --- | --- | --- | --- | --- | --- |"}

	keys := make([]TypePrioKey, 0, len(byTypePrio))
	for k := range byTypePrio {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b TypePrioKey) int {
		return cmp.Or(
			cmp.Compare(a.BeadType, b.BeadType),
			cmp.Compare(a.Priority, b.Priority),
		)
	})

	for _, k := range keys {
		lines = append(lines, calibrationRow(k.BeadType, fmt.Sprintf("P%d", k.Priority), byTypePrio[k]))
	}

	if len(all) > 0 {
		lines = append(lines, calibrationRow("(all)", "(all)", all))
	}
	return lines
}

func calibrationRow(col1, col2 string, values []int) string {
	sv := append([]int(nil), values...)
	slices.Sort(sv)
	p25, _ := Percentile(sv, 25)
	p75, _ := Percentile(sv, 75)
	return fmt.Sprintf("| %s | %s | %d | %s | %s | %s |",
		col1, col2, len(sv), FmtK(median(sv)), FmtK(p25), FmtK(p75))
}

type DepthActual struct {
	BeadID string
	Depth  int
	Tokens int
}

func ClosedDepthActuals(beads map[string]Bead, tokenByBeadID map[string]int) []DepthActual {
	var out []DepthActual
	for beadID, tokens := range tokenByBeadID {
		if tokens < 1 {
			continue
		}
		bead, ok := beads[beadID]
		if !ok || bead.Status != "closed" || bead.Depth == 0 {
			continue
		}
		out = append(out, DepthActual{BeadID: beadID, Depth: bead.Depth, Tokens: tokens})
	}
	return out
}

func BuildDepthGroups(actuals []DepthActual) (map[int][]int, []int) {
	byDepth := map[int][]int{}
	all := make([]int, 0, len(actuals))
	for _, a := range actuals {
		byDepth[a.Depth] = append(byDepth[a.Depth], a.Tokens)
		all = append(all, a.Tokens)
	}
	return byDepth, all
}

func RenderDepthCalibration(byDepth map[int][]int, all []int) []string {
	lines := []string{"## Depth calibration", "", "| reasoning_depth | n | median | P25 | P75 |", "| --- | --- | --- | --- | --- |"}

	depths := make([]int, 0, len(byDepth))
	for d := range byDepth {
		depths = append(depths, d)
	}
	slices.Sort(depths)

	for _, d := range depths {
		lines = append(lines, depthCalibrationRow(strconv.Itoa(d), byDepth[d]))
	}

	if len(all) > 0 {
		lines = append(lines, depthCalibrationRow("(all)", all))
	}
	return lines
}

func depthCalibrationRow(label string, values []int) string {
	sv := append([]int(nil), values...)
	slices.Sort(sv)
	p25, _ := Percentile(sv, 25)
	p75, _ := Percentile(sv, 75)
	return fmt.Sprintf("| %s | %d | %s | %s | %s |",
		label, len(sv), FmtK(median(sv)), FmtK(p25), FmtK(p75))
}

type DepthOutlier struct {
	BeadID string

	Display string

	Depth        int
	Tokens       int
	CohortMedian float64
	Ratio        float64
}

const (
	depthOutlierRatioHigh = 2.0
	depthOutlierRatioLow  = 0.5
)

func DepthOutliers(actuals []DepthActual, byDepth map[int][]int) []DepthOutlier {
	cohortMedian := map[int]float64{}
	for d, values := range byDepth {
		if len(values) >= MinSamples {
			cohortMedian[d] = median(values)
		}
	}

	var out []DepthOutlier
	for _, a := range actuals {
		m, ok := cohortMedian[a.Depth]
		if !ok {
			continue
		}
		ratio := float64(a.Tokens) / m
		if ratio >= depthOutlierRatioHigh || ratio <= depthOutlierRatioLow {
			out = append(out, DepthOutlier{BeadID: a.BeadID, Depth: a.Depth, Tokens: a.Tokens, CohortMedian: m, Ratio: ratio})
		}
	}

	slices.SortFunc(out, func(x, y DepthOutlier) int {
		return cmp.Or(
			cmp.Compare(y.Ratio, x.Ratio),
			cmp.Compare(x.BeadID, y.BeadID),
		)
	})
	return out
}

func RenderDepthOutliers(outliers []DepthOutlier) []string {
	if len(outliers) == 0 {
		return nil
	}
	lines := []string{"## Depth outliers", "", "| bead | reasoning_depth | tokens | cohort median | ratio |", "| --- | --- | --- | --- | --- |"}
	for _, o := range outliers {
		bead := o.Display
		if bead == "" {
			bead = o.BeadID
		}
		lines = append(lines, fmt.Sprintf("| %s | %d | %s | %s | %.2f |",
			bead, o.Depth, FmtK(float64(o.Tokens)), FmtK(o.CohortMedian), o.Ratio))
	}
	return lines
}

type ReworkActual struct {
	BeadID  string
	Depth   int
	Reworks int
}

func RenderReworkRate(rows []ReworkActual) []string {
	lines := []string{"## Rework", "", "| reasoning_depth | n | reworked | rate | records |", "| --- | --- | --- | --- | --- |"}

	byDepth := map[int][]ReworkActual{}
	for _, r := range rows {
		byDepth[r.Depth] = append(byDepth[r.Depth], r)
	}

	var depths []int
	hasZero := false
	for d := range byDepth {
		if d == 0 {
			hasZero = true
			continue
		}
		depths = append(depths, d)
	}
	slices.Sort(depths)
	if hasZero {
		depths = append(depths, 0)
	}

	for _, d := range depths {
		label := strconv.Itoa(d)
		if d == 0 {
			label = "-"
		}
		lines = append(lines, reworkRateRow(label, byDepth[d]))
	}

	if len(rows) > 0 {
		lines = append(lines, reworkRateRow("(all)", rows))
	}
	return lines
}

func reworkRateRow(label string, rows []ReworkActual) string {
	n := len(rows)
	reworked, records := 0, 0
	for _, r := range rows {
		if r.Reworks > 0 {
			reworked++
		}
		records += r.Reworks
	}
	rate := 0.0
	if n > 0 {
		rate = float64(reworked) / float64(n) * 100
	}
	return fmt.Sprintf("| %s | %d | %d | %.0f%% | %d |", label, n, reworked, rate, records)
}

type ScopeGrowth struct {
	BeadID string

	Display string

	ClaimedAt          string
	DepsAfterClaim     int
	ChildrenAfterClaim int
}

func RenderScopeGrowth(rows []ScopeGrowth) []string {
	var kept []ScopeGrowth
	for _, r := range rows {
		if r.DepsAfterClaim+r.ChildrenAfterClaim > 0 {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	slices.SortFunc(kept, func(x, y ScopeGrowth) int {
		return cmp.Or(
			cmp.Compare(y.DepsAfterClaim+y.ChildrenAfterClaim, x.DepsAfterClaim+x.ChildrenAfterClaim),
			cmp.Compare(x.BeadID, y.BeadID),
		)
	})

	lines := []string{"## Scope growth", "", "| bead | claimed | deps after | children after |", "| --- | --- | --- | --- |"}
	for _, r := range kept {
		bead := r.Display
		if bead == "" {
			bead = r.BeadID
		}
		lines = append(lines, fmt.Sprintf("| %s | %s | %d | %d |", bead, r.ClaimedAt, r.DepsAfterClaim, r.ChildrenAfterClaim))
	}
	return lines
}

type ReleaseWindow struct {
	Remaining int
	Created   int
	Floor     time.Time
}

type ReleaseETAResult struct {
	Early, Late                  time.Time
	EarlyDiverges, LateDiverges  bool
	Skip                         bool
	EarlyDays, LateDays          float64
	ClosePerDayLo, ClosePerDayHi float64
	EarlyDaysNone, LateDaysNone  bool
}

func ReleaseETA(now time.Time, windowHours float64, closedTotal, closedWithPR int, releases []ReleaseWindow) []ReleaseETAResult {
	cHi := float64(closedTotal) / windowHours
	cLo := float64(closedWithPR) / windowHours

	results := make([]ReleaseETAResult, len(releases))
	cumRemaining := 0.0
	cumCreatedPerHour := 0.0
	for i, r := range releases {
		cumRemaining += float64(r.Remaining)
		cumCreatedPerHour += float64(r.Created) / windowHours
		if cumRemaining <= 0 {
			results[i] = ReleaseETAResult{Skip: true}
			continue
		}
		res := ReleaseETAResult{ClosePerDayLo: cLo * 24, ClosePerDayHi: cHi * 24}
		if cHi > 0 {
			res.EarlyDays = cumRemaining / (cHi * 24)
		} else {
			res.EarlyDaysNone = true
		}
		if cLo > 0 {
			res.LateDays = cumRemaining / (cLo * 24)
		} else {
			res.LateDaysNone = true
		}
		if denom := cHi - cumCreatedPerHour; denom > 0 {
			res.Early = now.Add(time.Duration(cumRemaining / denom * float64(time.Hour)))
		} else {
			res.EarlyDiverges = true
		}
		if denom := cLo - cumCreatedPerHour; denom > 0 {
			res.Late = now.Add(time.Duration(cumRemaining / denom * float64(time.Hour)))
		} else {
			res.LateDiverges = true
		}
		if !r.Floor.IsZero() {
			if !res.EarlyDiverges && res.Early.Before(r.Floor) {
				res.Early = r.Floor
			}
			if !res.LateDiverges && res.Late.Before(r.Floor) {
				res.Late = r.Floor
			}
		}
		results[i] = res
	}
	return results
}
