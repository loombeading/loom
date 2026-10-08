// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNow(t *testing.T) {
	t.Run("unset uses the real clock", func(t *testing.T) {
		before := time.Now().UTC()
		got, err := Now(envFunc(map[string]string{}))
		after := time.Now().UTC()
		if err != nil {
			t.Fatalf("Now() error = %v", err)
		}
		if got.Before(before) || got.After(after) {
			t.Errorf("Now() = %v, want between %v and %v", got, before, after)
		}
	})

	t.Run("LM_NOW overrides the clock", func(t *testing.T) {
		got, err := Now(envFunc(map[string]string{"LM_NOW": "2026-01-02T03:04:05Z"}))
		if err != nil {
			t.Fatalf("Now() error = %v", err)
		}
		want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("Now() = %v, want %v", got, want)
		}
	})

	t.Run("LM_NOW normalizes non-UTC offsets to UTC", func(t *testing.T) {
		got, err := Now(envFunc(map[string]string{"LM_NOW": "2026-01-02T12:04:05+09:00"}))
		if err != nil {
			t.Fatalf("Now() error = %v", err)
		}
		want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("Now() = %v, want %v", got, want)
		}
	})

	t.Run("empty LM_NOW falls through to the real clock", func(t *testing.T) {
		if _, err := Now(envFunc(map[string]string{"LM_NOW": ""})); err != nil {
			t.Errorf("Now() error = %v, want nil", err)
		}
	})

	t.Run("invalid LM_NOW is a ValidationError", func(t *testing.T) {
		_, err := Now(envFunc(map[string]string{"LM_NOW": "not-a-time"}))
		if err == nil {
			t.Fatal("Now() error = nil, want an error")
		}
		if !errors.As(err, new(*ValidationError)) {
			t.Errorf("Now() error type = %T, want *ValidationError", err)
		}
	})
}
