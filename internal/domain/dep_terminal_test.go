// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestUpsertLinkBlocksMissingPrereqIsLookupError(t *testing.T) {
	db := testDB(t)
	bead := newBead(t, db, "n0")
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	err = UpsertLink(context.Background(), tx, "u", bead, "missing", BlocksDepType, "n1", "")
	var term *TerminalBlockerError
	if !errors.Is(err, sql.ErrNoRows) || errors.As(err, &term) {
		t.Fatalf("err = %v, want a lookup error wrapping sql.ErrNoRows", err)
	}
}
