// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/loombeading/loom/internal/storage"
)

type coefficientSnapshot struct {
	value string
	setAt string
}

func coefficientDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), beadsDir(t), storage.OpenOptions{Env: func(string) string { return "" }, Actor: "test"})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func readCoefficient(t *testing.T, db *storage.DB, key string) coefficientSnapshot {
	t.Helper()
	var s coefficientSnapshot
	if err := db.SQL.QueryRowContext(context.Background(), `SELECT value, value_set_at FROM coefficients WHERE key = ?`, key).Scan(&s.value, &s.setAt); err != nil {
		t.Fatalf("read coefficient %s: %v", key, err)
	}
	return s
}

func countCoefficientAudit(t *testing.T, db *storage.DB) int {
	t.Helper()
	var n int
	if err := db.SQL.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE kind = 'coefficient'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCoefficientShowListsEveryKeyInDictionaryOrder(t *testing.T) {
	initBeadsDir(t)
	out, errS, code := runCmd(t, []string{"coefficient", "show"}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "## Coefficients" {
		t.Fatalf("first line = %q, want %q", lines[0], "## Coefficients")
	}
	if len(lines) != 19 {
		t.Fatalf("lines = %d, want 1 heading + 18 keys:\n%s", len(lines), out)
	}
	keys := make([]string, 0, 18)
	for _, l := range lines[1:] {
		rest, ok := strings.CutPrefix(l, "- ")
		if !ok {
			t.Fatalf("line %q does not start with %q", l, "- ")
		}
		key, _, ok := strings.Cut(rest, " = ")
		if !ok {
			t.Fatalf("line %q has no %q", l, " = ")
		}
		keys = append(keys, key)
	}
	if !slices.IsSorted(keys) {
		t.Errorf("keys are not in dictionary order: %v", keys)
	}
	if want := "- due_lead = 72h (set 1970-01-01T00:00:00.000Z)"; !slices.Contains(lines, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

func TestCoefficientShowEmptyTablePrintsNone(t *testing.T) {
	initBeadsDir(t)
	db := coefficientDB(t)
	if _, err := db.SQL.ExecContext(context.Background(), `DELETE FROM coefficients`); err != nil {
		t.Fatal(err)
	}
	out, errS, code := runCmd(t, []string{"coefficient", "show"}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if want := "## Coefficients\n(none)\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestCoefficientShowRejectsArguments(t *testing.T) {
	initBeadsDir(t)
	if _, _, code := runCmd(t, []string{"coefficient", "show", "due_lead"}, ""); code != 1 {
		t.Errorf("show with an argument: exit code = %d, want 1", code)
	}
}

func TestCoefficientSetWritesValueAndAuditRow(t *testing.T) {
	initBeadsDir(t)
	db := coefficientDB(t)
	out, errS, code := runCmd(t, []string{"coefficient", "set", "due_lead", "48h", "--reason", "tune"}, "")
	if code != 0 {
		t.Fatalf("set: exit code = %d, stderr = %q", code, errS)
	}
	if want := "- Coefficient set: due_lead = 48h (was 72h)\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	got := readCoefficient(t, db, "due_lead")
	if got.value != "48h" || got.setAt == "1970-01-01T00:00:00.000Z" {
		t.Errorf("coefficient = %+v, want value 48h with a new value_set_at", got)
	}
	var occurredAt, actor, field, newValue, reason, origin string
	var beadID sql.NullString
	err := db.SQL.QueryRowContext(context.Background(),
		`SELECT occurred_at, actor, bead_id, field, new_value, reason, origin FROM audit_log WHERE kind = 'coefficient'`).
		Scan(&occurredAt, &actor, &beadID, &field, &newValue, &reason, &origin)
	if err != nil {
		t.Fatalf("audit row: %v", err)
	}
	if beadID.Valid || field != "due_lead" || newValue != "48h" || reason != "tune" || actor != "test-actor" || origin != "local" || occurredAt != got.setAt {
		t.Errorf("audit row = (%q, %q, %v, %q, %q, %q, %q), want a bead-less due_lead=48h row by test-actor at value_set_at", occurredAt, actor, beadID, field, newValue, reason, origin)
	}

	if _, errS, code := runCmd(t, []string{"coefficient", "set", "due_lead", "48h", "--reason", "reconfirm"}, ""); code != 0 {
		t.Fatalf("set again: exit code = %d, stderr = %q", code, errS)
	}
	if n := countCoefficientAudit(t, db); n != 2 {
		t.Errorf("audit rows after re-setting the same value = %d, want 2", n)
	}
}

func TestCoefficientSetReadsReasonFromStdin(t *testing.T) {
	initBeadsDir(t)
	db := coefficientDB(t)
	if _, errS, code := runCmd(t, []string{"coefficient", "set", "due_lead", "48h", "--reason", "-"}, "from stdin"); code != 0 {
		t.Fatalf("set: exit code = %d, stderr = %q", code, errS)
	}
	var reason string
	if err := db.SQL.QueryRowContext(context.Background(), `SELECT reason FROM audit_log WHERE kind = 'coefficient'`).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "from stdin") {
		t.Errorf("reason = %q, want it to contain %q", reason, "from stdin")
	}
}

func TestCoefficientSetRejectsWithoutWriting(t *testing.T) {
	initBeadsDir(t)
	db := coefficientDB(t)
	cases := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"no reason", []string{"coefficient", "set", "due_lead", "48h"}, "", "reason"},
		{"empty reason", []string{"coefficient", "set", "due_lead", "48h", "--reason", ""}, "", "reason"},
		{"empty stdin reason", []string{"coefficient", "set", "due_lead", "48h", "--reason", "-"}, "", "reason"},
		{"unknown key", []string{"coefficient", "set", "no_such_key", "1", "--reason", "r"}, "", "cutoff_priority, decay_after, due_lead"},
		{"out of range", []string{"coefficient", "set", "cutoff_priority", "9", "--reason", "r"}, "", "between 1 and 4"},
		{"non-positive duration", []string{"coefficient", "set", "due_lead", "0h", "--reason", "r"}, "", "positive duration"},
		{"ceiling above floor", []string{"coefficient", "set", "severity_2_ceiling", "3", "--reason", "r"}, "", "severity_2_ceiling"},
		{"floor below ceiling", []string{"coefficient", "set", "severity_3_floor", "1", "--reason", "r"}, "", "severity_3_floor"},
		{"missing value", []string{"coefficient", "set", "due_lead", "--reason", "r"}, "", "requires"},
	}
	snapshot := map[string]coefficientSnapshot{
		"due_lead":           readCoefficient(t, db, "due_lead"),
		"cutoff_priority":    readCoefficient(t, db, "cutoff_priority"),
		"severity_2_ceiling": readCoefficient(t, db, "severity_2_ceiling"),
		"severity_3_floor":   readCoefficient(t, db, "severity_3_floor"),
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errS, code := runCmd(t, tc.args, tc.stdin)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (stdout = %q, stderr = %q)", code, out, errS)
			}
			if !strings.Contains(errS, tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", errS, tc.want)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			for k, s := range snapshot {
				if got := readCoefficient(t, db, k); got != s {
					t.Errorf("%s changed: %+v, want %+v", k, got, s)
				}
			}
			if n := countCoefficientAudit(t, db); n != 0 {
				t.Errorf("audit rows = %d, want 0", n)
			}
		})
	}
}

func TestCoefficientSetOnReadOnlyDatabaseRejects(t *testing.T) {
	initBeadsDir(t)
	makeDBReadOnly(t)
	out, errS, code := runCmd(t, []string{"coefficient", "set", "due_lead", "48h", "--reason", "r"}, "")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout = %q, stderr = %q)", code, out, errS)
	}
	if out != "" || !strings.Contains(errS, "read-only") {
		t.Errorf("stdout = %q, stderr = %q, want empty stdout and a read-only rejection on stderr", out, errS)
	}
	db := coefficientDB(t)
	if got := readCoefficient(t, db, "due_lead"); got.value != "72h" {
		t.Errorf("due_lead = %+v, want it unchanged at 72h", got)
	}
}

func TestCoefficientHistoryReconstructsValueAtTime(t *testing.T) {
	initBeadsDir(t)
	db := coefficientDB(t)
	for _, v := range []string{"48h", "36h"} {
		if _, errS, code := runCmd(t, []string{"coefficient", "set", "due_lead", v, "--reason", "r"}, ""); code != 0 {
			t.Fatalf("set %s: exit code = %d, stderr = %q", v, code, errS)
		}
	}
	rows, err := db.SQL.QueryContext(context.Background(),
		`SELECT occurred_at, new_value FROM audit_log WHERE kind = 'coefficient' AND field = 'due_lead' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var times, values []string
	for rows.Next() {
		var at, v string
		if err := rows.Scan(&at, &v); err != nil {
			t.Fatal(err)
		}
		times, values = append(times, at), append(values, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(values, []string{"48h", "36h"}) {
		t.Fatalf("audited values = %v, want [48h 36h]", values)
	}
	if times[0] > times[1] {
		t.Fatalf("audit times out of order: %v", times)
	}
	valueAt := func(t0 string) string {
		got := "72h"
		for i := range times {
			if times[i] <= t0 {
				got = values[i]
			}
		}
		return got
	}
	if got := valueAt("1999-01-01T00:00:00.000Z"); got != "72h" {
		t.Errorf("value before the first change = %q, want the initial 72h", got)
	}
	if got := valueAt(times[1]); got != "36h" {
		t.Errorf("value at the last change = %q, want 36h", got)
	}
}

func TestCoefficientExportImportRoundTripAndImportRejectsInvertedBounds(t *testing.T) {
	initBeadsDir(t)
	if _, errS, code := runCmd(t, []string{"coefficient", "set", "due_lead", "48h", "--reason", "r"}, ""); code != 0 {
		t.Fatalf("set: exit code = %d, stderr = %q", code, errS)
	}
	var coefLines []string
	for _, l := range exportLines(t) {
		if strings.Contains(l, `"_type":"coefficient"`) {
			coefLines = append(coefLines, l)
		}
	}
	if len(coefLines) != 18 {
		t.Fatalf("coefficient rows in export = %d, want 18", len(coefLines))
	}

	initBeadsDir(t)
	if _, errS, code := runCmd(t, []string{"import", "-"}, strings.Join(coefLines, "\n")+"\n"); code != 0 {
		t.Fatalf("import: exit code = %d, stderr = %q", code, errS)
	}
	db := coefficientDB(t)
	want := readCoefficient(t, db, "due_lead")
	if want.value != "48h" || want.setAt == "1970-01-01T00:00:00.000Z" {
		t.Errorf("imported due_lead = %+v, want 48h with the exported value_set_at", want)
	}
	for _, l := range exportLines(t) {
		if strings.Contains(l, `"key":"due_lead"`) && !slices.Contains(coefLines, l) {
			t.Errorf("round trip changed the row: %s", l)
		}
	}

	bad := fmt.Sprintf(`{"_set_at":{"value":"2030-01-01T00:00:00.000Z"},"_type":"coefficient","key":%q,"value":"3"}`, "severity_2_ceiling") + "\n"
	_, errS, code := runCmd(t, []string{"import", "-"}, bad)
	if code == 0 || !strings.Contains(errS, "severity_2_ceiling") {
		t.Errorf("import of ceiling above floor: exit code = %d, stderr = %q, want a rejection naming severity_2_ceiling", code, errS)
	}
	if got := readCoefficient(t, db, "severity_2_ceiling"); got.value != "1" {
		t.Errorf("severity_2_ceiling = %+v after a rejected import, want it unchanged at 1", got)
	}
}

func TestCoefficientRequiresSubcommand(t *testing.T) {
	initBeadsDir(t)
	if _, _, code := runCmd(t, []string{"coefficient"}, ""); code != 1 {
		t.Errorf("bare coefficient: exit code = %d, want 1", code)
	}
	if _, _, code := runCmd(t, []string{"coefficient", "nope"}, ""); code != 1 {
		t.Errorf("unknown subcommand: exit code = %d, want 1", code)
	}
}

func TestCoefficientCommandsFailWhenTheDatabaseCannotBeOpened(t *testing.T) {
	initBeadsDir(t)
	t.Setenv("LM_DIR", t.TempDir()+"/missing")
	for _, args := range [][]string{
		{"coefficient", "show"},
		{"coefficient", "set", "due_lead", "48h", "--reason", "r"},
	} {
		out, errS, code := runCmd(t, args, "")
		if code != 1 || out != "" || errS == "" {
			t.Errorf("%v: exit code = %d, stdout = %q, stderr = %q, want exit 1 with only stderr", args, code, out, errS)
		}
	}
}

func TestCoefficientShowFailsWhenTheTableCannotBeRead(t *testing.T) {
	initBeadsDir(t)
	db := coefficientDB(t)
	if _, err := db.SQL.ExecContext(context.Background(), `DROP TABLE coefficients`); err != nil {
		t.Fatal(err)
	}
	out, errS, code := runCmd(t, []string{"coefficient", "show"}, "")
	if code != 1 || out != "" || !strings.Contains(errS, "coefficients") {
		t.Errorf("exit code = %d, stdout = %q, stderr = %q, want exit 1 with a coefficients error on stderr", code, out, errS)
	}
}

func TestCoefficientSetFailsWhenStdinReasonCannotBeRead(t *testing.T) {
	initBeadsDir(t)
	var out, errBuf strings.Builder
	code := runCoefficientSet([]string{"due_lead", "48h", "--reason", "-"}, iotest.ErrReader(errors.New("boom")), &out, &errBuf)
	if code != 1 || out.String() != "" || !strings.Contains(errBuf.String(), "read stdin") {
		t.Errorf("exit code = %d, stdout = %q, stderr = %q, want exit 1 with a read stdin error on stderr", code, out.String(), errBuf.String())
	}
	db := coefficientDB(t)
	if got := readCoefficient(t, db, "due_lead"); got.value != "72h" {
		t.Errorf("due_lead = %+v, want it unchanged at 72h", got)
	}
}
