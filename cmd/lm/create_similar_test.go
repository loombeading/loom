// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/storage"
)

func similarSection(out string) []string {
	_, sec, ok := strings.Cut(out, "\n\n## Similar\n")
	if !ok {
		return nil
	}
	sec, _, _ = strings.Cut(sec, "\n\n")
	return strings.Split(strings.TrimSuffix(sec, "\n"), "\n")
}

func TestCreateSimilarSameTitle(t *testing.T) {
	initBeadsDir(t)
	first := mustCreateBead(t, "--title", "README の主なコマンドを削除する")

	out, errS, code := runCmd(t, []string{"create", "--title", "README の主なコマンドを削除する"}, "")
	if code != 0 {
		t.Fatalf("create: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, "- Created: ") {
		t.Fatalf("stdout = %q, want the Created: line first", out)
	}
	lines := similarSection(out)
	if len(lines) != 1 {
		t.Fatalf("## Similar lines = %q, want 1:\n%s", lines, out)
	}
	if !strings.HasPrefix(lines[0], "- "+mustDisplayAlias(t, first)+" [open/P2] ") || !strings.HasSuffix(lines[0], "  match=12/12") {
		t.Errorf("similar line = %q, want %s with match=12/12", lines[0], first)
	}
}

func TestCreateSimilarNoneOmitsSection(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "alpha beta gamma")

	out, _, code := runCmd(t, []string{"create", "--title", "delta epsilon zeta"}, "")
	if code != 0 {
		t.Fatalf("create: exit code = %d", code)
	}
	if strings.Count(out, "\n") != 1 || strings.Contains(out, "## Similar") {
		t.Errorf("stdout = %q, want only the Created: line", out)
	}
}

func TestCreateSimilarThreshold(t *testing.T) {
	initBeadsDir(t)
	half := mustCreateBead(t, "--title", "alpha beta")
	mustCreateBead(t, "--title", "alpha only")
	body := mustCreateBead(t, "--title", "unrelated", "--description", "alpha beta gamma delta")

	out, _, _ := runCmd(t, []string{"create", "--title", "alpha beta gamma delta"}, "")
	lines := similarSection(out)
	if len(lines) != 2 {
		t.Fatalf("## Similar lines = %q, want 2:\n%s", lines, out)
	}
	if !strings.HasPrefix(lines[0], "- "+mustDisplayAlias(t, body)+" ") || !strings.HasSuffix(lines[0], "  match=4/4") {
		t.Errorf("line 0 = %q, want %s match=4/4", lines[0], body)
	}
	if !strings.HasPrefix(lines[1], "- "+mustDisplayAlias(t, half)+" ") || !strings.HasSuffix(lines[1], "  match=2/4") {
		t.Errorf("line 1 = %q, want %s match=2/4", lines[1], half)
	}
}

func TestCreateSimilarShortTitleSkipped(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "alpha")

	out, _, _ := runCmd(t, []string{"create", "--title", "alpha"}, "")
	if strings.Contains(out, "## Similar") {
		t.Errorf("stdout = %q, want no ## Similar for a one-word title", out)
	}
}

func TestCreateSimilarExclusions(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "alpha beta parent")
	blocker := mustCreateBead(t, "--title", "alpha beta blocker")
	closed := mustCreateBead(t, "--title", "alpha beta closed")
	if _, errS, code := runCmd(t, []string{"close", closed}, ""); code != 0 {
		t.Fatalf("close: %q", errS)
	}
	mustCreateBead(t, "--title", "alpha beta gate", "--type", "gate", "--label", "kind:adjudicate")
	other := mustCreateBead(t, "--title", "alpha beta other")
	if _, errS, code := runCmd(t, []string{"create", "--namespace", "zz", "--title", "alpha beta ns"}, ""); code != 0 {
		t.Logf("namespace create skipped: %q", errS)
	}

	out, _, _ := runCmd(t, []string{"create", "--title", "alpha beta", "--parent", parent, "--blocked-by", blocker}, "")
	lines := similarSection(out)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "- "+mustDisplayAlias(t, other)+" ") ||
		!strings.HasPrefix(lines[1], "- "+mustDisplayAlias(t, closed)+" [closed/") {
		t.Errorf("## Similar lines = %q, want %s then closed %s:\n%s", lines, other, closed, out)
	}

	gateOut, _, _ := runCmd(t, []string{"create", "--title", "alpha beta", "--type", "gate", "--label", "kind:adjudicate"}, "")
	if strings.Contains(gateOut, "## Similar") {
		t.Errorf("gate create stdout = %q, want no ## Similar", gateOut)
	}
}

func TestCreateSimilarLimitAndOrder(t *testing.T) {
	initBeadsDir(t)
	ids := make([]string, 0, 7)
	for _, p := range []string{"3", "3", "1", "2", "4", "1", "2"} {
		ids = append(ids, mustCreateBead(t, "--title", "alpha beta", "-p", p))
	}
	out, _, _ := runCmd(t, []string{"create", "--title", "alpha beta"}, "")
	lines := similarSection(out)
	want := []string{ids[2], ids[5], ids[3], ids[6], ids[0]}
	if len(lines) != len(want) {
		t.Fatalf("## Similar lines = %d, want %d:\n%s", len(lines), len(want), out)
	}
	for i, id := range want {
		if !strings.HasPrefix(lines[i], "- "+mustDisplayAlias(t, id)+" ") {
			t.Errorf("line %d = %q, want %s", i, lines[i], id)
		}
	}
}

func TestCreateSimilarClosedSlot(t *testing.T) {
	initBeadsDir(t)
	old := mustCreateBead(t, "--title", "alpha beta old")
	if _, errS, code := runCmd(t, []string{"close", old}, ""); code != 0 {
		t.Fatalf("close: %q", errS)
	}
	ctx := context.Background()
	db, err := storage.Open(ctx, beadsDir(t), storage.OpenOptions{Env: func(string) string { return "" }, Actor: "test"})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	oldClosedAt := time.Now().UTC().Add(-31 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z07:00")
	_, err = db.SQL.ExecContext(ctx, `UPDATE beads SET closed_at = ? WHERE id = ?`, oldClosedAt, old)
	_ = db.Close()
	if err != nil {
		t.Fatalf("backdate closed_at: %v", err)
	}
	open := make([]string, 0, 6)
	closed := make([]string, 0, 4)
	for range 6 {
		open = append(open, mustCreateBead(t, "--title", "alpha beta open"))
	}
	for range 4 {
		id := mustCreateBead(t, "--title", "alpha beta closed")
		if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
			t.Fatalf("close: %q", errS)
		}
		closed = append(closed, id)
	}
	cancelled := mustCreateBead(t, "--title", "alpha beta cancelled")
	if _, errS, code := runCmd(t, []string{"update", cancelled, "--status", "cancelled"}, ""); code != 0 {
		t.Fatalf("cancel: %q", errS)
	}

	out, errS, code := runCmd(t, []string{"create", "--title", "alpha beta"}, "")
	if code != 0 {
		t.Fatalf("create: exit %d, stderr %q", code, errS)
	}
	lines := similarSection(out)
	want := append(append([]string{}, open[:5]...), closed[:3]...)
	if len(lines) != len(want) {
		t.Fatalf("## Similar lines = %d, want %d:\n%s", len(lines), len(want), out)
	}
	for i, id := range want {
		status := "[open/"
		if i >= 5 {
			status = "[closed/"
		}
		if !strings.HasPrefix(lines[i], "- "+mustDisplayAlias(t, id)+" "+status) {
			t.Errorf("line %d = %q, want %s %s", i, lines[i], id, status)
		}
	}
}

func TestCreateSimilarMilestone(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "release epic", "--label", "milestone:v1")
	mid := mustCreateBead(t, "--title", "middle", "--parent", epic)
	child := mustCreateBead(t, "--title", "alpha beta child", "--parent", mid)
	loose := mustCreateBead(t, "--title", "alpha beta loose", "-p", "3")

	out, errS, code := runCmd(t, []string{"create", "--title", "alpha beta", "--no-milestone", "outside"}, "")
	if code != 0 {
		t.Fatalf("create: exit %d, stderr %q", code, errS)
	}
	lines := similarSection(out)
	if len(lines) != 3 {
		t.Fatalf("## Similar lines = %q, want 3:\n%s", lines, out)
	}
	if !strings.HasPrefix(lines[0], "- "+mustDisplayAlias(t, child)+" ") || !strings.HasSuffix(lines[0], "  match=2/2  milestone="+mustDisplayAlias(t, epic)) {
		t.Errorf("line 0 = %q, want %s with milestone=%s", lines[0], child, epic)
	}
	if !strings.HasPrefix(lines[1], "- "+mustDisplayAlias(t, loose)+" ") || !strings.HasSuffix(lines[1], "  match=2/2") {
		t.Errorf("line 1 = %q, want %s without milestone", lines[1], loose)
	}
	if !strings.HasPrefix(lines[2], "- Hint: ") || !strings.Contains(lines[2], "--type parent-child") {
		t.Errorf("line 2 = %q, want the Hint line", lines[2])
	}

	plain, plainErr, _ := runCmd(t, []string{"create", "--title", "alpha beta"}, "")
	if strings.Contains(plain, "- Hint: ") || !strings.Contains(plain, "milestone="+mustDisplayAlias(t, epic)) {
		t.Errorf("stdout without exempt = %q (stderr %q), want milestone= and no Hint", plain, plainErr)
	}
}

func TestSimilarWords(t *testing.T) {
	got := domain.SimilarWords("Go の lm-create 2x a 日")
	want := []string{"go", "lm", "create", "2x"}
	if len(got) != len(want) {
		t.Fatalf("SimilarWords = %v, want %v", got, want)
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("SimilarWords missing %q in %v", w, got)
		}
	}
	jp := domain.SimilarWords("重複起票abc")
	for _, w := range []string{"重複", "複起", "起票", "abc"} {
		if _, ok := jp[w]; !ok {
			t.Errorf("SimilarWords(重複起票abc) missing %q in %v", w, jp)
		}
	}
	if len(jp) != 4 {
		t.Errorf("SimilarWords(重複起票abc) = %v, want 4 words", jp)
	}
}

func TestWriteSimilarReadFailure(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "alpha beta")
	ctx := context.Background()
	db, err := storage.Open(ctx, beadsDir(t), storage.OpenOptions{Env: func(string) string { return "" }, Actor: "test"})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	for _, match := range []string{"FROM beads", "WITH RECURSIVE"} {
		var stdout, stderr strings.Builder
		writeSimilar(ctx, &stdout, &stderr, failingQuerier{Querier: db.SQL, match: match}, domain.DefaultNamespace, "alpha beta", "x", nil)
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "similar beads: ") {
			t.Errorf("match %q: stdout %q, stderr %q; want no stdout and a similar beads error", match, stdout.String(), stderr.String())
		}
	}
	var stdout strings.Builder
	writeSimilar(ctx, &stdout, io.Discard, db.SQL, domain.DefaultNamespace, "alpha beta", "x", nil)
	if !strings.Contains(stdout.String(), "## Similar") {
		t.Errorf("stdout = %q, want %s listed", stdout.String(), id)
	}
}

type rowSQLErrQuerier struct {
	domain.Querier

	match string
}

func (q rowSQLErrQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, q.match) {
		return q.Querier.QueryRowContext(ctx, "SELECT no_such_column FROM beads")
	}
	return q.Querier.QueryRowContext(ctx, query, args...)
}

func TestWriteSimilarMilestoneReadFailure(t *testing.T) {
	initBeadsDir(t)
	epic := mustCreateBead(t, "--title", "release epic", "--label", "milestone:v1")
	mustCreateBead(t, "--title", "alpha beta child", "--parent", epic)
	closed := mustCreateBead(t, "--title", "alpha beta done")
	if _, errS, code := runCmd(t, []string{"close", closed, "--reason", "done"}, ""); code != 0 {
		t.Fatalf("close: exit %d, stderr %q", code, errS)
	}
	newID := mustCreateBead(t, "--title", "alpha beta", "--no-milestone", "outside")
	ctx := context.Background()
	db, err := storage.Open(ctx, beadsDir(t), storage.OpenOptions{Env: func(string) string { return "" }, Actor: "test"})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	newID, err = domain.ResolveID(ctx, db.SQL, newID)
	if err != nil {
		t.Fatalf("ResolveID: %v", err)
	}
	for _, q := range []domain.Querier{
		failingQuerier{Querier: db.SQL, match: "closed_at >="},
		rowSQLErrQuerier{Querier: db.SQL, match: "WITH RECURSIVE anc"},
		rowSQLErrQuerier{Querier: db.SQL, match: "COALESCE(labels"},
	} {
		var stdout, stderr strings.Builder
		writeSimilar(ctx, &stdout, &stderr, q, domain.DefaultNamespace, "alpha beta", newID, nil)
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "similar beads: ") {
			t.Errorf("%#v: stdout %q, stderr %q; want no stdout and a similar beads error", q, stdout.String(), stderr.String())
		}
	}
	var stdout strings.Builder
	writeSimilar(ctx, &stdout, io.Discard, db.SQL, domain.DefaultNamespace, "alpha beta", newID, nil)
	if !strings.Contains(stdout.String(), "- Hint: ") {
		t.Errorf("stdout = %q, want the Hint line", stdout.String())
	}
}
