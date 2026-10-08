// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import "time"

func NowRFC3339Milli() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
