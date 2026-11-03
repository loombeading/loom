// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/domain"
)

func TestImportKeepsShortIDOfExistingBead(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, domain.CreateInput{Title: "before", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2000-01-01T00:00:00.000Z"})
	short := id[:5]
	if _, err := db.ExecContext(t.Context(), `UPDATE beads SET short_id = ? WHERE id = ?`, short, id); err != nil {
		t.Fatal(err)
	}

	out := make([]string, 0, 8)
	for line := range strings.SplitSeq(strings.TrimSpace(exportString(t, db)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row["id"] == id && row["title"] != nil {
			row["title"] = "after"
			row["updated_at"] = "2099-01-01T00:00:00.000Z"
			delete(row, "_set_at")
			b, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			line = string(b)
		}
		out = append(out, line)
	}
	importWithNow(t, db, strings.Join(out, "\n")+"\n", "2099-01-01T00:00:00.000Z")

	var gotShort, gotTitle string
	if err := db.QueryRowContext(t.Context(), `SELECT short_id, title FROM beads WHERE id = ?`, id).Scan(&gotShort, &gotTitle); err != nil {
		t.Fatal(err)
	}
	if gotTitle != "after" {
		t.Fatalf("title = %q, want %q (取り込みが既存行を更新していない)", gotTitle, "after")
	}
	if gotShort != short {
		t.Fatalf("short_id = %q, want %q", gotShort, short)
	}
}
