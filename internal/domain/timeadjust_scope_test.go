// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

type recordedQuery struct {
	query string
	args  []any
}

type recordingQuerier struct {
	q    Querier
	seen []recordedQuery
}

func (r *recordingQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.seen = append(r.seen, recordedQuery{query, args})
	return r.q.QueryContext(ctx, query, args...)
}

func (r *recordingQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	r.seen = append(r.seen, recordedQuery{query, args})
	return r.q.QueryRowContext(ctx, query, args...)
}

var tableAliasRE = regexp.MustCompile(`(?i)\b(beads|dependencies)\b(?:\s+(?:AS\s+)?([a-z_][a-z0-9_]*))?`)

func fullScans(t *testing.T, db *sql.DB, rq recordedQuery) []string {
	t.Helper()
	names := map[string]bool{"beads": true, "dependencies": true}
	for _, m := range tableAliasRE.FindAllStringSubmatch(rq.query, -1) {
		if m[2] != "" {
			names[strings.ToLower(m[2])] = true
		}
	}
	rows, err := db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+rq.query, rq.args...)
	if err != nil {
		t.Fatalf("explain %q: %v", rq.query, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		f := strings.Fields(detail)
		if len(f) < 2 || !names[strings.ToLower(f[1])] {
			continue
		}
		if f[0] == "SCAN" || (f[0] == "SEARCH" && !strings.Contains(detail, "id=")) {
			out = append(out, detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTimeAdjustQueriesSearchOnlyCandidates(t *testing.T) {
	f := newAdjustFixture(t)
	execSQL(t, f.db, `UPDATE coefficients SET value = 'on' WHERE key = 'time_adjust'`)
	all := f.list(t, true)
	full, err := TimeAdjust(t.Context(), f.db, f.now, mustCoefficients(t, f.db), nil, all.Beads)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"aged", "milestone-child", "decayed", "redetected", "due", "plain"} {
		id := f.ids[name]
		idx := slices.IndexFunc(all.Beads, func(b Bead) bool { return b.ID == id })
		if idx < 0 {
			t.Fatalf("%s is not Ready in the fixture", name)
		}
		rec := &recordingQuerier{q: f.db}
		one, err := TimeAdjust(t.Context(), rec, f.now, mustCoefficients(t, f.db), nil, all.Beads[idx:idx+1])
		if err != nil {
			t.Fatal(err)
		}
		if one.Eff[id] != full.Eff[id] || one.Demand[id] != full.Demand[id] || one.Held[id] != full.Held[id] ||
			!one.HeldSince[id].Equal(full.HeldSince[id]) || !one.HeldFrom[id].Equal(full.HeldFrom[id]) {
			t.Errorf("%s: adjusting it alone gave eff=%+v demand=%v held=%v, adjusting all Ready gave eff=%+v demand=%v held=%v",
				name, one.Eff[id], one.Demand[id], one.Held[id], full.Eff[id], full.Demand[id], full.Held[id])
		}
		if len(rec.seen) == 0 {
			t.Fatalf("%s: TimeAdjust ran no query", name)
		}
		for _, rq := range rec.seen {
			if !slices.ContainsFunc(rq.args, func(a any) bool { s, ok := a.(string); return ok && strings.Contains(s, id) }) {
				t.Errorf("TimeAdjust query is not bound to the candidate %s (args %v):\n%s", name, rq.args, rq.query)
			}
			if scans := fullScans(t, f.db, rq); len(scans) > 0 {
				t.Errorf("TimeAdjust query scans a whole table %v instead of searching from the candidates:\n%s", scans, rq.query)
			}
		}
	}

	many := slices.Clone(all.Beads)
	for i := range 1000 {
		many = append(many, Bead{ID: fmt.Sprintf("absent-%d", i)})
	}
	rec := &recordingQuerier{q: f.db}
	if _, err := TimeAdjust(t.Context(), rec, f.now, mustCoefficients(t, f.db), nil, many); err != nil {
		t.Fatal(err)
	}
	for _, rq := range rec.seen {
		if scans := fullScans(t, f.db, rq); len(scans) > 0 {
			t.Errorf("with %d candidates, TimeAdjust query starts from %v instead of the candidates:\n%s", len(many), scans, rq.query)
		}
	}
}

func mustCoefficients(t *testing.T, db *sql.DB) Coefficients {
	t.Helper()
	c, err := LoadCoefficients(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
