// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func mustImport(t *testing.T, jsonl string, extra ...string) string {
	t.Helper()
	args := append([]string{"import"}, extra...)
	args = append(args, "-")
	out, errS, code := runCmd(t, args, jsonl)
	if code != 0 {
		t.Fatalf("import: exit code = %d, stderr = %q, stdout = %q", code, errS, out)
	}
	return out
}

func canonicalID(t *testing.T, displayID string) string {
	t.Helper()
	out, errS, code := runCmd(t, []string{"show", displayID}, "")
	if code != 0 {
		t.Fatalf("show %s: exit code = %d, stderr = %q", displayID, code, errS)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "id: ") {
			return frontMatterStringValue(line, "id")
		}
	}
	t.Fatalf("show %s: no id line:\n%s", displayID, out)
	return ""
}

func exportString(t *testing.T) string {
	t.Helper()
	out, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export: exit code = %d, stderr = %q", code, errS)
	}
	return out
}

func TestImportOrderIndependentByteIdentical(t *testing.T) {
	line1 := `{"_type":"bead","id":"a1111111111111111111111111","namespace":"lm","title":"one","status":"open","priority":2,"type":"task","labels":[],"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z","_set_at":{"namespace":"2026-01-01T00:00:00.000Z","title":"2026-01-01T00:00:00.000Z","status":"2026-01-01T00:00:00.000Z","priority":"2026-01-01T00:00:00.000Z","type":"2026-01-01T00:00:00.000Z"}}`
	line2 := `{"_type":"bead","id":"b2222222222222222222222222","namespace":"lm","title":"two","status":"open","priority":2,"type":"task","labels":[],"created_at":"2026-01-01T00:00:01.000Z","updated_at":"2026-01-01T00:00:01.000Z","_set_at":{"namespace":"2026-01-01T00:00:01.000Z","title":"2026-01-01T00:00:01.000Z","status":"2026-01-01T00:00:01.000Z","priority":"2026-01-01T00:00:01.000Z","type":"2026-01-01T00:00:01.000Z"}}`
	line3 := `{"_type":"dependency","bead_id":"a1111111111111111111111111","depends_on_id":"b2222222222222222222222222","type":"blocks","created_at":"2026-01-01T00:00:02.000Z","removed":false}`

	orders := [][]string{
		{line1, line2, line3},
		{line3, line2, line1},
		{line2, line1, line3},
	}
	exports := make([]string, 0, len(orders))
	for _, order := range orders {
		initBeadsDir(t)
		mustImport(t, strings.Join(order, "\n")+"\n")
		exports = append(exports, exportString(t))
	}
	for i := 1; i < len(exports); i++ {
		if exports[0] != exports[i] {
			t.Fatalf("import order %d differs from order 0:\n--- 0 ---\n%s\n--- %d ---\n%s", i, exports[0], i, exports[i])
		}
	}
}

func TestImportHistoryNotFoldedIntoCreatedLine(t *testing.T) {
	initBeadsDir(t)
	line := `{"_type":"bead","id":"c3333333333333333333333333","namespace":"lm","title":"imported","status":"open","priority":2,"type":"task","labels":[],"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z","_set_at":{"namespace":"2026-01-01T00:00:00.000Z","title":"2026-01-01T00:00:00.000Z","status":"2026-01-01T00:00:00.000Z","priority":"2026-01-01T00:00:00.000Z","type":"2026-01-01T00:00:00.000Z"}}`
	mustImport(t, line+"\n")

	out, errS, code := runCmd(t, []string{"show", "c3333333333333333333333333"}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	hist := historySection(t, out)
	if strings.Contains(hist, "created (") {
		t.Errorf("history section = %q, want no folded created(...) line for an imported bead", hist)
	}
	if !strings.Contains(hist, "merge namespace = lm") {
		t.Errorf("history section = %q, want an individual merge-record line for field namespace", hist)
	}
	if !strings.Contains(hist, "merge title = imported") {
		t.Errorf("history section = %q, want an individual merge-record line for field title", hist)
	}
}

func TestImportFieldLevelLWWDifferentColumnsWin(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "before")
	cid := canonicalID(t, id)

	jsonl := `{"_type":"bead","id":"` + cid + `","namespace":"lm","title":"after","status":"in_progress","priority":2,"type":"task","labels":[],"created_at":"2020-01-01T00:00:00.000Z","updated_at":"2099-01-01T00:00:00.000Z","_set_at":{"title":"2099-01-01T00:00:00.000Z","status":"2000-01-01T00:00:00.000Z"}}`
	mustImport(t, jsonl)

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: %d %q", code, errS)
	}
	if !strings.Contains(out, `title: "after"`) {
		t.Errorf("expected newer title to win:\n%s", out)
	}
	if !strings.Contains(out, `status: "open"`) {
		t.Errorf("expected older status assertion to lose (stay open):\n%s", out)
	}
}

func TestImportSameTimestampTieBreaksOnValueBytes(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "aaa")
	cid := canonicalID(t, id)

	sameTS := "2099-01-01T00:00:00.000Z"
	jsonl := `{"_type":"bead","id":"` + cid + `","namespace":"lm","title":"zzz","status":"open","priority":2,"type":"task","labels":[],"created_at":"2020-01-01T00:00:00.000Z","updated_at":"` + sameTS + `","_set_at":{"title":"` + sameTS + `"}}`
	mustImport(t, jsonl)

	out, _, _ := runCmd(t, []string{"show", id}, "")

	if !strings.Contains(out, `title: "zzz"`) {
		t.Errorf("expected zzz to win:\n%s", out)
	}
}

func TestImportOutOfVocabularyBeadTypeRejectsWholeBatch(t *testing.T) {
	initBeadsDir(t)
	good := `{"_type":"bead","id":"c3333333333333333333333333","namespace":"lm","title":"x","status":"open","priority":2,"type":"task","labels":[],"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}`
	bad := `{"_type":"bead","id":"d4444444444444444444444444","namespace":"lm","title":"x","status":"open","priority":2,"type":"spike","labels":[],"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}`
	_, errS, code := runCmd(t, []string{"import", "-"}, good+"\n"+bad+"\n")
	if code == 0 || !strings.Contains(errS, `out-of-vocabulary type "spike"`) {
		t.Fatalf("import: exit code = %d, stderr = %q, want a failure naming the type", code, errS)
	}
	if exp := exportString(t); strings.Contains(exp, "c3333333333333333333333333") {
		t.Errorf("export after a rejected batch = %q, want no row from that batch", exp)
	}
}

func TestImportLabelsWholeSetLWWOlderRemovalWins(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "x", "--label", "keep-me")
	cid := canonicalID(t, id)

	jsonl := `{"_type":"bead","id":"` + cid + `","namespace":"lm","title":"x","status":"open","priority":2,"type":"task","labels":[],"created_at":"2020-01-01T00:00:00.000Z","updated_at":"2099-01-01T00:00:00.000Z","_set_at":{"labels":"2099-01-01T00:00:00.000Z"}}`
	mustImport(t, jsonl)

	out, _, _ := runCmd(t, []string{"show", id}, "")
	if !strings.Contains(out, "labels: []") {
		t.Errorf("expected the newer (empty) label set to win entirely:\n%s", out)
	}
}

func TestImportCycleIsResolvedDeterministically(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")
	c := mustCreateBead(t, "--title", "c")
	ca, cc := canonicalID(t, a), canonicalID(t, c)

	if _, _, code := runCmd(t, []string{"dep", "add", b, a, "--type", "blocks"}, ""); code != 0 {
		t.Fatalf("dep add b a")
	}
	if _, _, code := runCmd(t, []string{"dep", "add", c, b, "--type", "blocks"}, ""); code != 0 {
		t.Fatalf("dep add c b")
	}

	jsonl := `{"_type":"dependency","bead_id":"` + ca + `","depends_on_id":"` + cc + `","type":"blocks","created_at":"2099-01-01T00:00:00.000Z","removed":false}`
	out := mustImport(t, jsonl)
	if !strings.Contains(out, "Cycle resolved:") {
		t.Fatalf("expected a Cycle resolved line:\n%s", out)
	}

	showOut, _, _ := runCmd(t, []string{"show", a, "--all-deps"}, "")
	if strings.Contains(showOut, cc+" | blocks | false") {
		t.Errorf("expected the newest (a->c) edge to have been removed:\n%s", showOut)
	}
}

func TestImportDuplicateParentKeepsOldest(t *testing.T) {
	initBeadsDir(t)
	child := mustCreateBead(t, "--title", "child")
	oldParent := mustCreateBead(t, "--title", "old-parent")
	newParent := mustCreateBead(t, "--title", "new-parent")
	cchild, coldParent, cnewParent := canonicalID(t, child), canonicalID(t, oldParent), canonicalID(t, newParent)

	jsonl := `{"_type":"dependency","bead_id":"` + cchild + `","depends_on_id":"` + coldParent + `","type":"parent-child","created_at":"2020-01-01T00:00:00.000Z","removed":false}
{"_type":"dependency","bead_id":"` + cchild + `","depends_on_id":"` + cnewParent + `","type":"parent-child","created_at":"2099-01-01T00:00:00.000Z","removed":false}`
	out := mustImport(t, jsonl)
	if !strings.Contains(out, "Parent resolved:") {
		t.Fatalf("expected a Parent resolved line:\n%s", out)
	}

	showOut, _, _ := runCmd(t, []string{"show", child, "--all-deps"}, "")
	if !strings.Contains(showOut, "(removed)") {
		t.Errorf("expected the newer parent edge to have been removed:\n%s", showOut)
	}
}

func TestImportDanglingDependencyCounted(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "x")
	cid := canonicalID(t, id)
	jsonl := `{"_type":"dependency","bead_id":"` + cid + `","depends_on_id":"zzzzzzzzzzzzzzzzzzzzzzzzzz","type":"blocks","created_at":"2026-01-01T00:00:00.000Z","removed":false}`
	out := mustImport(t, jsonl)
	if !strings.Contains(out, "Dangling: 1 link(s)") {
		t.Fatalf("expected Dangling: 1 link(s):\n%s", out)
	}
}

func TestImportSkipsUnknownTypeLine(t *testing.T) {
	initBeadsDir(t)
	jsonl := `{"_type":"audit","occurred_at":"2026-01-01T00:00:00.000Z","actor":"x","kind":"field"}`
	out := mustImport(t, jsonl)
	if !strings.Contains(out, "Skipped: 1 line(s) with unknown _type") {
		t.Fatalf("expected Skipped line:\n%s", out)
	}
	exp := exportString(t)
	if strings.Contains(exp, "audit") {
		t.Errorf("audit-typed line must not reach the database:\n%s", exp)
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	initBeadsDir(t)
	before := exportString(t)

	jsonl := `{"_type":"bead","id":"e5555555555555555555555555","namespace":"lm","title":"x","status":"open","priority":2,"type":"task","labels":[],"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}`
	out := mustImport(t, jsonl, "--dry-run")
	if !strings.HasPrefix(out, "Dry run: no changes written\n") {
		t.Fatalf("expected the dry-run line first:\n%s", out)
	}
	if !strings.Contains(out, "Imported: 1 bead(s)") {
		t.Errorf("expected the dry-run merge result to still be reported:\n%s", out)
	}

	after := exportString(t)
	if before != after {
		t.Errorf("dry-run must not change the database:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestImportMalformedJSONLineRollsBackEverything(t *testing.T) {
	initBeadsDir(t)
	before := exportString(t)

	jsonl := `{"_type":"bead","id":"f6666666666666666666666666","namespace":"lm","title":"x","status":"open","priority":2,"type":"task","labels":[],"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}
not valid json`
	_, errS, code := runCmd(t, []string{"import", "-"}, jsonl)
	if code == 0 {
		t.Fatalf("expected a non-zero exit code for malformed JSON, stderr = %q", errS)
	}

	after := exportString(t)
	if before != after {
		t.Errorf("malformed line must roll back the whole batch:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestImportRejectedUnderReadOnly(t *testing.T) {
	initBeadsDir(t)
	t.Setenv("LM_READONLY", "1")
	jsonl := `{"_type":"bead","id":"g7777777777777777777777777","namespace":"lm","title":"x","status":"open","priority":2,"type":"task","labels":[],"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}`
	_, _, code := runCmd(t, []string{"import", "-"}, jsonl)
	if code == 0 {
		t.Fatalf("expected import to be rejected under LM_READONLY")
	}
}

func TestImportExportRoundTripByteIdentical(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a", "--label", "x", "--label", "y", "--external-ref", "PR#1")
	mustCreateBead(t, "--title", "b", "--parent", a)
	first := exportString(t)

	initBeadsDir(t)
	mustImport(t, first)
	second := exportString(t)

	if first != second {
		t.Fatalf("export -> import -> export is not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}
