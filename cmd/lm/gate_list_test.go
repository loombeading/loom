// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/loombeading/loom/internal/domain"
)

func TestGateWaitProgressesReturnsQueryError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	got, err := gateWaitProgresses(context.Background(), db, []domain.GateListEntry{{ID: "g1"}})
	if err == nil {
		t.Fatalf("gateWaitProgresses on an empty schema = %v, want error", got)
	}
}

func TestWriteGateEntryReturnsAuxError(t *testing.T) {
	want := errors.New("aux failed")
	sec := gateSection{aux: func(domain.GateListEntry, string) ([]string, error) { return nil, want }}
	line := func(domain.GateListEntry) (string, error) { return "- g1", nil }
	var out bytes.Buffer
	if err := writeGateEntry(&out, sec, domain.GateListEntry{ID: "g1"}, nil, line); !errors.Is(err, want) {
		t.Fatalf("writeGateEntry err = %v, want %v", err, want)
	}
	if out.String() != "- g1\n" {
		t.Fatalf("stdout = %q, want the Gate line before the aux error", out.String())
	}
}
