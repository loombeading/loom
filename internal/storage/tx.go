// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"database/sql"
)

func (d *DB) WithWrite(ctx context.Context, fn func(*sql.Tx) error) error {
	if d.ReadOnly {
		return ErrReadOnlyWrite
	}

	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
