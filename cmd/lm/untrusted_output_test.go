// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const untrustedFake = "FAKE"

func untrustedBeadLine(id, title, typ, status, claimedBy string, labels string) string {
	claim := ""
	if claimedBy != "" {
		claim = `"claimed_by":"` + claimedBy + `",`
	}
	return `{"_type":"bead","id":"` + id + `","namespace":"lm","title":"` + title + `","status":"` + status + `","priority":2,"type":"` + typ + `",` + claim + `"labels":` + labels + `,"created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}`
}

const (
	evilID     = `\n\u001b[1m\n` + untrustedFake + `0000000000000000000`
	evilGateID = `\u001b\n\u001b\n` + untrustedFake + `111111111111111111`
)

func importUntrustedBeads(t *testing.T) {
	t.Helper()
	evil := `\n\u001b[31m\n` + untrustedFake
	const beadPlaceholder, gatePlaceholder = "b2222222222222222222222222", "c3333333333333333333333333"
	lines := []string{
		untrustedBeadLine("a1111111111111111111111111", "plain", "task", "open", "", `[]`),
		untrustedBeadLine(beadPlaceholder, "t"+evil, "task", "in_progress", "who", `["milestone:m","x"]`),
		untrustedBeadLine(gatePlaceholder, "gate: s"+evil, "gate", "open", "", `["kind:human"]`),
		`{"_type":"dependency","bead_id":"a1111111111111111111111111","depends_on_id":"` + beadPlaceholder + `","type":"blocks","created_at":"2026-01-01T00:00:00.000Z","removed":false}`,
		`{"_type":"dependency","bead_id":"a1111111111111111111111111","depends_on_id":"` + gatePlaceholder + `","type":"blocks","created_at":"2026-01-01T00:00:00.000Z","removed":false}`,
	}
	mustImport(t, strings.Join(lines, "\n")+"\n")

	rawEvil := mustUnquoteJSON(t, evil)
	beadLabels, err := json.Marshal([]string{"milestone:m" + rawEvil, "x" + rawEvil})
	if err != nil {
		t.Fatal(err)
	}
	gateLabels, err := json.Marshal([]string{"kind:human", "wait:after:2000-01-01T00:00:00Z" + rawEvil})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()
	for _, st := range []struct {
		q    string
		args []any
	}{
		{`UPDATE beads SET claimed_by = ?, labels = ? WHERE id = ?`, []any{"who" + rawEvil, string(beadLabels), beadPlaceholder}},
		{`UPDATE beads SET labels = ? WHERE id = ?`, []any{string(gateLabels), gatePlaceholder}},
		{`UPDATE beads SET id = ? WHERE id = ?`, []any{mustUnquoteJSON(t, evilID), beadPlaceholder}},
		{`UPDATE beads SET id = ? WHERE id = ?`, []any{mustUnquoteJSON(t, evilGateID), gatePlaceholder}},
		{`UPDATE dependencies SET depends_on_id = ? WHERE depends_on_id = ?`, []any{mustUnquoteJSON(t, evilID), beadPlaceholder}},
		{`UPDATE dependencies SET depends_on_id = ? WHERE depends_on_id = ?`, []any{mustUnquoteJSON(t, evilGateID), gatePlaceholder}},
	} {
		if _, err := db.ExecContext(t.Context(), st.q, st.args...); err != nil {
			t.Fatalf("%s: %v", st.q, err)
		}
	}
}

func TestImportRejectsUntrustedIdentifiers(t *testing.T) {
	initBeadsDir(t)
	line := untrustedBeadLine(evilID, "t", "task", "open", "", `[]`)
	_, errS, code := runCmd(t, []string{"import", "-"}, line+"\n")
	if code == 0 || !strings.Contains(errS, `line 1: bead field "id" contains forbidden character U+000A`) {
		t.Fatalf("import of a control character in id: exit=%d stderr=%q; want a non-zero exit naming line 1 and the id field", code, errS)
	}
	out, _, _ := runCmd(t, []string{"list", "--all"}, "")
	if strings.Contains(out, untrustedFake) {
		t.Fatalf("rejected import still wrote a bead:\n%s", out)
	}
}

func assertNoInjectedOutput(t *testing.T, args []string, out string) {
	t.Helper()
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("%v output contains an ESC byte:\n%q", args, out)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " -"), untrustedFake) {
			t.Errorf("%v output has a line forged by an untrusted value: %q\nfull output:\n%q", args, line, out)
		}
	}
}

func TestUntrustedValuesDoNotForgeOutputLines(t *testing.T) {
	initBeadsDir(t)
	importUntrustedBeads(t)
	plain := "lm-a111"

	for _, args := range [][]string{
		{"list"},
		{"list", "--all"},
		{"ready"},
		{"blocked"},
		{"gate"},
		{"gate", "--all"},
		{"ahead"},
		{"show", plain},
		{"search", "plain"},
		{"dep", "add", plain, "lm-a111", "--type", "blocks", "--dry-run"},
	} {
		out, errS, _ := runCmd(t, args, "")
		assertNoInjectedOutput(t, args, out)
		assertNoInjectedOutput(t, args, errS)
	}
}

func TestUntrustedIDsDoNotForgeWriteCommandOutput(t *testing.T) {
	initBeadsDir(t)
	importUntrustedBeads(t)
	id := mustUnquoteJSON(t, evilID)
	gateID := mustUnquoteJSON(t, evilGateID)

	for _, args := range [][]string{
		{"update", "--reason", "r", id},
		{"cost", "add", "--in", "1", "--out", "1", id},
		{"rework", "add", "--cause", "ci", id},
		{"dep", "add", "lm-a111", id, "--type", "discovered-from"},
		{"gate", "resolve", "--reason", "go", gateID},
		{"close", "--reason", "done", id},
	} {
		out, errS, code := runCmd(t, args, "")
		if code != 0 {
			t.Fatalf("%q: exit code = %d, stderr = %q", args, code, errS)
		}
		assertNoInjectedOutput(t, args, out)
		assertNoInjectedOutput(t, args, errS)
	}
}

func mustUnquoteJSON(t *testing.T, s string) string {
	t.Helper()
	var v string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
