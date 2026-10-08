// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"fmt"
	"time"
)

func Now(env func(string) string) (time.Time, error) {
	v := env("LM_NOW")
	if v == "" {
		return time.Now().UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, &ValidationError{Msg: fmt.Sprintf("invalid LM_NOW %q: want RFC3339", v)}
	}
	return t.UTC(), nil
}
