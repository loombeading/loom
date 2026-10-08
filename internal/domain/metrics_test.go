// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type reworkQueryOverride struct {
	*sql.DB

	forceQueryErr error
	forceRowsSQL  string
}

func (q reworkQueryOverride) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "field = ? AND bead_id IN") {
		if q.forceQueryErr != nil {
			return nil, q.forceQueryErr
		}
		if q.forceRowsSQL != "" {
			return q.DB.QueryContext(ctx, q.forceRowsSQL)
		}
	}
	return q.DB.QueryContext(ctx, query, args...)
}

func mustCloseBead(t *testing.T, db *sql.DB, id, now string) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = CloseBead(context.Background(), tx, CloseInput{ID: id, Actor: "u", Now: now})
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func mustRemoveLink(t *testing.T, db *sql.DB, beadID, dependsOnID, depType string) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = RemoveLink(context.Background(), tx, "u", beadID, dependsOnID, depType, "n", "")
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func TestDerivedMetricsEmptyIDsReturnsEmptyMap(t *testing.T) {
	db := testDB(t)
	got, err := DerivedMetrics(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, want empty map", got)
	}
}

func TestDerivedMetricsBlastRadiusCountsDirectOpenBlockers(t *testing.T) {
	db := testDB(t)
	target := mustCreate(t, db, CreateInput{Title: "target", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	blockedOpen1 := mustCreate(t, db, CreateInput{Title: "b1", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n2"})
	blockedOpen2 := mustCreate(t, db, CreateInput{Title: "b2", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n3"})
	blockedClosed := mustCreate(t, db, CreateInput{Title: "b3", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n4"})
	blockedRemovedLink := mustCreate(t, db, CreateInput{Title: "b4", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n5"})

	mustNoErr(t, mustAddLink(t, db, blockedOpen1, target, BlocksDepType))
	mustNoErr(t, mustAddLink(t, db, blockedOpen2, target, BlocksDepType))
	mustNoErr(t, mustAddLink(t, db, blockedClosed, target, BlocksDepType))
	mustNoErr(t, mustAddLink(t, db, blockedRemovedLink, target, BlocksDepType))

	mustNoErr(t, mustCloseBead(t, db, blockedClosed, "n10"))
	mustNoErr(t, mustRemoveLink(t, db, blockedRemovedLink, target, BlocksDepType))

	got, err := DerivedMetrics(context.Background(), db, []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if got[target].BlastRadius != 2 {
		t.Errorf("BlastRadius = %d, want 2 (got %+v)", got[target].BlastRadius, got[target])
	}
}

func TestDerivedMetricsRetryCountCountsReleaseAndForceRelease(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "n2"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Release: true, Actor: "alice", Now: "n3"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "bob", Now: "n4"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Release: true, Force: true, Actor: "carol", Now: "n5"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "dave", Now: "n6"}))

	got, err := DerivedMetrics(context.Background(), db, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if got[id].RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2 (got %+v)", got[id].RetryCount, got[id])
	}
}

func TestDerivedMetricsTokenCostSumsInAndOut(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: id, TokensIn: 100, TokensOut: 50, Actor: "alice", Now: "n2"}))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: id, TokensIn: 20, TokensOut: 5, Actor: "bob", Now: "n3"}))

	got, err := DerivedMetrics(context.Background(), db, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if got[id].TokenCost != 175 {
		t.Errorf("TokenCost = %d, want 175 (got %+v)", got[id].TokenCost, got[id])
	}
}

func TestDerivedMetricsReworkCountCountsFieldRework(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	if _, err := db.ExecContext(t.Context(), `INSERT INTO audit_log (occurred_at, actor, bead_id, kind, field, new_value, origin) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"n2", "u", id, "field", "rework", "ci", "local"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO audit_log (occurred_at, actor, bead_id, kind, field, new_value, origin) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"n3", "u", id, "field", "rework", "review", "local"); err != nil {
		t.Fatal(err)
	}

	got, err := DerivedMetrics(context.Background(), db, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if got[id].ReworkCount != 2 {
		t.Errorf("ReworkCount = %d, want 2 (got %+v)", got[id].ReworkCount, got[id])
	}
}

func TestDerivedMetricsReworkCountQueryErrorIsWrapped(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	q := reworkQueryOverride{DB: db, forceQueryErr: errors.New("forced")}
	_, err := DerivedMetrics(context.Background(), q, []string{id})
	if err == nil || !strings.Contains(err.Error(), "derived metrics: rework count:") {
		t.Fatalf("err = %v, want it to contain %q", err, "derived metrics: rework count:")
	}
}

func TestDerivedMetricsReworkCountScanErrorIsWrapped(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	q := reworkQueryOverride{DB: db, forceRowsSQL: "SELECT 'x'"}
	_, err := DerivedMetrics(context.Background(), q, []string{id})
	if err == nil || !strings.Contains(err.Error(), "derived metrics: rework count:") {
		t.Fatalf("err = %v, want it to contain %q", err, "derived metrics: rework count:")
	}
}

func TestDerivedMetricsZeroForIDWithNoData(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	got, err := DerivedMetrics(context.Background(), db, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if got[id] != (DerivedMetric{}) {
		t.Errorf("metric = %+v, want zero value", got[id])
	}
}

func TestDerivedMetricsBatchesMultipleIDsInOneCallEach(t *testing.T) {
	db := testDB(t)
	a := mustCreate(t, db, CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	b := mustCreate(t, db, CreateInput{Title: "b", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n2"})
	blockerOfA := mustCreate(t, db, CreateInput{Title: "ba", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n3"})
	mustNoErr(t, mustAddLink(t, db, blockerOfA, a, BlocksDepType))

	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: b, TokensIn: 1, TokensOut: 1, Actor: "u", Now: "n5"}))

	got, err := DerivedMetrics(context.Background(), db, []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got[a].BlastRadius != 1 {
		t.Errorf("a.BlastRadius = %d, want 1", got[a].BlastRadius)
	}
	if got[b].TokenCost != 2 {
		t.Errorf("b.TokenCost = %d, want 2", got[b].TokenCost)
	}
}

func mustAddLinkAt(t *testing.T, db *sql.DB, beadID, dependsOnID, depType, occurredAt string) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = AddLink(context.Background(), tx, "u", beadID, dependsOnID, depType, occurredAt, "")
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func TestDerivedMetricsScopeGrowthNeverClaimedHasEmptyClaimedAt(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	got, err := DerivedMetrics(context.Background(), db, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if got[id].ClaimedAt != "" {
		t.Errorf("ClaimedAt = %q, want \"\" (never claimed)", got[id].ClaimedAt)
	}
	if got[id].DepsAfterClaim != 0 || got[id].ChildrenAfterClaim != 0 {
		t.Errorf("metric = %+v, want DepsAfterClaim=0 ChildrenAfterClaim=0 for a never-claimed Bead", got[id])
	}
}

func TestDerivedMetricsScopeGrowthClaimedNoGrowthIsZero(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: id, Claim: true, Actor: "alice", Now: "n2"}))

	got, err := DerivedMetrics(context.Background(), db, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if got[id].ClaimedAt != "n2" {
		t.Errorf("ClaimedAt = %q, want %q", got[id].ClaimedAt, "n2")
	}
	if got[id].DepsAfterClaim != 0 || got[id].ChildrenAfterClaim != 0 {
		t.Errorf("metric = %+v, want DepsAfterClaim=0 ChildrenAfterClaim=0 (nothing grew after claim)", got[id])
	}
}

func TestDerivedMetricsScopeGrowthCountsOnlyDependencyAuditAfterClaim(t *testing.T) {
	db := testDB(t)
	target := mustCreate(t, db, CreateInput{Title: "target", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	before := mustCreate(t, db, CreateInput{Title: "before", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	after := mustCreate(t, db, CreateInput{Title: "after", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	mustNoErr(t, mustAddLinkAt(t, db, target, before, BlocksDepType, "n2"))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Force: true, Reason: "r", Actor: "alice", Now: "n3"}))
	mustNoErr(t, mustAddLinkAt(t, db, target, after, BlocksDepType, "n4"))

	got, err := DerivedMetrics(context.Background(), db, []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if got[target].DepsAfterClaim != 1 {
		t.Errorf("DepsAfterClaim = %d, want 1 (only the post-claim dependency add)", got[target].DepsAfterClaim)
	}
}

func TestDerivedMetricsScopeGrowthIgnoresRemovalsAndNonBlocksLinks(t *testing.T) {
	db := testDB(t)
	target := mustCreate(t, db, CreateInput{Title: "target", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	other := mustCreate(t, db, CreateInput{Title: "other", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	parent := mustCreate(t, db, CreateInput{Title: "parent", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	blocker := mustCreate(t, db, CreateInput{Title: "blocker", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	mustNoErr(t, mustAddLinkAt(t, db, target, blocker, BlocksDepType, "n2"))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Force: true, Reason: "r", Actor: "alice", Now: "n3"}))
	mustNoErr(t, mustAddLinkAt(t, db, target, other, DiscoveredFromDepType, "n4"))
	mustNoErr(t, mustAddLinkAt(t, db, target, other, DuplicatesDepType, "n4"))
	mustNoErr(t, mustAddLinkAt(t, db, target, parent, ParentChildDepType, "n4"))

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(context.Background(), tx, "u", target, blocker, BlocksDepType, "n5", ""); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	mustNoErr(t, tx.Commit())

	got, err := DerivedMetrics(context.Background(), db, []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if got[target].DepsAfterClaim != 0 {
		t.Errorf("DepsAfterClaim = %d, want 0 (removals and non-blocks links are not scope growth)", got[target].DepsAfterClaim)
	}
}

func TestDerivedMetricsScopeGrowthCountsOnlyChildrenCreatedAfterClaim(t *testing.T) {
	db := testDB(t)
	parent := mustCreate(t, db, CreateInput{Title: "parent", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	_ = mustCreate(t, db, CreateInput{Title: "child-before", Priority: 2, BeadType: "task", Namespace: "lm", Parent: parent, Actor: "u", Now: "n2"})
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: parent, Claim: true, Actor: "alice", Now: "n3"}))
	childAfter := mustCreate(t, db, CreateInput{Title: "child-after", Priority: 2, BeadType: "task", Namespace: "lm", Parent: parent, Actor: "u", Now: "n4"})

	got, err := DerivedMetrics(context.Background(), db, []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if got[parent].ChildrenAfterClaim != 1 {
		t.Errorf("ChildrenAfterClaim = %d, want 1 (only child-after: %s)", got[parent].ChildrenAfterClaim, childAfter)
	}
}

func TestDerivedMetricsScopeGrowthUsesFirstClaimEvenAfterReleaseAndReclaim(t *testing.T) {
	db := testDB(t)
	target := mustCreate(t, db, CreateInput{Title: "target", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})
	blocker := mustCreate(t, db, CreateInput{Title: "blocker", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n1"})

	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Actor: "alice", Now: "n2"}))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Release: true, Actor: "alice", Now: "n3"}))
	mustNoErr(t, mustAddLinkAt(t, db, target, blocker, BlocksDepType, "n4"))
	mustNoErr(t, mustUpdate(t, db, UpdateInput{Priority: -1, ID: target, Claim: true, Force: true, Reason: "r", Actor: "bob", Now: "n5"}))

	got, err := DerivedMetrics(context.Background(), db, []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if got[target].ClaimedAt != "n2" {
		t.Errorf("ClaimedAt = %q, want %q (the first claim, not the reclaim)", got[target].ClaimedAt, "n2")
	}
	if got[target].DepsAfterClaim != 1 {
		t.Errorf("DepsAfterClaim = %d, want 1 (the n4 dependency add, which is after the first claim T=n2)", got[target].DepsAfterClaim)
	}
}
