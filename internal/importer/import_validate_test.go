// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const (
	validBeadLine = `{"_type":"bead","id":"0000000000000000000000000a","namespace":"lm","title":"ok","status":"open","priority":2,"type":"task","created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}`
	badBeadID     = "0000000000000000000000000b"
)

func identRow(t *testing.T, typ, key string, value any) string {
	t.Helper()
	var row map[string]any
	switch typ {
	case "bead":
		row = map[string]any{"_type": "bead", "id": badBeadID, "namespace": "lm", "title": "t\nline", "description": "a\tb\r\nc", "status": "open", "priority": 2, "type": "task", "created_at": "2026-01-01T00:00:00.000Z", "updated_at": "2026-01-01T00:00:00.000Z"}
	case "dependency":
		row = map[string]any{"_type": "dependency", "bead_id": badBeadID, "depends_on_id": "0000000000000000000000000a", "type": "blocks", "created_at": "2026-01-01T00:00:00.000Z"}
	case "token_cost":
		row = map[string]any{"_type": "token_cost", "id": "0000000000000000000000000c", "bead_id": badBeadID, "actor": "a", "recorded_at": "2026-01-01T00:00:00.000Z", "tokens_in": 1, "tokens_out": 1}
	}
	if key != "" {
		row[key] = value
	}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func importInTx(t *testing.T, input string) (int, error) {
	t.Helper()
	db := testDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := Run(context.Background(), tx, strings.NewReader(input), Options{Actor: "test", Now: "2026-01-02T00:00:00.000Z"})
	var n int
	if err := tx.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM beads) + (SELECT count(*) FROM dependencies) + (SELECT count(*) FROM token_costs) + (SELECT count(*) FROM audit_log)`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	return n, runErr
}

func TestImportRejectsForbiddenCharactersInIdentifierFields(t *testing.T) {
	forbidden := []rune{0x00, 0x09, 0x0A, 0x0D, 0x1B, 0x1F, 0x7F, 0x80, 0x9F, 0x202A, 0x202E, 0x2066, 0x2069, 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF, 0xE0000, 0xE0041, 0xE007F}
	fields := []struct {
		typ, key string
		list     bool
	}{
		{"bead", "id", false},
		{"bead", "claimed_by", false},
		{"bead", "labels", true},
		{"bead", "external_refs", true},
		{"dependency", "bead_id", false},
		{"dependency", "depends_on_id", false},
		{"token_cost", "id", false},
		{"token_cost", "bead_id", false},
		{"token_cost", "actor", false},
	}
	for _, f := range fields {
		for _, r := range forbidden {
			s := "x" + string(r) + "y"
			var value any = s
			if f.list {
				value = []string{"ok", s}
			}
			input := validBeadLine + "\n" + identRow(t, f.typ, f.key, value) + "\n"
			n, err := importInTx(t, input)
			if err == nil {
				t.Errorf("%s.%s with U+%04X: import succeeded, want an error", f.typ, f.key, r)
				continue
			}
			want := fmt.Sprintf("line 2: %s field %q contains forbidden character U+%04X", f.typ, f.key, r)
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s.%s with U+%04X: error %q does not contain %q", f.typ, f.key, r, err, want)
			}
			if n != 0 {
				t.Errorf("%s.%s with U+%04X: %d row(s) written before the error, want 0", f.typ, f.key, r, n)
			}
		}
	}
}

func TestImportAcceptsBoundaryCharactersAndBodyNewlines(t *testing.T) {
	for _, r := range []rune{0x20, 0x7E, 0xA0, 0x2029, 0x202F, 0x2065, 0x206A, 0x200A, 0x200E, 0xFEFE, 0xDFFFF, 0xE0080, 'あ'} {
		s := "x" + string(r) + "y"
		input := identRow(t, "bead", "labels", []string{s}) + "\n" + identRow(t, "bead", "claimed_by", s) + "\n"
		if _, err := importInTx(t, input); err != nil {
			t.Errorf("U+%04X: import failed: %v", r, err)
		}
	}
	if n, err := importInTx(t, identRow(t, "bead", "", nil)+"\n"); err != nil || n == 0 {
		t.Fatalf("newline in title/description: err=%v rows=%d; want accepted", err, n)
	}
}
