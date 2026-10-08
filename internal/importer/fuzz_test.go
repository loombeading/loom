// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/loombeading/loom/internal/storage"
)

type fuzzTB interface {
	Helper()
	TempDir() string
	Fatalf(format string, args ...any)
	Cleanup(func())
}

func openFuzzDB(t fuzzTB) *storage.DB {
	t.Helper()
	dir := filepath.Join(t.TempDir(), storage.BeadsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := storage.Open(context.Background(), dir, storage.OpenOptions{
		Env:         func(string) string { return "" },
		Actor:       "fuzz",
		AllowCreate: true,
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

var canonicalListSeeds = [][]byte{
	nil,
	[]byte("[]"),
	[]byte(`["b","a","a"]`),
	[]byte(`["  x "]`),
	[]byte("{}"),
	[]byte(`"s"`),
	[]byte("[1]"),
}

func fuzzCanonicalList(f *testing.F, name string, fn func(json.RawMessage) (string, []string, error)) {
	f.Helper()
	for _, s := range canonicalListSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		b, list, err := fn(json.RawMessage(data))
		if len(data) == 0 {
			if err == nil && b != "" {
				t.Fatalf("%s(empty) = %q, %v, nil; want err != nil or b == \"\"", name, b, list)
			}
			return
		}
		if err != nil {
			return
		}
		var parsed []string
		if uerr := json.Unmarshal([]byte(b), &parsed); uerr != nil {
			t.Fatalf("%s(%q) = %q: json.Unmarshal failed: %v", name, data, b, uerr)
		}
		if !reflect.DeepEqual(parsed, list) {
			t.Fatalf("%s(%q) = %q: unmarshaled %#v != returned list %#v (order must be preserved)", name, data, b, parsed, list)
		}
		b2, _, err2 := fn(json.RawMessage(b))
		if err2 != nil {
			t.Fatalf("%s(%q) second call error: %v", name, b, err2)
		}
		if b2 != b {
			t.Fatalf("%s not idempotent: %s(%q) = %q, %s(%q) = %q", name, name, data, b, name, b, b2)
		}
	})
}

func FuzzCanonicalLabels(f *testing.F) {
	fuzzCanonicalList(f, "canonicalLabels", canonicalLabels)
}

func FuzzCanonicalExternalRefs(f *testing.F) {
	fuzzCanonicalList(f, "canonicalExternalRefs", canonicalExternalRefs)
}

var fuzzRunJSONLSeed = `{"_type":"bead","id":"00000000000000000000","namespace":"lm","title":"after","status":"in_progress","priority":2,"type":"task","labels":[],"created_at":"2000-01-01T00:00:00.000Z","updated_at":"2000-06-01T00:00:00.000Z","_set_at":{"title":"2000-01-01T00:00:00.000Z","status":"2099-01-01T00:00:00.000Z"}}` + "\n"

func FuzzRun(f *testing.F) {
	for _, s := range []string{
		fuzzRunJSONLSeed,
		"",
		"{}",
		`{"_type":"bead"}`,
		`{"_type":"dependency"}`,
		"not json",
		`{"_type":"bead","id":"x","labels":"s"}`,
	} {
		f.Add([]byte(s))
	}

	db := openFuzzDB(f)

	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		tx, err := db.SQL.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		sum, err := Run(ctx, tx, bytes.NewReader(data), Options{Actor: "fuzz", Now: "2026-01-01T00:00:00Z"})
		if err == nil && sum == nil {
			t.Fatalf("Run(%q) = nil, nil; want a non-nil summary when err is nil", data)
		}
	})
}
