// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func lineOf(t *testing.T, out, title string) (string, int) {
	t.Helper()
	for i, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, " "+title) || strings.Contains(line, " "+title+"  ") {
			return line, i
		}
	}
	t.Fatalf("no line for %q in:\n%s", title, out)
	return "", -1
}

func TestReadyShowsEffectivePriorityAndOrdersByIt(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "plain p1", "--priority", "1")
	blocker := mustCreateBead(t, "--title", "blocker p2", "--priority", "2")
	p0 := mustCreateBead(t, "--title", "urgent p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
	blockerAlias := mustDisplayAlias(t, blocker)
	p0Alias := mustDisplayAlias(t, p0)

	out, errS, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d, stderr = %q", code, errS)
	}
	bl, bi := lineOf(t, out, "blocker p2")
	pl, pi := lineOf(t, out, "plain p1")
	if bi > pi {
		t.Errorf("blocker of P0 must come before the P1 Bead:\n%s", out)
	}
	if !strings.Contains(bl, "[open/P2]") || !strings.HasSuffix(bl, "  eff=P0("+p0Alias+")") {
		t.Errorf("blocker line = %q, want stored P2 and eff=P0(%s) at the end", bl, p0Alias)
	}
	if strings.Contains(pl, "eff=") {
		t.Errorf("plain line = %q, must not show eff= when it equals the stored priority", pl)
	}

	out, errS, code = runCmd(t, []string{"ready", "--claim"}, "")
	if code != 0 {
		t.Fatalf("ready --claim: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "Claimed: "+blockerAlias) || !strings.Contains(out, "eff=P0("+p0Alias+")") {
		t.Errorf("ready --claim output = %q, want the blocker claimed with eff=P0(%s)", out, p0Alias)
	}
}

func TestNewlyReadyOrdersByEffectivePriority(t *testing.T) {
	initBeadsDir(t)
	pre := mustCreateBead(t, "--title", "pre")
	mustCreateBead(t, "--title", "freed p1", "--priority", "1", "--blocked-by", pre)
	blocker := mustCreateBead(t, "--title", "freed p3", "--priority", "3", "--blocked-by", pre)
	p0 := mustCreateBead(t, "--title", "urgent p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
	p0Alias := mustDisplayAlias(t, p0)

	out, errS, code := runCmd(t, []string{"close", pre}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	_, section, _ := strings.Cut(out, "## Newly ready")
	bl, bi := lineOf(t, section, "freed p3")
	_, pi := lineOf(t, section, "freed p1")
	if bi > pi {
		t.Errorf("Newly ready must list the blocker of P0 first:\n%s", section)
	}
	if !strings.HasSuffix(bl, "  eff=P0("+p0Alias+")") {
		t.Errorf("blocker line = %q, want eff=P0(%s)", bl, p0Alias)
	}
}

var errEffBoom = errors.New("boom")

type failingQuerier struct {
	domain.Querier

	match string
}

func (q failingQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, q.match) {
		return nil, errEffBoom
	}
	return q.Querier.QueryContext(ctx, query, args...)
}

func TestWithEffectiveAndNewlyReadyErrors(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	ctx := context.Background()
	db, err := storage.Open(ctx, beadsDir(t), storage.OpenOptions{Env: func(string) string { return "" }, Actor: "test"})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows := []output.BeadRow{{CanonicalID: id}}
	raised := map[string]domain.EffectivePriority{id: {Priority: 0, Source: "0000000000000000000000NONE"}}
	if err := withEffective(ctx, failingQuerier{Querier: db.SQL, match: "WITH RECURSIVE"}, rows, raised, nil); !errors.Is(err, errEffBoom) {
		t.Errorf("withEffective with a failing alias query: err = %v, want errEffBoom", err)
	}
	if err := withEffective(ctx, db.SQL, rows, raised, nil); !errors.Is(err, domain.ErrBeadNotFound) {
		t.Errorf("withEffective with a missing source: err = %v, want ErrBeadNotFound", err)
	}
	q := failingQuerier{Querier: db.SQL, match: "FROM lend"}
	if err := writeNewlyReady(ctx, q, io.Discard, nil, map[string]bool{id: true}, ""); !errors.Is(err, errEffBoom) {
		t.Errorf("writeNewlyReady with a failing effective-priority query: err = %v, want errEffBoom", err)
	}
}

type rowFailingQuerier struct {
	domain.Querier

	match string
}

func (q rowFailingQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, q.match) {
		return q.Querier.QueryRowContext(ctx, "SELECT id FROM beads WHERE 1 = 0")
	}
	return q.Querier.QueryRowContext(ctx, query, args...)
}

type nthMatchErrQuerier struct {
	domain.Querier

	match  string
	failOn int
	n      int
}

func (q *nthMatchErrQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, q.match) {
		q.n++
		if q.n == q.failOn {
			return nil, errEffBoom
		}
	}
	return q.Querier.QueryContext(ctx, query, args...)
}

func TestWriteNewlyReadyFillSourcesAndWithEffectiveErrors(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker p2", "--priority", "2")
	mustCreateBead(t, "--title", "urgent p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
	ctx := context.Background()
	db, err := storage.Open(ctx, beadsDir(t), storage.OpenOptions{Env: func(string) string { return "" }, Actor: "test"})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	rowQ := rowFailingQuerier{Querier: db.SQL, match: "reach(id)"}
	if err := writeNewlyReady(ctx, rowQ, io.Discard, nil, map[string]bool{blocker: true}, ""); err == nil {
		t.Error("writeNewlyReady with a failing FillSources query: err = nil, want an error")
	}

	seedQ := &nthMatchErrQuerier{Querier: db.SQL, match: "seed(id)", failOn: 2}
	if err := writeNewlyReady(ctx, seedQ, io.Discard, nil, map[string]bool{blocker: true}, ""); !errors.Is(err, errEffBoom) {
		t.Errorf("writeNewlyReady with a failing withEffective alias query: err = %v, want errEffBoom", err)
	}
}

func TestListSortPriorityAndLimitUseEffectivePriority(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "plain p1", "--priority", "1")
	blocker := mustCreateBead(t, "--title", "blocker p3", "--priority", "3")
	p0 := mustCreateBead(t, "--title", "urgent p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
	p0Alias := mustDisplayAlias(t, p0)

	out, errS, code := runCmd(t, []string{"list", "--sort", "priority", "--limit", "2"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d, stderr = %q", code, errS)
	}
	bl, bi := lineOf(t, out, "blocker p3")
	_, ui := lineOf(t, out, "urgent p0")
	if bi > ui {
		t.Errorf("blocker of P0 created before it must come first:\n%s", out)
	}
	if !strings.Contains(bl, "[open/P3]") || !strings.HasSuffix(bl, "  eff=P0("+p0Alias+")") {
		t.Errorf("blocker line = %q, want stored P3 and eff=P0(%s)", bl, p0Alias)
	}
	if strings.Contains(out, "plain p1") {
		t.Errorf("--limit 2 must cut after sorting by effective priority:\n%s", out)
	}

	out, _, _ = runCmd(t, []string{"list"}, "")
	if strings.Contains(out, "eff=") {
		t.Errorf("list in updated order = %q, must not re-sort or mark eff=", out)
	}
}

func TestBlockedOrdersByEffectivePriority(t *testing.T) {
	initBeadsDir(t)
	pre := mustCreateBead(t, "--title", "pre")
	mustCreateBead(t, "--title", "waiting p1", "--priority", "1", "--blocked-by", pre)
	mid := mustCreateBead(t, "--title", "waiting p3", "--priority", "3", "--blocked-by", pre)
	p0 := mustCreateBead(t, "--title", "urgent p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", mid)
	preAlias := mustDisplayAlias(t, pre)
	p0Alias := mustDisplayAlias(t, p0)

	out, errS, code := runCmd(t, []string{"blocked", "--limit", "1"}, "")
	if code != 0 {
		t.Fatalf("blocked: exit code = %d, stderr = %q", code, errS)
	}
	ml, _ := lineOf(t, out, "waiting p3  ← blocked by: "+preAlias)
	if !strings.HasSuffix(ml, "  eff=P0("+p0Alias+")") {
		t.Errorf("blocked line = %q, want eff=P0(%s)", ml, p0Alias)
	}
	if strings.Contains(out, "waiting p1") {
		t.Errorf("--limit 1 must keep the effective-P0 row:\n%s", out)
	}
}

func TestShowEffectivePriority(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker p2", "--priority", "2")
	p0 := mustCreateBead(t, "--title", "urgent p0", "--expedite", "1h", "--reason", "interrupt", "--blocked-by", blocker)
	p0Alias := mustDisplayAlias(t, p0)

	out, errS, code := runCmd(t, []string{"show", blocker}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	for _, want := range []string{"priority: 2\n", "effective_priority: 0\n", `effective_priority_source: "` + p0Alias + `"` + "\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}

	out, _, _ = runCmd(t, []string{"show", p0}, "")
	for _, want := range []string{"effective_priority: 0\n", "effective_priority_source: \"expedite\"\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}

	if _, errS, code := runCmd(t, []string{"close", p0}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	out, _, _ = runCmd(t, []string{"show", blocker}, "")
	for _, want := range []string{"effective_priority: 2\n", "effective_priority_source: null\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output after the downstream closed missing %q:\n%s", want, out)
		}
	}

	out, _, _ = runCmd(t, []string{"export"}, "")
	if strings.Contains(out, "effective_priority") {
		t.Errorf("export output = %q, must not carry the derived effective priority", out)
	}
}
