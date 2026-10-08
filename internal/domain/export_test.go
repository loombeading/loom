// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func exportLines(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var buf bytes.Buffer
	if err := Export(context.Background(), db, &buf); err != nil {
		t.Fatalf("Export: %v", err)
	}
	out := buf.String()
	if out == "" {
		return nil
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("Export output does not end in a single trailing newline: %q", out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	for _, l := range lines {
		if strings.Contains(l, "\n") {
			t.Fatalf("a line contains an embedded newline: %q", l)
		}
	}
	return lines
}

func decodeLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("decode line %q: %v", line, err)
	}
	return m
}

func TestExportDeterministicAcrossRuns(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, a, b, BlocksDepType))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: a, TokensIn: 1, TokensOut: 2, Actor: "u", Now: "n2"}))

	var first, second bytes.Buffer
	if err := Export(context.Background(), db, &first); err != nil {
		t.Fatal(err)
	}
	if err := Export(context.Background(), db, &second); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatalf("Export is not byte-identical across runs:\n--- first ---\n%s\n--- second ---\n%s", first.String(), second.String())
	}
}

func TestExportUnchangedRecordsHaveUnchangedLines(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")

	before := exportLines(t, db)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateBead(context.Background(), tx, UpdateInput{
		ID: a, Title: "changed", Priority: -1, Actor: "u", Now: "n2",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	after := exportLines(t, db)

	beforeByID := lineByID(t, before)
	afterByID := lineByID(t, after)

	if beforeByID[b] != afterByID[b] {
		t.Errorf("unrelated record %s's line changed:\nbefore: %s\nafter:  %s", b, beforeByID[b], afterByID[b])
	}
	if beforeByID[a] == afterByID[a] {
		t.Errorf("modified record %s's line did not change", a)
	}
}

func lineByID(t *testing.T, lines []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(lines))
	for _, l := range lines {
		m := decodeLine(t, l)
		if m["_type"] != "bead" {
			continue
		}
		out[asString(t, m["id"])] = l
	}
	return out
}

func asString(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("value %#v is not a string", v)
	}
	return s
}

func TestExportOmitsNullFieldsAndAlwaysEmitsLabels(t *testing.T) {
	db := testDB(t)
	id := newBead(t, db, "n0")

	lines := exportLines(t, db)
	row := lineByID(t, lines)[id]
	m := decodeLine(t, row)

	for _, key := range []string{
		"description", "owner", "claimed_by", "summary",
		"reasoning_depth", "closed_at",
	} {
		if v, ok := m[key]; ok {
			t.Errorf("unset field %q present with value %v, want key omitted", key, v)
		}
	}

	at, ok := m["_set_at"].(map[string]any)
	if !ok {
		t.Fatalf(`"_set_at" key missing or not an object: %v`, m["_set_at"])
	}
	for _, key := range []string{
		"description", "owner", "claimed_by",
		"summary", "external_refs", "reasoning_depth", "closed_at",
	} {
		if v, ok := at[key]; ok {
			t.Errorf("unset field %q present in _at with value %v, want key omitted", key, v)
		}
	}

	labels, ok := m["labels"]
	if !ok {
		t.Fatal(`"labels" key missing, want "[]"`)
	}
	if arr, ok := labels.([]any); !ok || len(arr) != 0 {
		t.Errorf("labels = %#v, want empty array", labels)
	}

	externalRefs, ok := m["external_refs"]
	if !ok {
		t.Fatal(`"external_refs" key missing, want "[]"`)
	}
	if arr, ok := externalRefs.([]any); !ok || len(arr) != 0 {
		t.Errorf("external_refs = %#v, want empty array", externalRefs)
	}

	for _, key := range []string{"namespace", "title", "status", "priority", "type"} {
		if _, ok := at[key]; !ok {
			t.Errorf("field-level LWW timestamp _at.%q missing for a set field", key)
		}
	}
}

func TestExportLabelsSortedDictionaryOrder(t *testing.T) {
	db := testDB(t)
	id := mustCreate(t, db, CreateInput{
		Title: "x", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "n0",
		Labels: []string{"z", "a", "m"},
	})

	m := decodeLine(t, lineByID(t, exportLines(t, db))[id])
	labels, ok := m["labels"].([]any)
	if !ok {
		t.Fatalf("labels = %#v, want array", m["labels"])
	}
	got := make([]string, len(labels))
	for i, l := range labels {
		got[i] = asString(t, l)
	}
	want := []string{"a", "m", "z"}
	if !sort.StringsAreSorted(got) || len(got) != len(want) {
		t.Errorf("labels = %v, want sorted %v", got, want)
	}
}

func TestExportIgnoresNamespaceFilterDefault(t *testing.T) {
	db := testDB(t)
	a := mustCreate(t, db, CreateInput{Title: "a", Priority: 2, BeadType: "task", Namespace: "aaa", Actor: "u", Now: "n0"})
	b := mustCreate(t, db, CreateInput{Title: "b", Priority: 2, BeadType: "task", Namespace: "bbb", Actor: "u", Now: "n1"})

	byID := lineByID(t, exportLines(t, db))
	if _, ok := byID[a]; !ok {
		t.Errorf("bead %s (namespace aaa) missing from export", a)
	}
	if _, ok := byID[b]; !ok {
		t.Errorf("bead %s (namespace bbb) missing from export", b)
	}
}

func TestExportOrderAndTypesAndNoAuditLog(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, b, a, BlocksDepType))
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: a, TokensIn: 1, TokensOut: 2, Actor: "u", Now: "n2"}))

	var auditCount int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_log`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount == 0 {
		t.Fatal("test setup produced no audit_log rows; the no-audit assertion below would be vacuous")
	}

	lines := exportLines(t, db)
	var sawTypes []string
	lastType := ""
	lastID := ""
	for _, l := range lines {
		m := decodeLine(t, l)
		typ := asString(t, m["_type"])
		if typ == "audit" || typ == "audit_log" {
			t.Fatalf("audit_log row leaked into export: %s", l)
		}
		if typ != lastType {
			sawTypes = append(sawTypes, typ)
			lastType = typ
			lastID = ""
		} else {
			var id string
			switch typ {
			case "bead", "token_cost":
				id = asString(t, m["id"])
			case "coefficient":
				id = asString(t, m["key"])
			case "dependency":
				id = asString(t, m["bead_id"]) + "\x00" + asString(t, m["depends_on_id"]) + "\x00" + asString(t, m["type"])
			}
			if id < lastID {
				t.Errorf("%s rows out of ascending order: %q before %q", typ, lastID, id)
			}
			lastID = id
		}
	}

	want := []string{"bead", "dependency", "token_cost", "coefficient"}
	if len(sawTypes) != len(want) {
		t.Fatalf("_type block order = %v, want %v", sawTypes, want)
	}
	for i, w := range want {
		if sawTypes[i] != w {
			t.Errorf("_type block order = %v, want %v", sawTypes, want)
		}
	}
}

func TestExportDependencyRowShape(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	b := newBead(t, db, "n1")
	mustNoErr(t, mustAddLink(t, db, a, b, BlocksDepType))

	var depLine string
	for _, l := range exportLines(t, db) {
		m := decodeLine(t, l)
		if m["_type"] == "dependency" {
			depLine = l
			break
		}
	}
	if depLine == "" {
		t.Fatal("no dependency row in export")
	}
	m := decodeLine(t, depLine)
	if m["bead_id"] != a || m["depends_on_id"] != b || m["type"] != BlocksDepType {
		t.Errorf("dependency row = %v, want bead_id=%s depends_on_id=%s type=%s", m, a, b, BlocksDepType)
	}
	if m["removed"] != false {
		t.Errorf("removed = %v, want false", m["removed"])
	}
	at, ok := m["_set_at"].(map[string]any)
	if !ok {
		t.Fatalf(`"_set_at" key missing or not an object: %v`, m["_set_at"])
	}
	if v, ok := at["removed"]; ok {
		t.Errorf("_at.removed present for a never-removed link: %v", v)
	}
}

func TestExportTokenCostRowShape(t *testing.T) {
	db := testDB(t)
	a := newBead(t, db, "n0")
	mustNoErr(t, mustAddTokenCost(t, db, AddTokenCostInput{BeadID: a, TokensIn: 1, TokensOut: 2, Actor: "u", Now: "n1"}))

	var tcLine string
	for _, l := range exportLines(t, db) {
		if decodeLine(t, l)["_type"] == "token_cost" {
			tcLine = l
			break
		}
	}
	if tcLine == "" {
		t.Fatal("no token_cost row in export")
	}
	m := decodeLine(t, tcLine)
	for _, key := range []string{"id", "bead_id", "actor", "recorded_at", "tokens_in", "tokens_out"} {
		if _, ok := m[key]; !ok {
			t.Errorf("token_cost row missing key %q: %v", key, m)
		}
	}
}
