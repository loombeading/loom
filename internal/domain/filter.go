// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"fmt"
	"time"
)

const filterTimeFormat = "2006-01-02T15:04:05.000Z07:00"

func ParseFilterTime(s string) (string, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC().Format(filterTimeFormat), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", &ValidationError{Msg: fmt.Sprintf("invalid time %q: want RFC3339 or YYYY-MM-DD", s)}
	}
	return t.UTC().Format(filterTimeFormat), nil
}

func ResolveParentFilter(ctx context.Context, q Querier, input string, n ShortIDs) (string, string, error) {
	id, err := ResolveID(ctx, q, input)
	if err != nil {
		return "", "", err
	}
	b, err := GetBead(ctx, q, id)
	if err != nil {
		return "", "", err
	}
	return id, DisplayID(b.Namespace, id, n), nil
}
