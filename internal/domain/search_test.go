// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
)

func searchCreate(t *testing.T, db *sql.DB, in CreateInput) string {
	t.Helper()
	in.Priority = DefaultPriority
	in.BeadType = DefaultBeadType
	in.Namespace = "lm"
	in.Actor = "alice"
	in.Now = "2026-01-01T00:00:00.000Z"
	return mustCreate(t, db, in)
}

func searchIDs(t *testing.T, db *sql.DB, f SearchFilter) []string {
	t.Helper()
	f.Limit = 50
	beads, total, err := SearchBeads(t.Context(), db, f)
	if err != nil {
		t.Fatalf("SearchBeads(%+v): %v", f, err)
	}
	if total != len(beads) {
		t.Errorf("SearchBeads(%+v) total = %d, want %d", f, total, len(beads))
	}
	ids := make([]string, 0, len(beads))
	for _, b := range beads {
		ids = append(ids, b.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestSearchBeads(t *testing.T) {
	db := testDB(t)
	byTitle := searchCreate(t, db, CreateInput{Title: "alpha target"})
	byReason := searchCreate(t, db, CreateInput{Title: "second", Reason: "because zebra crossing"})
	byRef := searchCreate(t, db, CreateInput{Title: "third", ExternalRefs: []string{"https://example.test/pull/777"}})
	byDesc := searchCreate(t, db, CreateInput{Title: "fourth", Description: "mentions quokka here"})
	bySummary := searchCreate(t, db, CreateInput{Title: "fifth", Summary: "wombat summary"})
	other := searchCreate(t, db, CreateInput{Title: "unrelated"})

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"title", "alpha", byTitle},
		{"reason", "zebra", byReason},
		{"external ref", "pull/777", byRef},
		{"description", "quokka", byDesc},
		{"summary", "wombat", bySummary},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := searchIDs(t, db, SearchFilter{Query: c.query})
			if !slices.Equal(got, []string{c.want}) {
				t.Errorf("query %q = %v, want [%s]", c.query, got, c.want)
			}
		})
	}
	if got := searchIDs(t, db, SearchFilter{Query: "no-such-word"}); len(got) != 0 {
		t.Errorf("no match = %v, want empty", got)
	}

	_, err := db.ExecContext(t.Context(), `UPDATE beads SET status = ? WHERE id = ?`, StatusClosed, byTitle)
	mustNoErr(t, err)
	if got := searchIDs(t, db, SearchFilter{Query: "alpha"}); len(got) != 0 {
		t.Errorf("closed without All = %v, want empty", got)
	}
	if got := searchIDs(t, db, SearchFilter{Query: "alpha", All: true}); !slices.Equal(got, []string{byTitle}) {
		t.Errorf("closed with All = %v, want [%s]", got, byTitle)
	}
	if got := searchIDs(t, db, SearchFilter{Query: "alpha", All: true, Namespace: "other"}); len(got) != 0 {
		t.Errorf("other namespace = %v, want empty", got)
	}
	if got := searchIDs(t, db, SearchFilter{Query: "unrelated"}); !slices.Equal(got, []string{other}) {
		t.Errorf("unrelated = %v, want [%s]", got, other)
	}
}

func TestSearchBeadsLikeWildcardsAreLiteral(t *testing.T) {
	db := testDB(t)
	percent := searchCreate(t, db, CreateInput{Title: "rate 50% done"})
	underscore := searchCreate(t, db, CreateInput{Title: "snake_case name"})
	plain := searchCreate(t, db, CreateInput{Title: "rate 50 done and snakeXcase"})

	if got := searchIDs(t, db, SearchFilter{Query: "50%"}); !slices.Equal(got, []string{percent}) {
		t.Errorf("percent = %v, want [%s]", got, percent)
	}
	if got := searchIDs(t, db, SearchFilter{Query: "snake_case"}); !slices.Equal(got, []string{underscore}) {
		t.Errorf("underscore = %v, want [%s]", got, underscore)
	}
	if got := searchIDs(t, db, SearchFilter{Query: "%"}); !slices.Equal(got, []string{percent}) {
		t.Errorf("bare percent = %v, want [%s] (plain %s must not match)", got, percent, plain)
	}
}

func TestSearchBeadsQueryError(t *testing.T) {
	db := testDB(t)
	q := &nthCallErrQuerier{Querier: db, failOn: 1}
	if _, _, err := SearchBeads(t.Context(), q, SearchFilter{Query: "x"}); !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
}

func TestSentLinks(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "aaa111", "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, "bbb222", "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, "ccc333", "lm", "2026-01-01T00:00:00.000Z")
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed) VALUES
			('aaa111', 'bbb222', 'blocks', '2026-01-02T00:00:00.000Z', 0),
			('ccc333', 'bbb222', 'discovered-from', '2026-01-03T00:00:00.000Z', 0)`)
	mustNoErr(t, err)

	got, err := SentLinks(t.Context(), db, "bbb222", false)
	mustNoErr(t, err)
	want := []DepRow{
		{DependsOnID: "aaa111", Type: "blocks", CreatedAt: "2026-01-02T00:00:00.000Z"},
		{DependsOnID: "ccc333", Type: "discovered-from", CreatedAt: "2026-01-03T00:00:00.000Z"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("SentLinks = %+v, want %+v", got, want)
	}

	_, err = db.ExecContext(t.Context(), `UPDATE dependencies SET removed = 1 WHERE bead_id = 'aaa111'`)
	mustNoErr(t, err)
	got, err = SentLinks(t.Context(), db, "bbb222", false)
	mustNoErr(t, err)
	if !slices.Equal(got, want[1:]) {
		t.Errorf("SentLinks without removed = %+v, want %+v", got, want[1:])
	}
	got, err = SentLinks(t.Context(), db, "bbb222", true)
	mustNoErr(t, err)
	wantAll := []DepRow{
		{DependsOnID: "aaa111", Type: "blocks", Removed: true, CreatedAt: "2026-01-02T00:00:00.000Z"},
		want[1],
	}
	if !slices.Equal(got, wantAll) {
		t.Errorf("SentLinks with removed = %+v, want %+v", got, wantAll)
	}

	got, err = SentLinks(t.Context(), db, "aaa111", true)
	mustNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("SentLinks of a bead nothing depends on = %+v, want empty", got)
	}

	q := &nthCallErrQuerier{Querier: db, failOn: 1}
	if _, err := SentLinks(t.Context(), q, "bbb222", false); !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
}

func TestDomainErrorStrings(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"self link", &SelfLinkError{ID: "lm-a"}, "lm-a cannot link to itself"},
		{"cycle", &CycleError{DepType: "blocks", Cycle: []string{"lm-a", "lm-b", "lm-a"}}, "adding this blocks link would create a cycle: lm-a -> lm-b -> lm-a"},
		{"duplicate parent", &DuplicateParentError{BeadID: "lm-a", CurrentParentID: "lm-p"}, "lm-a already has an active parent (lm-p); remove that link before adding a new one"},
		{"link not found", &LinkNotFoundError{BeadID: "lm-a", DependsOnID: "lm-b", DepType: "blocks"}, "no active blocks link from lm-a to lm-b"},
		{"terminal blocker", &TerminalBlockerError{IDs: []string{"lm-a", "lm-b"}}, "blocks prerequisite already terminal: lm-a, lm-b; 前提は既に終端。張らずに進めるか、前提を --reopen してから張る"},
		{"not found", &NotFoundError{Input: "zzz"}, `no Bead matches "zzz"`},
		{"ambiguous", &AmbiguousIDError{Input: "ab", Candidates: []string{"ab1", "ab2"}}, `"ab" is ambiguous; candidates: ab1, ab2`},
		{"alias unresolved", &AliasUnresolvedError{Input: "lm-x.1"}, `alias "lm-x.1" could not be resolved under the current hierarchy; use the canonical ID instead`},
		{"gate not adjudicated", &GateNotAdjudicatedError{Path: AdjudicationMissing}, "a human/confirm Gate needs adjudicated-by:<closed adjudicate Gate>; create it with --kind adjudicate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Error(); got != c.want {
				t.Errorf("Error() = %q, want %q", got, c.want)
			}
		})
	}

	err := &GateNotAdjudicatedError{Path: AdjudicationNotClosed}
	if !errors.Is(err, ErrGateNotAdjudicated) {
		t.Errorf("errors.Is(%v, ErrGateNotAdjudicated) = false, want true", err)
	}
}

func TestSimilarWords(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"ascii lowercased", "Hello World", []string{"hello", "world"}},
		{"single ascii letter dropped", "a bc d", []string{"bc"}},
		{"punctuation splits", "foo-bar_baz.qux", []string{"foo", "bar", "baz", "qux"}},
		{"digits are word characters", "v2 build42", []string{"v2", "build42"}},
		{"non-ascii bigrams", "日本語", []string{"日本", "本語"}},
		{"single non-ascii dropped", "日 本", nil},
		{"script boundary splits", "abc日本語def", []string{"abc", "日本", "本語", "def"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SimilarWords(c.in)
			if len(got) != len(c.want) {
				t.Errorf("SimilarWords(%q) = %v, want %v", c.in, got, c.want)
			}
			for _, w := range c.want {
				if _, ok := got[w]; !ok {
					t.Errorf("SimilarWords(%q) = %v, missing %q", c.in, got, w)
				}
			}
		})
	}
}
