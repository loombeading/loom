// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestResolveIDFormA_PrefixHexPrefix(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a3f80000000000000000000000000000", "lm", "2026-01-01T00:00:00.000Z")

	got, err := ResolveID(context.Background(), db, "lm-a3f8")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a3f80000000000000000000000000000" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveIDFormB_HexOnly(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a3f80000000000000000000000000000", "lm", "2026-01-01T00:00:00.000Z")

	got, err := ResolveID(context.Background(), db, "a3f8")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a3f80000000000000000000000000000" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveIDFormD_FullCanonical(t *testing.T) {
	db := testDB(t)
	canonical := "a3f80000000000000000000000000000"
	insertBead(t, db, canonical, "lm", "2026-01-01T00:00:00.000Z")

	got, err := ResolveID(context.Background(), db, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonical {
		t.Fatalf("got %q", got)
	}
}

func TestResolveIDNotFound(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a3f80000000000000000000000000000", "lm", "2026-01-01T00:00:00.000Z")

	_, err := ResolveID(context.Background(), db, "lm-zzzz")
	if _, ok := errors.AsType[*NotFoundError](err); !ok {
		t.Fatalf("err = %v, want *NotFoundError", err)
	}
}

func TestResolveIDAmbiguous(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a3f81111111111111111111111111111", "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, "a3f82222222222222222222222222222", "lm", "2026-01-01T00:00:00.001Z")

	_, err := ResolveID(context.Background(), db, "a3f8")
	var ambErr *AmbiguousIDError
	if !errors.As(err, &ambErr) {
		t.Fatalf("err = %v, want *AmbiguousIDError", err)
	}
	if len(ambErr.Candidates) != 2 {
		t.Fatalf("candidates = %v, want 2 entries", ambErr.Candidates)
	}
}

func TestResolveIDNamespaceMismatch(t *testing.T) {
	db := testDB(t)
	canonical := "a3f80000000000000000000000000000"
	insertBead(t, db, canonical, "web", "2026-01-01T00:00:00.000Z")

	var notice bytes.Buffer
	got, err := ResolveID(WithStaleIDNotice(context.Background(), &notice), db, "lm-a3f8")
	if err != nil {
		t.Fatal(err)
	}
	if got != canonical {
		t.Fatalf("got %q, want %q", got, canonical)
	}
	want := "Note: lm-a3f8 uses a former namespace; the current display ID is web-a3f\n"
	if notice.String() != want {
		t.Fatalf("notice = %q, want %q", notice.String(), want)
	}
}

func TestResolveIDNamespaceMismatchAlias(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	child := "b3f80000000000000000000000000000"
	insertBead(t, db, root, "gr", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child, "gr", "2026-01-01T00:01:00.000Z")
	insertParentChild(t, db, child, root, "2026-01-01T00:01:00.000Z", false)

	var notice bytes.Buffer
	got, err := ResolveID(WithStaleIDNotice(context.Background(), &notice), db, "lm-a3f8.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != child {
		t.Fatalf("got %q, want %q", got, child)
	}
	want := "Note: lm-a3f8.1 uses a former namespace; the current display ID is gr-a3f.1\n"
	if notice.String() != want {
		t.Fatalf("notice = %q, want %q", notice.String(), want)
	}
}

func TestResolveIDMatchingNamespaceNoNotice(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a3f80000000000000000000000000000", "lm", "2026-01-01T00:00:00.000Z")

	var notice bytes.Buffer
	if _, err := ResolveID(WithStaleIDNotice(context.Background(), &notice), db, "lm-a3f8"); err != nil {
		t.Fatal(err)
	}
	if notice.Len() != 0 {
		t.Fatalf("notice = %q, want empty", notice.String())
	}
}

func TestResolveIDAliasRoundTrip(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	child1 := "b3f80000000000000000000000000000"
	child2 := "c3f80000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child1, "lm", "2026-01-01T00:01:00.000Z")
	insertBead(t, db, child2, "lm", "2026-01-01T00:02:00.000Z")
	insertParentChild(t, db, child1, root, "2026-01-01T00:01:00.000Z", false)
	insertParentChild(t, db, child2, root, "2026-01-01T00:02:00.000Z", false)

	alias, err := Alias(context.Background(), db, child2)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ResolveID(context.Background(), db, alias)
	if err != nil {
		t.Fatalf("ResolveID(%q): %v", alias, err)
	}
	if got != child2 {
		t.Fatalf("got %q, want %q", got, child2)
	}
}

func TestResolveIDAliasUnresolvable(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")

	_, err := ResolveID(context.Background(), db, "lm-a3f8.1")
	if _, ok := errors.AsType[*AliasUnresolvedError](err); !ok {
		t.Fatalf("err = %v, want *AliasUnresolvedError", err)
	}
}

func TestResolveIDNormalizesInput(t *testing.T) {
	db := testDB(t)
	canonical := NewCanonicalID()
	insertBead(t, db, canonical, "lm", "2026-01-01T00:00:00.000Z")

	prefixLen := min(len(canonical), 8)
	spelled := make([]byte, prefixLen)
	for i := range prefixLen {
		switch c := canonical[i]; {
		case c == '1':
			spelled[i] = 'I'
		case c == '0':
			spelled[i] = 'O'
		case c >= 'a' && c <= 'z':
			spelled[i] = c - 'a' + 'A'
		default:
			spelled[i] = c
		}
	}

	got, err := ResolveID(context.Background(), db, "lm-"+string(spelled))
	if err != nil {
		t.Fatalf("ResolveID(%q): %v", "lm-"+string(spelled), err)
	}
	if got != canonical {
		t.Fatalf("got %q, want %q", got, canonical)
	}
}

func TestResolveIDNoNamespaceNoSteps(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	child1 := "b3f80000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child1, "lm", "2026-01-01T00:01:00.000Z")
	insertParentChild(t, db, child1, root, "2026-01-01T00:01:00.000Z", false)

	got, err := ResolveID(context.Background(), db, "a3f8")
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("got %q, want %q", got, root)
	}
}

func TestResolveIDNoNamespaceAliasSteps(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	child1 := "b3f80000000000000000000000000000"
	child2 := "c3f80000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child1, "lm", "2026-01-01T00:01:00.000Z")
	insertBead(t, db, child2, "lm", "2026-01-01T00:02:00.000Z")
	insertParentChild(t, db, child1, root, "2026-01-01T00:01:00.000Z", false)
	insertParentChild(t, db, child2, root, "2026-01-01T00:02:00.000Z", false)

	got, err := ResolveID(context.Background(), db, "a3f8.2")
	if err != nil {
		t.Fatalf("ResolveID(%q): %v", "a3f8.2", err)
	}
	if got != child2 {
		t.Fatalf("got %q, want %q", got, child2)
	}

	gotNS, err := ResolveID(context.Background(), db, "lm-a3f8.2")
	if err != nil {
		t.Fatalf("ResolveID(%q): %v", "lm-a3f8.2", err)
	}
	if gotNS != got {
		t.Fatalf("got %q, want %q (same as %q)", gotNS, got, "a3f8.2")
	}

	gotUpper, err := ResolveID(context.Background(), db, "A3F8.2")
	if err != nil {
		t.Fatalf("ResolveID(%q): %v", "A3F8.2", err)
	}
	if gotUpper != got {
		t.Fatalf("got %q, want %q", gotUpper, got)
	}
}

func TestResolveIDNoNamespaceAliasAmbiguous(t *testing.T) {
	db := testDB(t)
	insertBead(t, db, "a3f81111111111111111111111111111", "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, "a3f82222222222222222222222222222", "lm", "2026-01-01T00:00:00.001Z")

	_, err := ResolveID(context.Background(), db, "a3f8.1")
	if _, ok := errors.AsType[*AmbiguousIDError](err); !ok {
		t.Fatalf("err = %v, want *AmbiguousIDError", err)
	}
}

func TestResolveIDNoNamespaceAliasOutOfRange(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")

	_, err := ResolveID(context.Background(), db, "a3f8.1")
	if _, ok := errors.AsType[*AliasUnresolvedError](err); !ok {
		t.Fatalf("err = %v, want *AliasUnresolvedError", err)
	}
}

func TestResolveIDMalformedAliasRejected(t *testing.T) {
	db := testDB(t)
	root := "a3f80000000000000000000000000000"
	child1 := "b3f80000000000000000000000000000"
	insertBead(t, db, root, "lm", "2026-01-01T00:00:00.000Z")
	insertBead(t, db, child1, "lm", "2026-01-01T00:01:00.000Z")
	insertParentChild(t, db, child1, root, "2026-01-01T00:01:00.000Z", false)

	for _, in := range []string{
		"lm-a3f8.0", "lm-a3f8.x", "lm-a3f8.", "lm-a3f8.1x",
		"lm-a3f8.01", "lm-a3f8.1.", "lm-a3f8..1", "lm-a3f8.+1", "lm-a3f8.-1",
		"lm-a3f8.9999999999", "lm-.1", ".1", "a3f8.x",
	} {
		_, err := ResolveID(context.Background(), db, in)
		if _, ok := errors.AsType[*AliasUnresolvedError](err); !ok {
			t.Errorf("ResolveID(%q) err = %v, want *AliasUnresolvedError", in, err)
		}
	}
}
