// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCreatePartialNonexistentSourceReturnsErrBeadNotFound(t *testing.T) {
	db := testDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = CreatePartial(context.Background(), tx, PartialInput{
		SourceID: "no-such-id", Title: "leftover", Actor: "u", Now: "n1",
	})
	if !errors.Is(err, ErrBeadNotFound) {
		t.Fatalf("err = %v, want ErrBeadNotFound", err)
	}
}

func TestCreatePartialWrapsLookupSourceError(t *testing.T) {
	db := testDB(t)
	src := mustCreate(t, db, CreateInput{Title: "src", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = CreatePartial(ctx, tx, PartialInput{SourceID: src, Title: "leftover", Actor: "u", Now: "n1"})
	if err == nil {
		t.Fatal("want an error from a canceled context")
	}
	if errors.Is(err, ErrBeadNotFound) {
		t.Fatalf("err = %v, want the generic lookup error, not ErrBeadNotFound", err)
	}
	if !strings.Contains(err.Error(), "create partial: lookup source:") {
		t.Errorf("err = %q, want it to contain %q", err.Error(), "create partial: lookup source:")
	}
}

func TestCreatePartialWrapsDependencyErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup string
		want  string
	}{
		{"LookupParent", `DROP TABLE dependencies`, "create partial: lookup parent:"},
		{"BlocksLink", `
			CREATE TRIGGER block_blocks_insert BEFORE INSERT ON dependencies
			WHEN NEW.type = 'blocks'
			BEGIN SELECT RAISE(ABORT, 'boom'); END`, "add link: insert:"},
		{"DiscoveredFromLink", `
			CREATE TRIGGER block_discovered_from_insert BEFORE INSERT ON dependencies
			WHEN NEW.type = 'discovered-from'
			BEGIN SELECT RAISE(ABORT, 'boom'); END`, "add link: insert:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			src := mustCreate(t, db, CreateInput{Title: "src", Priority: 2, BeadType: "task", Namespace: "lm", Actor: "u", Now: "2026-01-01T00:00:00.000Z"})

			if _, err := db.ExecContext(t.Context(), tc.setup); err != nil {
				t.Fatalf("setup: %v", err)
			}

			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()

			_, err = CreatePartial(context.Background(), tx, PartialInput{SourceID: src, Title: "leftover", Actor: "u", Now: "n1"})
			if err == nil {
				t.Fatal("want an error from the broken dependencies table")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestCreatePartialWrapsCreateBeadError(t *testing.T) {
	db := testDB(t)
	src := "src1"
	insertBead(t, db, src, "not valid ns", "2026-01-01T00:00:00.000Z")

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = CreatePartial(context.Background(), tx, PartialInput{SourceID: src, Title: "leftover", Actor: "u", Now: "n1"})
	if err == nil {
		t.Fatal("want an error when the source's namespace is invalid")
	}
	if _, ok := errors.AsType[*ValidationError](err); !ok {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
}
