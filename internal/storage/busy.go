// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"errors"

	"modernc.org/sqlite"
)

const sqliteBusyPrimaryCode = 5

func IsBusyErr(err error) bool {
	if sqliteErr, ok := errors.AsType[*sqlite.Error](err); ok {
		return sqliteErr.Code()&0xff == sqliteBusyPrimaryCode
	}
	return false
}
