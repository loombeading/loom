// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestEstimateInputReturnsWholeGraphRegardlessOfReadyFilterNamespace(t *testing.T) {
	db := testDB(t)
	x := mustCreate(t, db, CreateInput{Title: "x-task", Priority: 2, BeadType: "task", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	y := mustCreate(t, db, CreateInput{Title: "y-task", Priority: 2, BeadType: "task", Namespace: "y", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})

	beads, order, _, _, readyIDs, err := EstimateInput(context.Background(), db, ReadyFilter{Namespace: "x", Priority: -1})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := beads[x]; !ok {
		t.Errorf("beads = %v, want the namespace-x Bead %q present", beads, x)
	}
	if _, ok := beads[y]; !ok {
		t.Errorf("beads = %v, want the namespace-y Bead %q present too (EstimateInput returns the whole graph, not a namespace subset)", beads, y)
	}
	if !containsID(order, x) || !containsID(order, y) {
		t.Errorf("order = %v, want both %q and %q", order, x, y)
	}

	if got := beads[x].Namespace; got != "x" {
		t.Errorf("beads[x].Namespace = %q, want %q", got, "x")
	}
	if got := beads[y].Namespace; got != "y" {
		t.Errorf("beads[y].Namespace = %q, want %q", got, "y")
	}

	if !containsID(readyIDs, x) {
		t.Errorf("readyIDs = %v, want the namespace-x Bead %q (ReadyFilter.Namespace scopes readyIDs)", readyIDs, x)
	}
	if containsID(readyIDs, y) {
		t.Errorf("readyIDs = %v, must not include the namespace-y Bead %q (ReadyFilter.Namespace scopes readyIDs)", readyIDs, y)
	}
}

func TestEstimateInputCarriesDepthOrZeroWhenUnset(t *testing.T) {
	db := testDB(t)
	withDepth := mustCreate(t, db, CreateInput{Title: "with-depth", Priority: 2, BeadType: "task", Namespace: "x", Depth: 3, Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	withoutDepth := mustCreate(t, db, CreateInput{Title: "without-depth", Priority: 2, BeadType: "task", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})

	beads, order, deps, _, _, err := EstimateInput(context.Background(), db, ReadyFilter{Namespace: "x", Priority: -1})
	_, _ = order, deps
	if err != nil {
		t.Fatal(err)
	}

	if got := beads[withDepth].Depth; got != 3 {
		t.Errorf("beads[withDepth].Depth = %d, want 3", got)
	}
	if got := beads[withoutDepth].Depth; got != 0 {
		t.Errorf("beads[withoutDepth].Depth = %d, want 0 (unset)", got)
	}
}

func TestEstimateInputQueryErrors(t *testing.T) {
	db := testDB(t)
	x := mustCreate(t, db, CreateInput{Title: "x-task", Priority: 2, BeadType: "task", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	y := mustCreate(t, db, CreateInput{Title: "y-task", Priority: 2, BeadType: "task", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	mustLink(t, db, x, y, BlocksDepType)
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: x, TokensIn: 1, TokensOut: 0, Actor: "u", Now: "2026-01-01T00:00:00Z"}))
	for failOn := 1; failOn <= 4; failOn++ {
		q := &nthCallErrQuerier{Querier: db, failOn: failOn}
		if _, _, _, _, _, err := EstimateInput(context.Background(), q, ReadyFilter{Priority: -1}); !errors.Is(err, errBoom) {
			t.Errorf("EstimateInput failOn=%d: err = %v, want errBoom", failOn, err)
		}
	}
}

func TestEstimateInputScanErrors(t *testing.T) {
	db := testDB(t)
	for match, goodRow := range map[string]string{
		"FROM beads":        `SELECT 'a', 't', 'task', 2, 'open', 'x', NULL`,
		"FROM dependencies": `SELECT 'a', 'b', 'blocks'`,
		"FROM token_costs":  `SELECT 'a', 1, 2`,
	} {
		for _, sub := range []string{`SELECT 'a', 'b', 'c', 'x', 'y', 'z', 'w', 'v'`, goodRow + ` UNION ALL ` + strings.Replace(goodRow, "'a'", "abs(-9223372036854775808)", 1)} {
			q := substituteQuerier{DB: db, match: match, sub: sub}
			if _, _, _, _, _, err := EstimateInput(context.Background(), q, ReadyFilter{Priority: -1}); err == nil {
				t.Errorf("EstimateInput with %q answered by %q: err = nil, want error", match, sub)
			}
		}
	}
}

func TestDailyTokenTotalsEmptyNamespaceReturnsAllRecords(t *testing.T) {
	db := testDB(t)
	x := mustCreate(t, db, CreateInput{Title: "x-task", Priority: 2, BeadType: "task", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	y := mustCreate(t, db, CreateInput{Title: "y-task", Priority: 2, BeadType: "task", Namespace: "y", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: x, TokensIn: 100, TokensOut: 0, Actor: "u", Now: "2026-01-01T00:00:00Z"}))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: y, TokensIn: 200, TokensOut: 0, Actor: "u", Now: "2026-01-02T00:00:00Z"}))

	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	totals, err := DailyTokenTotals(context.Background(), db, now, "")
	if err != nil {
		t.Fatal(err)
	}

	sum := 0
	for _, dt := range totals {
		sum += dt.Tokens
	}
	if len(totals) != 2 || sum != 300 {
		t.Errorf("DailyTokenTotals(namespace=\"\") = %v, want 2 days summing to 300 (both namespaces included)", totals)
	}
}

func TestTokenCostEntriesErrors(t *testing.T) {
	db := testDB(t)
	x := mustCreate(t, db, CreateInput{Title: "x-task", Priority: 2, BeadType: "task", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: x, TokensIn: 100, TokensOut: 0, Actor: "u", Now: "not-a-time"}))
	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	if _, err := DailyTokenTotals(context.Background(), db, now, "x"); err == nil {
		t.Error("DailyTokenTotals with unparseable recorded_at: err = nil, want parse error")
	}
	_ = db.Close()
	if _, err := TokenCostEntries(context.Background(), db, ""); err == nil {
		t.Error("TokenCostEntries on closed DB: err = nil, want query error")
	}
}

func TestDailyTokenTotalsFiltersByNamespace(t *testing.T) {
	db := testDB(t)
	x := mustCreate(t, db, CreateInput{Title: "x-task", Priority: 2, BeadType: "task", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	y := mustCreate(t, db, CreateInput{Title: "y-task", Priority: 2, BeadType: "task", Namespace: "y", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: x, TokensIn: 100, TokensOut: 0, Actor: "u", Now: "2026-01-01T00:00:00Z"}))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: y, TokensIn: 200, TokensOut: 0, Actor: "u", Now: "2026-01-02T00:00:00Z"}))

	now := mustParseTime(t, "2026-01-10T00:00:00Z")
	totals, err := DailyTokenTotals(context.Background(), db, now, "x")
	if err != nil {
		t.Fatal(err)
	}

	sum := 0
	for _, dt := range totals {
		sum += dt.Tokens
	}
	if len(totals) != 1 || sum != 100 {
		t.Errorf("DailyTokenTotals(namespace=\"x\") = %v, want exactly the 1 day / 100 tokens recorded against the namespace-x Bead (namespace-y record must be excluded)", totals)
	}

	totalsY, err := DailyTokenTotals(context.Background(), db, now, "y")
	if err != nil {
		t.Fatal(err)
	}
	sumY := 0
	for _, dt := range totalsY {
		sumY += dt.Tokens
	}
	if len(totalsY) != 1 || sumY != 200 {
		t.Errorf("DailyTokenTotals(namespace=\"y\") = %v, want exactly the 1 day / 200 tokens recorded against the namespace-y Bead", totalsY)
	}
}

func TestThroughputInput(t *testing.T) {
	db := testDB(t)
	closedWithPR := mustCreate(t, db, CreateInput{Title: "closed-with-pr", Priority: 2, BeadType: "task", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	if err := mustUpdate(t, db, UpdateInput{ID: closedWithPR, Priority: -1, AddExternalRefs: []string{"https://github.com/o/r/pull/1"}, Actor: "u", Now: "2026-01-01T01:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	if err := mustCloseBead(t, db, closedWithPR, "2026-01-02T00:00:00.000Z"); err != nil {
		t.Fatal(err)
	}
	closedNoPR := mustCreate(t, db, CreateInput{Title: "closed-no-pr", Priority: 2, BeadType: "task", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	if err := mustCloseBead(t, db, closedNoPR, "2026-01-02T00:00:00.000Z"); err != nil {
		t.Fatal(err)
	}
	open := mustCreate(t, db, CreateInput{Title: "open", Priority: 2, BeadType: "task", Namespace: "y", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})
	gate := mustCreate(t, db, CreateInput{Title: "gate", Priority: 2, BeadType: "gate", Namespace: "x", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})

	all, err := ThroughputInput(context.Background(), db, "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ThroughputBead{}
	for _, tb := range all {
		byID[tb.ID] = tb
	}
	if _, ok := byID[gate]; ok {
		t.Errorf("ThroughputInput included the Gate Bead %s, want excluded", gate)
	}
	if tb, ok := byID[closedWithPR]; !ok || !tb.Closed || !tb.HasPR {
		t.Errorf("ThroughputInput[closedWithPR] = %+v, ok=%v, want Closed=true HasPR=true", tb, ok)
	}
	if tb, ok := byID[closedNoPR]; !ok || !tb.Closed || tb.HasPR {
		t.Errorf("ThroughputInput[closedNoPR] = %+v, ok=%v, want Closed=true HasPR=false", tb, ok)
	}
	if tb, ok := byID[open]; !ok || tb.Closed {
		t.Errorf("ThroughputInput[open] = %+v, ok=%v, want Closed=false", tb, ok)
	}
	wantCreatedAt := mustParseTime(t, "2026-01-01T00:00:00.000Z")
	if !byID[closedWithPR].CreatedAt.Equal(wantCreatedAt) {
		t.Errorf("ThroughputInput[closedWithPR].CreatedAt = %v, want %v", byID[closedWithPR].CreatedAt, wantCreatedAt)
	}
	wantClosedAt := mustParseTime(t, "2026-01-02T00:00:00.000Z")
	if !byID[closedWithPR].ClosedAt.Equal(wantClosedAt) {
		t.Errorf("ThroughputInput[closedWithPR].ClosedAt = %v, want %v", byID[closedWithPR].ClosedAt, wantClosedAt)
	}

	scopedX, err := ThroughputInput(context.Background(), db, "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(scopedX) != 2 {
		t.Errorf("ThroughputInput(namespace=\"x\") returned %d Beads, want 2 (open Bead in namespace y excluded)", len(scopedX))
	}
}
