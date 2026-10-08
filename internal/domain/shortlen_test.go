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

func TestLoadShortIDsReadsStoredColumn(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "abcde1111111111111111111111111a", "lm", "2026-01-01T00:00:00.000Z")
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET short_id = 'abcde'`); err != nil {
		t.Fatal(err)
	}
	s, err := LoadShortIDs(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if got := DisplayID("lm", "abcde1111111111111111111111111a", s); got != "lm-abcde" {
		t.Fatalf("DisplayID = %q, want lm-abcde", got)
	}
}

func TestDisplayIDFallsBackToDefaultLen(t *testing.T) {
	got := DisplayID("lm", "a3f8000000000000000000000000000", ShortIDs{})
	if want := "lm-a3f8"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAllocateCanonicalIDRedrawsOnPrefixOverlap(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "0000aaaaaaaaaaaaaaaaaaaaaa", "lm", "2026-01-01T00:00:00.000Z")
	for range 50 {
		id, short, err := allocateCanonicalID(t.Context(), db, 4)
		if err != nil {
			t.Fatal(err)
		}
		if len(short) < 4 || !strings.HasPrefix(id, short) {
			t.Fatalf("short %q is not a prefix >= 4 of %q", short, id)
		}
		if strings.HasPrefix(id, "0000") {
			t.Fatalf("id %q overlaps the existing prefix 0000", id)
		}
	}
}

func TestAllocateCanonicalIDClampsLength(t *testing.T) {
	db := testDB(t)
	id, short, err := allocateCanonicalID(t.Context(), db, 100)
	if err != nil {
		t.Fatal(err)
	}
	if short != id {
		t.Fatalf("short = %q, want the canonical id %q at the length cap", short, id)
	}
	_, short, err = allocateCanonicalID(t.Context(), db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(short) != MinShortLen {
		t.Fatalf("len(short) = %d, want %d", len(short), MinShortLen)
	}
}

func TestImportShortIDExtendsUntilUnique(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "abcd1111111111111111111111", "lm", "2026-01-01T00:00:00.000Z")
	got, err := ImportShortID(t.Context(), db, "abcd2222222222222222222222", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abcd2" {
		t.Fatalf("got %q, want abcd2", got)
	}
	got, err = ImportShortID(t.Context(), db, "ffff2222222222222222222222", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ffff" {
		t.Fatalf("got %q, want ffff", got)
	}
}

func TestResolveIDExactShortIDBeatsLongerPrefixMatch(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "abc1111111111111111111111a", "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, "abcd222222222222222222222b", "lm", "2026-01-01T00:00:00.001Z")
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET short_id = 'abcd' WHERE id LIKE 'abcd%'`); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveID(t.Context(), db, "lm-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc1111111111111111111111a" {
		t.Fatalf("got %q, want the bead whose short_id is abc", got)
	}
}

func TestResolveIDPrefersExactShortID(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "abcd1111111111111111111111", "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, "abcd2222222222222222222222", "lm", "2026-01-01T00:00:00.001Z")
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET short_id = substr(id, 1, 5)`); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveID(t.Context(), db, "lm-abcd2")
	if err != nil {
		t.Fatal(err)
	}
	if got != "abcd2222222222222222222222" {
		t.Fatalf("got %q", got)
	}
	if _, err := ResolveID(t.Context(), db, "lm-abc"); err == nil {
		t.Fatal("ResolveID(lm-abc) = nil error, want ambiguity")
	}
}

func TestCreateBeadStoresShortIDAndNeverOverlapsExistingPrefixes(t *testing.T) {
	db := testDB(t)
	seen := map[string]bool{}
	for i := range 40 {
		id := mustCreate(t, db, CreateInput{
			Title: "t", Priority: DefaultPriority, BeadType: DefaultBeadType,
			Namespace: "lm", Actor: "a", Now: "2026-01-01T00:00:00.000Z", ShortLen: 3 + i%2,
		})
		var short string
		if err := db.QueryRowContext(t.Context(), `SELECT short_id FROM beads WHERE id = ?`, id).Scan(&short); err != nil {
			t.Fatal(err)
		}
		if len(short) < 3 || !strings.HasPrefix(id, short) {
			t.Fatalf("short_id %q is not a prefix of %q", short, id)
		}
		if seen[short] {
			t.Fatalf("short_id %q repeated", short)
		}
		seen[short] = true
	}
}

type stubShortQuerier struct {
	Querier

	rewrite func(query string) string
	taken   func(call int) bool
	calls   int
}

func (q *stubShortQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if q.rewrite != nil {
		query = q.rewrite(query)
	}
	return q.Querier.QueryContext(ctx, query, args...)
}

func (q *stubShortQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	q.calls++
	if q.taken(q.calls) {
		return q.Querier.QueryRowContext(ctx, `SELECT 1`)
	}
	return q.Querier.QueryRowContext(ctx, `SELECT 1 WHERE 0`)
}

func TestLoadShortIDsReportsQueryAndScanFailures(t *testing.T) {
	db := testDB(t)
	closed := testDB(t)
	_ = closed.Close()
	if _, err := LoadShortIDs(t.Context(), closed); err == nil {
		t.Fatal("LoadShortIDs on a closed database: err = nil")
	}
	insertBead(t, db, "abc1111111111111111111111a", "lm", "2026-01-01T00:00:00.000Z")
	q := &stubShortQuerier{Querier: db, rewrite: func(string) string { return `SELECT 1, 2, 3` }}
	if _, err := LoadShortIDs(t.Context(), q); err == nil {
		t.Fatal("LoadShortIDs with a mismatched column count: err = nil")
	}
}

func TestAllocateCanonicalIDGrowsAfterRedrawLimit(t *testing.T) {
	q := &stubShortQuerier{Querier: testDB(t), taken: func(call int) bool { return call <= shortIDRedrawLimit }}
	id, short, err := allocateCanonicalID(t.Context(), q, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(short) != 5 || !strings.HasPrefix(id, short) {
		t.Fatalf("short = %q, want a 5-character prefix of %q after %d overlaps", short, id, shortIDRedrawLimit)
	}
}

func TestShortIDAllocationReportsQueryFailures(t *testing.T) {
	closed := testDB(t)
	_ = closed.Close()
	if _, _, err := allocateCanonicalID(t.Context(), closed, 4); err == nil {
		t.Fatal("allocateCanonicalID on a closed database: err = nil")
	}
	if _, err := ImportShortID(t.Context(), closed, "abcd1111111111111111111111", 4); err == nil {
		t.Fatal("ImportShortID on a closed database: err = nil")
	}
}

func TestImportShortIDFallsBackToCanonicalWhenEveryPrefixIsTaken(t *testing.T) {
	q := &stubShortQuerier{Querier: testDB(t), taken: func(int) bool { return true }}
	canonical := "abcd1111111111111111111111"
	got, err := ImportShortID(t.Context(), q, canonical, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonical {
		t.Fatalf("got %q, want the canonical id", got)
	}
}

func closeBeadForTest(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET status = 'closed', closed_at = '2026-01-02T00:00:00.000Z' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
}

func TestAllocateCanonicalIDIgnoresClosedPrefixes(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "0000aaaaaaaaaaaaaaaaaaaaaa", "lm", "2026-01-01T00:00:00.000Z")
	closeBeadForTest(t, db, "0000aaaaaaaaaaaaaaaaaaaaaa")
	taken, err := prefixTaken(t.Context(), db, "0000")
	if err != nil || taken {
		t.Fatalf("prefixTaken(closed) = %v, %v; want false, nil", taken, err)
	}
	insertBead(t, db, "0001bbbbbbbbbbbbbbbbbbbbbb", "lm", "2026-01-01T00:00:00.001Z")
	taken, err = prefixTaken(t.Context(), db, "0001")
	if err != nil || !taken {
		t.Fatalf("prefixTaken(open) = %v, %v; want true, nil", taken, err)
	}
}

func TestResolveIDPrefersOpenOverClosedAndListsStatus(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a3f81111111111111111111111", "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, "a3f82222222222222222222222", "lm", "2026-01-01T00:00:00.001Z")
	insertBead(t, db, "a3f83333333333333333333333", "lm", "2026-01-01T00:00:00.002Z")
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET short_id = 'a3f8'`); err != nil {
		t.Fatal(err)
	}
	closeBeadForTest(t, db, "a3f81111111111111111111111")
	closeBeadForTest(t, db, "a3f82222222222222222222222")

	got, err := ResolveID(t.Context(), db, "lm-a3f8")
	if err != nil || got != "a3f83333333333333333333333" {
		t.Fatalf("got %q, %v; want the open bead", got, err)
	}
	got, err = ResolveID(t.Context(), db, "lm-a3f81")
	if err != nil || got != "a3f81111111111111111111111" {
		t.Fatalf("long form: got %q, %v; want the closed bead", got, err)
	}

	insertBead(t, db, "a3f84444444444444444444444", "lm", "2026-01-01T00:00:00.003Z")
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET short_id = 'a3f8' WHERE id LIKE 'a3f84%'`); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveID(t.Context(), db, "lm-a3f8")
	var amb *AmbiguousIDError
	if !errors.As(err, &amb) || len(amb.Candidates) != 4 || !strings.Contains(strings.Join(amb.Candidates, "\n"), "[closed] x") {
		t.Fatalf("err = %v, want ambiguity listing status and title", err)
	}
}
