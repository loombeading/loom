// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package estimate

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func fixtureBeadsAndTokens() (map[string]Bead, map[string]int) {
	beads := map[string]Bead{
		"tp1-1":          {ID: "tp1-1", BeadType: "task", Priority: 1, HasPrio: true, Status: "closed"},
		"tp1-2":          {ID: "tp1-2", BeadType: "task", Priority: 1, HasPrio: true, Status: "closed"},
		"tp1-3":          {ID: "tp1-3", BeadType: "task", Priority: 1, HasPrio: true, Status: "closed"},
		"tp1-4":          {ID: "tp1-4", BeadType: "task", Priority: 1, HasPrio: true, Status: "closed"},
		"tp1-5":          {ID: "tp1-5", BeadType: "task", Priority: 1, HasPrio: true, Status: "closed"},
		"tp2-1":          {ID: "tp2-1", BeadType: "task", Priority: 2, HasPrio: true, Status: "closed"},
		"tp2-2":          {ID: "tp2-2", BeadType: "task", Priority: 2, HasPrio: true, Status: "closed"},
		"open-1":         {ID: "open-1", BeadType: "task", Priority: 1, HasPrio: true, Status: "open"},
		"aaareadytaskp1": {ID: "aaareadytaskp1", BeadType: "task", Priority: 1, HasPrio: true, Status: "open"},
		"bbbreadybugp1":  {ID: "bbbreadybugp1", BeadType: "bug", Priority: 1, HasPrio: true, Status: "open"},
	}
	tokenByBeadID := map[string]int{
		"tp1-1":  7500,
		"tp1-2":  15000,
		"tp1-3":  22500,
		"tp1-4":  30000,
		"tp1-5":  37500,
		"tp2-1":  100000,
		"tp2-2":  200000,
		"open-1": 1000000,
	}
	return beads, tokenByBeadID
}

func TestTaskP1UsesTypePrioMedian(t *testing.T) {
	beads, tokenByBeadID := fixtureBeadsAndTokens()
	actuals := ClosedActuals(beads, tokenByBeadID)
	byTypePrio, byType, all := BuildGroups(actuals)

	tokens, tag, ok := Estimate("task", 1, byTypePrio, byType, all)
	if !ok || tag != "type/prio" {
		t.Fatalf("task/P1: type/prio 段の中央値を使う: tag=%s ok=%v", tag, ok)
	}
	if got := FmtK(tokens); got != "22.5k" {
		t.Errorf("task/P1: est タグは type/prio: got %s", got)
	}

	_, tagB, okB := Estimate("bug", 1, byTypePrio, byType, all)
	if !okB || tagB != "all" {
		t.Errorf("bug/P1: all 段にフォールバックする: tag=%s ok=%v", tagB, okB)
	}
}

func TestCalibrationTableCounts(t *testing.T) {
	beads, tokenByBeadID := fixtureBeadsAndTokens()
	actuals := ClosedActuals(beads, tokenByBeadID)
	byTypePrio, _, all := BuildGroups(actuals)

	if n := len(byTypePrio[TypePrioKey{"task", 1}]); n != 5 {
		t.Errorf("較正表: task/P1 の n=5: got %d", n)
	}
	if n := len(byTypePrio[TypePrioKey{"task", 2}]); n != 2 {
		t.Errorf("較正表: task/P2 の n=2: got %d", n)
	}
	lines := RenderCalibration(byTypePrio, all)
	found := false
	for _, l := range lines {
		if l == "| task | P1 | 5 | 22.5k | 15.0k | 30.0k |" {
			found = true
		}
	}
	if !found {
		t.Errorf("較正表に task/P1 の行が無い: %v", lines)
	}
}

func fixtureCourses() (map[string]Bead, []string, []Dep) {
	order := []string{
		"r1", "a1", "a2", "gate1", "b1", "epic1", "c1", "c2",
		"d_blocker", "d1", "g_blocker", "e1", "task_parent1", "f1", "f2",
	}
	beads := map[string]Bead{
		"r1":           {ID: "r1", BeadType: "task", Priority: 1, Status: "open"},
		"a1":           {ID: "a1", BeadType: "task", Priority: 1, Status: "open"},
		"a2":           {ID: "a2", BeadType: "task", Priority: 1, Status: "open"},
		"gate1":        {ID: "gate1", BeadType: "gate", Priority: 1, Status: "open"},
		"b1":           {ID: "b1", BeadType: "task", Priority: 1, Status: "open"},
		"epic1":        {ID: "epic1", BeadType: "epic", Priority: 1, Status: "open"},
		"c1":           {ID: "c1", BeadType: "task", Priority: 1, Status: "open"},
		"c2":           {ID: "c2", BeadType: "task", Priority: 1, Status: "open"},
		"d_blocker":    {ID: "d_blocker", BeadType: "task", Priority: 1, Status: "in_progress"},
		"d1":           {ID: "d1", BeadType: "task", Priority: 1, Status: "open"},
		"g_blocker":    {ID: "g_blocker", BeadType: "task", Priority: 1, Status: "open"},
		"e1":           {ID: "e1", BeadType: "task", Priority: 1, Status: "open"},
		"task_parent1": {ID: "task_parent1", BeadType: "task", Priority: 1, Status: "open"},
		"f1":           {ID: "f1", BeadType: "task", Priority: 1, Status: "open"},
		"f2":           {ID: "f2", BeadType: "task", Priority: 1, Status: "open"},
	}
	deps := []Dep{
		{BeadID: "a1", DependsOnID: "r1", Type: "blocks"},
		{BeadID: "a2", DependsOnID: "a1", Type: "blocks"},
		{BeadID: "b1", DependsOnID: "gate1", Type: "blocks"},
		{BeadID: "c1", DependsOnID: "epic1", Type: "parent-child"},
		{BeadID: "c2", DependsOnID: "epic1", Type: "parent-child"},
		{BeadID: "d1", DependsOnID: "d_blocker", Type: "blocks"},
		{BeadID: "e1", DependsOnID: "g_blocker", Type: "blocks", Removed: true},
		{BeadID: "f1", DependsOnID: "task_parent1", Type: "parent-child"},
		{BeadID: "f2", DependsOnID: "task_parent1", Type: "parent-child"},
	}
	return beads, order, deps
}

func courseContains(courses [][]string, course int, id string) bool {
	if course >= len(courses) {
		return false
	}
	return slices.Contains(courses[course], id)
}

func TestComputeCourses(t *testing.T) {
	beads, order, deps := fixtureCourses()

	courses, gateWaiting, _ := ComputeCourses(beads, order, deps, []string{"r1"})

	t.Run("blocks_chain_spans_two_courses", func(t *testing.T) {
		if !courseContains(courses, 0, "a1") {
			t.Errorf("a1 (blocked only by ready r1) should be in course 1: %v", courses)
		}
		if !courseContains(courses, 1, "a2") {
			t.Errorf("a2 (blocked by a1) should be in course 2: %v", courses)
		}
	})

	t.Run("open_gate_blocked_goes_to_gate_waiting_not_courses", func(t *testing.T) {
		for ci, c := range courses {
			for _, id := range c {
				if id == "b1" {
					t.Errorf("b1 (blocked by open gate) should not be in any course: course %d", ci)
				}
			}
		}
		found := false
		for _, gw := range gateWaiting {
			if gw.BeadID == "b1" {
				found = true
				if len(gw.GateIDs) != 1 || gw.GateIDs[0] != "gate1" {
					t.Errorf("b1 gateWaiting.GateIDs = %v, want [gate1]", gw.GateIDs)
				}
			}
		}
		if !found {
			t.Errorf("b1 should be in gateWaiting: %v", gateWaiting)
		}
	})

	t.Run("epic_waits_for_all_children_then_next_course", func(t *testing.T) {
		if !courseContains(courses, 0, "c1") || !courseContains(courses, 0, "c2") {
			t.Errorf("c1/c2 (no blockers) should be in course 1: %v", courses)
		}
		if !courseContains(courses, 1, "epic1") {
			t.Errorf("epic1 should be in course 2, after children finish in course 1: %v", courses)
		}
	})

	t.Run("in_progress_blocker_counts_as_done", func(t *testing.T) {
		if !courseContains(courses, 0, "d1") {
			t.Errorf("d1 (blocked by in_progress d_blocker) should be in course 1: %v", courses)
		}
	})

	t.Run("removed_blocks_dependency_is_ignored", func(t *testing.T) {
		if !courseContains(courses, 0, "e1") {
			t.Errorf("e1 (blocked by g_blocker via removed=true) should be in course 1: %v", courses)
		}
	})

	t.Run("task_type_parent_waits_for_all_children_like_epic", func(t *testing.T) {
		if !courseContains(courses, 0, "f1") || !courseContains(courses, 0, "f2") {
			t.Errorf("f1/f2 (no blockers) should be in course 1: %v", courses)
		}
		if !courseContains(courses, 1, "task_parent1") {
			t.Errorf("task_parent1 (task type, unfinished children) should be in course 2, matching lm ready: %v", courses)
		}
	})
}

func TestComputeCoursesTransitiveGateWait(t *testing.T) {
	beads := map[string]Bead{
		"g1": {ID: "g1", BeadType: "gate", Status: "open"},
		"g2": {ID: "g2", BeadType: "gate", Status: "open"},
		"a":  {ID: "a", BeadType: "task", Status: "open"},
		"b":  {ID: "b", BeadType: "task", Status: "open"},
	}
	order := []string{"g1", "g2", "a", "b"}
	deps := []Dep{
		{BeadID: "a", DependsOnID: "g1", Type: "blocks"},
		{BeadID: "a", DependsOnID: "b", Type: "blocks"},
		{BeadID: "b", DependsOnID: "g2", Type: "blocks"},
	}
	courses, gateWaiting, unclassified := ComputeCourses(beads, order, deps, nil)
	if len(courses) != 0 || len(unclassified) != 0 {
		t.Errorf("courses = %v, unclassified = %v, want both empty", courses, unclassified)
	}
	want := map[string][]string{"a": {"g1", "g2"}, "b": {"g2"}}
	if len(gateWaiting) != len(want) {
		t.Fatalf("gateWaiting = %+v, want a and b", gateWaiting)
	}
	for _, gw := range gateWaiting {
		if !slices.Equal(gw.GateIDs, want[gw.BeadID]) {
			t.Errorf("%s GateIDs = %v, want %v", gw.BeadID, gw.GateIDs, want[gw.BeadID])
		}
		if gw.BeadID == "a" && (gw.Via["g1"] != "a" || gw.Via["g2"] != "b") {
			t.Errorf("a Via = %v, want g1 via a (direct) and g2 via b", gw.Via)
		}
	}
}

func TestComputeCoursesCycleIsUnclassified(t *testing.T) {
	beads := map[string]Bead{
		"x": {ID: "x", BeadType: "task", Status: "open"},
		"y": {ID: "y", BeadType: "task", Status: "open"},
		"z": {ID: "z", BeadType: "task", Status: "open"},
	}
	order := []string{"x", "y", "z"}
	deps := []Dep{
		{BeadID: "x", DependsOnID: "y", Type: "blocks"},
		{BeadID: "y", DependsOnID: "x", Type: "blocks"},
		{BeadID: "z", DependsOnID: "x", Type: "blocks"},
	}
	courses, gateWaiting, unclassified := ComputeCourses(beads, order, deps, nil)
	if len(courses) != 0 || len(gateWaiting) != 0 {
		t.Errorf("courses = %v, gateWaiting = %+v, want both empty", courses, gateWaiting)
	}
	if !slices.Equal(unclassified, order) {
		t.Errorf("unclassified = %v, want %v", unclassified, order)
	}
}

func TestFmtK(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{999.4, "999"},
		{999.6, "1.00k"},
		{1000, "1.00k"},
		{22500, "22.5k"},
		{212000, "212k"},
		{212500, "212k"},
		{213500, "214k"},
		{99960, "100k"},
		{999600, "1.00M"},
		{1801000, "1.80M"},
		{15068000, "15.1M"},
		{215438000, "215M"},
		{1078133000, "1.08G"},
		{2345678000000, "2346G"},
		{-1801000, "-1.80M"},
		{-500, "-500"},
	}
	for _, c := range cases {
		if got := FmtK(c.in); got != c.want {
			t.Errorf("FmtK(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

func mustParseDay(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse day %q: %v", s, err)
	}
	return tm
}

func TestAutoBudgetFewerThanThreeDaysIsNA(t *testing.T) {
	days := []DayTotal{
		{Date: "2026-09-08", Tokens: 100000},
		{Date: "2026-09-09", Tokens: 200000},
	}
	now := mustParseDay(t, "2026-09-10")
	if _, _, ok := AutoBudget(days, now); ok {
		t.Errorf("AutoBudget with 2 recorded days = ok, want n/a (fewer than MinRecordedDays)")
	}
}

func TestAutoBudgetFiveDaysMedian(t *testing.T) {
	days := []DayTotal{
		{Date: "2026-09-05", Tokens: 100000},
		{Date: "2026-09-06", Tokens: 200000},
		{Date: "2026-09-07", Tokens: 300000},
		{Date: "2026-09-08", Tokens: 400000},
		{Date: "2026-09-09", Tokens: 500000},
	}
	now := mustParseDay(t, "2026-09-10")
	budget, n, ok := AutoBudget(days, now)
	if !ok {
		t.Fatalf("AutoBudget with 5 recorded days = not ok, want ok")
	}
	if n != 5 {
		t.Errorf("AutoBudget n = %d, want 5", n)
	}
	if budget != 300000 {
		t.Errorf("AutoBudget budget = %v, want 300000 (median of 5 days)", budget)
	}
}

func TestAutoBudgetTenDaysUsesOnlyMostRecentSeven(t *testing.T) {
	var days []DayTotal
	for i := 1; i <= 10; i++ {
		days = append(days, DayTotal{Date: mustParseDay(t, "2026-09-01").AddDate(0, 0, i-1).Format("2006-01-02"), Tokens: i * 100000})
	}
	now := mustParseDay(t, "2026-09-11")
	budget, n, ok := AutoBudget(days, now)
	if !ok {
		t.Fatalf("AutoBudget with 10 recorded days = not ok, want ok")
	}
	if n != MaxRecordedDays {
		t.Errorf("AutoBudget n = %d, want %d (capped at MaxRecordedDays)", n, MaxRecordedDays)
	}
	if budget != 700000 {
		t.Errorf("AutoBudget budget = %v, want 700000 (median of the most recent 7 days)", budget)
	}
}

func TestAutoBudgetIgnoresDaysAtOrAfterNow(t *testing.T) {
	days := []DayTotal{
		{Date: "2026-09-05", Tokens: 100000},
		{Date: "2026-09-06", Tokens: 200000},
		{Date: "2026-09-07", Tokens: 300000},
		{Date: "2026-09-08", Tokens: 999999999},
	}
	now := mustParseDay(t, "2026-09-08")
	budget, n, ok := AutoBudget(days, now)
	if !ok {
		t.Fatalf("AutoBudget = not ok, want ok using the 3 days before now")
	}
	if n != 3 {
		t.Errorf("AutoBudget n = %d, want 3 (the day at/after now is excluded)", n)
	}
	if budget != 200000 {
		t.Errorf("AutoBudget budget = %v, want 200000 (median of the 3 days before now)", budget)
	}
}

func fixtureDepthActuals() []DepthActual {
	return []DepthActual{
		{BeadID: "c1", Depth: 1, Tokens: 10000},
		{BeadID: "c2", Depth: 1, Tokens: 10000},
		{BeadID: "c3", Depth: 1, Tokens: 10000},
		{BeadID: "out-high", Depth: 1, Tokens: 25000},
		{BeadID: "out-low", Depth: 1, Tokens: 4000},
		{BeadID: "in-range", Depth: 1, Tokens: 8000},
		{BeadID: "d2-a", Depth: 2, Tokens: 5000},
		{BeadID: "d2-b", Depth: 2, Tokens: 50000},
	}
}

func TestClosedDepthActualsFiltersStatusDepthAndZeroTokens(t *testing.T) {
	beads := map[string]Bead{
		"closed-with-depth":  {ID: "closed-with-depth", Status: "closed", Depth: 3},
		"closed-no-depth":    {ID: "closed-no-depth", Status: "closed", Depth: 0},
		"open-with-depth":    {ID: "open-with-depth", Status: "open", Depth: 3},
		"cancelled-w-depth":  {ID: "cancelled-w-depth", Status: "cancelled", Depth: 3},
		"closed-zero-tokens": {ID: "closed-zero-tokens", Status: "closed", Depth: 3},
	}
	tokenByBeadID := map[string]int{
		"closed-with-depth":  15000,
		"closed-no-depth":    15000,
		"open-with-depth":    15000,
		"cancelled-w-depth":  15000,
		"closed-zero-tokens": 0,
	}
	got := ClosedDepthActuals(beads, tokenByBeadID)
	if len(got) != 1 || got[0].BeadID != "closed-with-depth" {
		t.Errorf("ClosedDepthActuals = %+v, want only closed-with-depth (closed status, depth set, tokens>=1)", got)
	}
	if got[0].Depth != 3 || got[0].Tokens != 15000 {
		t.Errorf("ClosedDepthActuals[0] = %+v, want Depth=3 Tokens=15000", got[0])
	}
}

func TestRenderDepthCalibrationRowsAndAllTotal(t *testing.T) {
	actuals := fixtureDepthActuals()
	byDepth, all := BuildDepthGroups(actuals)
	lines := RenderDepthCalibration(byDepth, all)

	if lines[0] != "## Depth calibration" {
		t.Fatalf("RenderDepthCalibration[0] = %q, want the heading", lines[0])
	}
	found1, foundAll := false, false
	for _, l := range lines {
		if strings.HasPrefix(l, "| 1 | 6 | ") {
			found1 = true
		}
		if strings.HasPrefix(l, "| (all) | 8 | ") {
			foundAll = true
		}
	}
	if !found1 {
		t.Errorf("depth calibration lines = %v, want a depth=1 row with n=6", lines)
	}
	if !foundAll {
		t.Errorf("depth calibration lines = %v, want an (all) row with n=8", lines)
	}
}

func TestRenderDepthCalibrationEmptyStillRendersHeader(t *testing.T) {
	lines := RenderDepthCalibration(map[int][]int{}, nil)
	if len(lines) == 0 {
		t.Fatal("RenderDepthCalibration(empty) = nil, want the unconditional header (matching RenderCalibration's behavior)")
	}
	if lines[0] != "## Depth calibration" {
		t.Errorf("RenderDepthCalibration(empty)[0] = %q, want the heading", lines[0])
	}
	for _, l := range lines[2:] {
		if strings.HasPrefix(l, "| (all)") {
			t.Errorf("RenderDepthCalibration(empty) has an (all) row %q, want none (all is empty)", l)
		}
	}
}

func TestDepthOutliersMinSamplesAndThresholds(t *testing.T) {
	actuals := fixtureDepthActuals()
	byDepth, _ := BuildDepthGroups(actuals)
	outliers := DepthOutliers(actuals, byDepth)

	ids := map[string]DepthOutlier{}
	for _, o := range outliers {
		ids[o.BeadID] = o
	}
	if _, ok := ids["out-high"]; !ok {
		t.Errorf("outliers = %+v, want out-high present (ratio 2.5)", outliers)
	}
	if _, ok := ids["out-low"]; !ok {
		t.Errorf("outliers = %+v, want out-low present (ratio 0.4)", outliers)
	}
	if _, ok := ids["in-range"]; ok {
		t.Errorf("outliers = %+v, want in-range absent (ratio 0.8, within bounds)", outliers)
	}
	if _, ok := ids["d2-a"]; ok {
		t.Errorf("outliers = %+v, want d2-a absent (depth=2 cohort has n=2 < MinSamples, never judged)", outliers)
	}
	if _, ok := ids["d2-b"]; ok {
		t.Errorf("outliers = %+v, want d2-b absent (depth=2 cohort has n=2 < MinSamples, never judged)", outliers)
	}
	if len(outliers) != 2 {
		t.Errorf("len(outliers) = %d, want 2 (out-high, out-low)", len(outliers))
	}

	if outliers[0].BeadID != "out-high" || outliers[1].BeadID != "out-low" {
		t.Errorf("outliers order = %v, want [out-high, out-low] (descending ratio)", outliers)
	}
	if got := outliers[0].Ratio; got < 2.49 || got > 2.51 {
		t.Errorf("out-high ratio = %v, want ~2.5", got)
	}
}

func TestDepthOutliersBoundaryRatiosAreInclusive(t *testing.T) {
	cohort := []DepthActual{
		{BeadID: "m1", Depth: 5, Tokens: 10000},
		{BeadID: "m2", Depth: 5, Tokens: 10000},
		{BeadID: "m3", Depth: 5, Tokens: 10000},
	}
	actuals := append(append([]DepthActual{}, cohort...),
		DepthActual{BeadID: "boundary-high", Depth: 5, Tokens: 20000},
		DepthActual{BeadID: "boundary-low", Depth: 5, Tokens: 5000},
		DepthActual{BeadID: "just-inside-high", Depth: 5, Tokens: 19999},
		DepthActual{BeadID: "just-inside-low", Depth: 5, Tokens: 5001},
	)
	byDepth, _ := BuildDepthGroups(actuals)
	outliers := DepthOutliers(actuals, byDepth)

	ids := map[string]bool{}
	for _, o := range outliers {
		ids[o.BeadID] = true
	}
	if !ids["boundary-high"] {
		t.Errorf("outliers = %+v, want boundary-high present (ratio == 2.0 is inclusive)", outliers)
	}
	if !ids["boundary-low"] {
		t.Errorf("outliers = %+v, want boundary-low present (ratio == 0.5 is inclusive)", outliers)
	}
	if ids["just-inside-high"] || ids["just-inside-low"] {
		t.Errorf("outliers = %+v, want just-inside-high/low absent (strictly within bounds)", outliers)
	}
}

func TestDepthOutliersTieBreaksByBeadIDAscending(t *testing.T) {
	cohort := []DepthActual{
		{BeadID: "m1", Depth: 4, Tokens: 10000},
		{BeadID: "m2", Depth: 4, Tokens: 10000},
		{BeadID: "m3", Depth: 4, Tokens: 10000},
	}
	actuals := append(append([]DepthActual{}, cohort...),
		DepthActual{BeadID: "zzz", Depth: 4, Tokens: 30000},
		DepthActual{BeadID: "aaa", Depth: 4, Tokens: 30000},
	)
	byDepth, _ := BuildDepthGroups(actuals)
	outliers := DepthOutliers(actuals, byDepth)
	if len(outliers) != 2 || outliers[0].BeadID != "aaa" || outliers[1].BeadID != "zzz" {
		t.Errorf("outliers = %v, want [aaa, zzz] (same ratio -> ascending BeadID tie-break)", outliers)
	}
}

func TestRenderDepthOutliersEmptyReturnsNil(t *testing.T) {
	if got := RenderDepthOutliers(nil); got != nil {
		t.Errorf("RenderDepthOutliers(nil) = %v, want nil (D2: 該当 0 件なら節ごと出さない)", got)
	}
}

func TestRenderDepthOutliersFormatsRatioToTwoDecimals(t *testing.T) {
	lines := RenderDepthOutliers([]DepthOutlier{
		{BeadID: "x1", Depth: 2, Tokens: 7000, CohortMedian: 3000, Ratio: 2.3333333},
	})
	want := "| x1 | 2 | 7.00k | 3.00k | 2.33 |"
	found := false
	for _, l := range lines {
		if l == want {
			found = true
		}
	}
	if !found {
		t.Errorf("RenderDepthOutliers lines = %v, want a line %q", lines, want)
	}
}

func TestRenderDepthOutliersUsesDisplayWhenSet(t *testing.T) {
	lines := RenderDepthOutliers([]DepthOutlier{
		{BeadID: "5nb1276gd79t20cka5wagm4da0", Display: "lm-5nb", Depth: 2, Tokens: 7000, CohortMedian: 3000, Ratio: 2.33},
	})
	if !strings.Contains(strings.Join(lines, "\n"), "| lm-5nb | 2 |") {
		t.Errorf("RenderDepthOutliers lines = %v, want the Display value lm-5nb in the bead column, not the canonical id", lines)
	}
	if strings.Contains(strings.Join(lines, "\n"), "5nb1276gd79t20cka5wagm4da0") {
		t.Errorf("RenderDepthOutliers lines = %v, must not leak the canonical id when Display is set", lines)
	}
}

func TestRenderDepthOutliersFallsBackToBeadIDWhenDisplayEmpty(t *testing.T) {
	lines := RenderDepthOutliers([]DepthOutlier{
		{BeadID: "x1", Depth: 2, Tokens: 7000, CohortMedian: 3000, Ratio: 2.33},
	})
	if !strings.Contains(strings.Join(lines, "\n"), "| x1 | 2 |") {
		t.Errorf("RenderDepthOutliers lines = %v, want BeadID x1 in the bead column when Display is empty", lines)
	}
}

func TestRenderScopeGrowthFiltersZeroSortsAndFormats(t *testing.T) {
	rows := []ScopeGrowth{
		{BeadID: "zero", ClaimedAt: "2026-01-01T00:00:00Z", DepsAfterClaim: 0, ChildrenAfterClaim: 0},
		{BeadID: "low", ClaimedAt: "2026-01-02T00:00:00Z", DepsAfterClaim: 1, ChildrenAfterClaim: 0},
		{BeadID: "high", ClaimedAt: "2026-01-03T00:00:00Z", DepsAfterClaim: 2, ChildrenAfterClaim: 1},
	}
	lines := RenderScopeGrowth(rows)
	if lines == nil {
		t.Fatal("RenderScopeGrowth = nil, want a rendered table (2 rows have non-zero totals)")
	}
	if lines[0] != "## Scope growth" {
		t.Fatalf("RenderScopeGrowth[0] = %q, want the heading", lines[0])
	}
	body := strings.Join(lines, "\n")
	if strings.Contains(body, "zero") {
		t.Errorf("RenderScopeGrowth output contains the zero-total row: %v", lines)
	}
	highIdx := indexOfLine(lines, "| high | 2026-01-03T00:00:00Z | 2 | 1 |")
	lowIdx := indexOfLine(lines, "| low | 2026-01-02T00:00:00Z | 1 | 0 |")
	if highIdx < 0 || lowIdx < 0 {
		t.Fatalf("RenderScopeGrowth lines = %v, want both high and low rows formatted exactly", lines)
	}
	if highIdx > lowIdx {
		t.Errorf("RenderScopeGrowth order = %v, want high (total 3) before low (total 1)", lines)
	}
}

func TestRenderScopeGrowthAllZeroReturnsNil(t *testing.T) {
	rows := []ScopeGrowth{
		{BeadID: "a", DepsAfterClaim: 0, ChildrenAfterClaim: 0},
		{BeadID: "b", DepsAfterClaim: 0, ChildrenAfterClaim: 0},
	}
	if got := RenderScopeGrowth(rows); got != nil {
		t.Errorf("RenderScopeGrowth(all zero) = %v, want nil (D3: 全件 0 なら節ごと出さない)", got)
	}
}

func TestRenderScopeGrowthTieBreaksByBeadIDAscending(t *testing.T) {
	rows := []ScopeGrowth{
		{BeadID: "zzz", DepsAfterClaim: 1, ChildrenAfterClaim: 0},
		{BeadID: "aaa", DepsAfterClaim: 1, ChildrenAfterClaim: 0},
	}
	lines := RenderScopeGrowth(rows)
	aaaIdx := -1
	zzzIdx := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "| aaa ") {
			aaaIdx = i
		}
		if strings.HasPrefix(l, "| zzz ") {
			zzzIdx = i
		}
	}
	if aaaIdx < 0 || zzzIdx < 0 || aaaIdx > zzzIdx {
		t.Errorf("RenderScopeGrowth lines = %v, want aaa before zzz (same total -> ascending BeadID tie-break)", lines)
	}
}

func TestRenderScopeGrowthUsesDisplayWhenSet(t *testing.T) {
	lines := RenderScopeGrowth([]ScopeGrowth{
		{BeadID: "dv55nqpbrrfcjcm8kp3k7k8br0", Display: "lm-dv5", ClaimedAt: "2026-01-01T00:00:00Z", DepsAfterClaim: 1, ChildrenAfterClaim: 1},
	})
	body := strings.Join(lines, "\n")
	if !strings.Contains(body, "| lm-dv5 | 2026-01-01T00:00:00Z | 1 | 1 |") {
		t.Errorf("RenderScopeGrowth lines = %v, want the Display value lm-dv5 in the bead column, not the canonical id", lines)
	}
	if strings.Contains(body, "dv55nqpbrrfcjcm8kp3k7k8br0") {
		t.Errorf("RenderScopeGrowth lines = %v, must not leak the canonical id when Display is set", lines)
	}
}

func TestRenderReworkRateGroupsByDepthAndAllTotal(t *testing.T) {
	rows := []ReworkActual{
		{BeadID: "d1-a", Depth: 1, Reworks: 0},
		{BeadID: "d1-b", Depth: 1, Reworks: 2},
		{BeadID: "d1-c", Depth: 1, Reworks: 1},
		{BeadID: "d2-a", Depth: 2, Reworks: 0},
	}
	lines := RenderReworkRate(rows)

	if lines[0] != "## Rework" {
		t.Fatalf("RenderReworkRate[0] = %q, want the heading", lines[0])
	}
	if got, want := indexOfLine(lines, "| 1 | 3 | 2 | 67% | 3 |"), true; (got >= 0) != want {
		t.Errorf("RenderReworkRate lines = %v, want the depth=1 row \"| 1 | 3 | 2 | 67%% | 3 |\"", lines)
	}
	if indexOfLine(lines, "| 2 | 1 | 0 | 0% | 0 |") < 0 {
		t.Errorf("RenderReworkRate lines = %v, want the depth=2 row \"| 2 | 1 | 0 | 0%% | 0 |\"", lines)
	}
	if indexOfLine(lines, "| (all) | 4 | 2 | 50% | 3 |") < 0 {
		t.Errorf("RenderReworkRate lines = %v, want the (all) row \"| (all) | 4 | 2 | 50%% | 3 |\"", lines)
	}
}

func TestRenderReworkRateDepthZeroSortsLastAsDash(t *testing.T) {
	rows := []ReworkActual{
		{BeadID: "d0", Depth: 0, Reworks: 0},
		{BeadID: "d2", Depth: 2, Reworks: 0},
		{BeadID: "d1", Depth: 1, Reworks: 0},
	}
	lines := RenderReworkRate(rows)

	idx1 := indexOfLine(lines, "| 1 | 1 | 0 | 0% | 0 |")
	idx2 := indexOfLine(lines, "| 2 | 1 | 0 | 0% | 0 |")
	idxDash := indexOfLine(lines, "| - | 1 | 0 | 0% | 0 |")
	if idx1 < 0 || idx2 < 0 || idxDash < 0 {
		t.Fatalf("RenderReworkRate lines = %v, want depth 1, 2 and the Depth==0 \"-\" row", lines)
	}
	if idx1 >= idx2 || idx2 >= idxDash {
		t.Errorf("RenderReworkRate order = %v, want depth ascending (1, 2) with Depth==0 (\"-\") last", lines)
	}
}

func TestRenderReworkRateEmptyStillRendersHeader(t *testing.T) {
	lines := RenderReworkRate(nil)
	if len(lines) == 0 {
		t.Fatal("RenderReworkRate(empty) = nil, want the unconditional header (matching RenderDepthCalibration's behavior)")
	}
	if lines[0] != "## Rework" {
		t.Errorf("RenderReworkRate(empty)[0] = %q, want the heading", lines[0])
	}
	for _, l := range lines[2:] {
		if strings.HasPrefix(l, "| (all)") {
			t.Errorf("RenderReworkRate(empty) has an (all) row %q, want none (population is empty)", l)
		}
	}
}

func TestRenderReworkRateRoundsToNearestPercent(t *testing.T) {
	lines := RenderReworkRate([]ReworkActual{
		{BeadID: "a", Depth: 5, Reworks: 1},
		{BeadID: "b", Depth: 5, Reworks: 0},
		{BeadID: "c", Depth: 5, Reworks: 0},
	})
	if indexOfLine(lines, "| 5 | 3 | 1 | 33% | 1 |") < 0 {
		t.Errorf("RenderReworkRate lines = %v, want the rounded rate row \"| 5 | 3 | 1 | 33%% | 1 |\"", lines)
	}
}

func TestPerBeadActual(t *testing.T) {
	reset := time.Date(2026, 9, 21, 2, 0, 0, 0, time.UTC)
	before := reset.Add(-time.Minute)
	entries := []CostEntry{
		{BeadID: "a", RecordedAt: reset, Tokens: 2_000_000},
		{BeadID: "a", RecordedAt: reset.Add(time.Hour), Tokens: 1_400_000},
		{BeadID: "a", RecordedAt: before, Tokens: 9_000_000},
		{BeadID: "b", RecordedAt: reset.Add(2 * time.Hour), Tokens: 10_400_000},
		{BeadID: "c", RecordedAt: reset.Add(3 * time.Hour), Tokens: 1_200_000},
		{BeadID: "d", RecordedAt: before, Tokens: 50_000_000},
	}
	got, n, ok := PerBeadActual(entries, reset)
	if !ok || n != 3 || got != 3_400_000 {
		t.Fatalf("PerBeadActual = (%v, %d, %v), want (3400000, 3, true)", got, n, ok)
	}

	got, n, ok = PerBeadActual(entries[:4], reset)
	if !ok || n != 2 || got != 6_900_000 {
		t.Fatalf("PerBeadActual even = (%v, %d, %v), want (6900000, 2, true)", got, n, ok)
	}

	if _, n, ok := PerBeadActual(entries, reset.Add(24*time.Hour)); ok || n != 0 {
		t.Fatalf("PerBeadActual empty window = (%d, %v), want (0, false)", n, ok)
	}
}

func TestReleaseETA(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

	got := ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 100}})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].EarlyDiverges || got[0].LateDiverges || got[0].Skip {
		t.Fatalf("got[0] = %+v, want neither diverging nor skipped", got[0])
	}
	wantEarly := now.Add(50 * time.Hour)
	wantLate := now.Add(100 * time.Hour)
	if !got[0].Early.Equal(wantEarly) || !got[0].Late.Equal(wantLate) {
		t.Fatalf("got[0] = {Early: %v, Late: %v}, want {Early: %v, Late: %v}", got[0].Early, got[0].Late, wantEarly, wantLate)
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 100}, {Remaining: 50}})
	if got[1].EarlyDiverges || got[1].LateDiverges || got[1].Skip {
		t.Fatalf("got[1] = %+v, want neither diverging nor skipped", got[1])
	}
	if want := now.Add(75 * time.Hour); !got[1].Early.Equal(want) {
		t.Fatalf("got[1].Early = %v, want %v", got[1].Early, want)
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 10, Created: 144}})
	if !got[0].EarlyDiverges || !got[0].LateDiverges {
		t.Fatalf("got[0] = %+v, want both ends diverging", got[0])
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 0}})
	if !got[0].Skip {
		t.Fatalf("got[0].Skip = %v, want true", got[0].Skip)
	}
}

func TestReleaseETADays(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

	got := ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 96}, {Remaining: 48}})
	if got[0].ClosePerDayHi != 48 || got[0].ClosePerDayLo != 24 {
		t.Fatalf("close/day = %v〜%v, want 24〜48", got[0].ClosePerDayLo, got[0].ClosePerDayHi)
	}
	if got[0].EarlyDays != 2 || got[0].LateDays != 4 || got[0].EarlyDaysNone || got[0].LateDaysNone {
		t.Fatalf("got[0] days = %+v, want 2〜4", got[0])
	}
	if got[1].EarlyDays != 3 || got[1].LateDays != 6 {
		t.Fatalf("got[1] days = %v〜%v, want 3〜6", got[1].EarlyDays, got[1].LateDays)
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 96, Created: 500}})
	if !got[0].EarlyDiverges || got[0].EarlyDays != 2 {
		t.Fatalf("got[0] = %+v, want diverging ETA but EarlyDays=2", got[0])
	}

	got = ReleaseETA(now, 72, 0, 0, []ReleaseWindow{{Remaining: 10}})
	if !got[0].EarlyDaysNone || !got[0].LateDaysNone || got[0].EarlyDays != 0 {
		t.Fatalf("got[0] = %+v, want both 見込み無し", got[0])
	}
	got = ReleaseETA(now, 72, 144, 0, []ReleaseWindow{{Remaining: 96}})
	if got[0].EarlyDaysNone || !got[0].LateDaysNone {
		t.Fatalf("got[0] = %+v, want only late 見込み無し", got[0])
	}
}

func TestReleaseETAFloor(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	floor := now.Add(1000 * time.Hour)

	got := ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 100, Floor: floor}})
	if !got[0].Early.Equal(floor) || !got[0].Late.Equal(floor) {
		t.Fatalf("got[0] = {Early: %v, Late: %v}, want both raised to floor %v", got[0].Early, got[0].Late, floor)
	}

	pastFloor := now.Add(1 * time.Hour)
	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 100, Floor: pastFloor}})
	wantEarly, wantLate := now.Add(50*time.Hour), now.Add(100*time.Hour)
	if !got[0].Early.Equal(wantEarly) || !got[0].Late.Equal(wantLate) {
		t.Fatalf("got[0] = {Early: %v, Late: %v}, want unraised {Early: %v, Late: %v}", got[0].Early, got[0].Late, wantEarly, wantLate)
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 10, Created: 144, Floor: floor}})
	if !got[0].EarlyDiverges || !got[0].LateDiverges {
		t.Fatalf("got[0] = %+v, want both ends still diverging", got[0])
	}

	got = ReleaseETA(now, 72, 144, 72, []ReleaseWindow{{Remaining: 10, Floor: floor}, {Remaining: 50}})
	if !got[0].Early.Equal(floor) {
		t.Fatalf("got[0].Early = %v, want raised to floor %v", got[0].Early, floor)
	}
	if want := now.Add(30 * time.Hour); !got[1].Early.Equal(want) {
		t.Fatalf("got[1].Early = %v, want unfloored %v (floor must not propagate)", got[1].Early, want)
	}
}

func indexOfLine(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	return -1
}
