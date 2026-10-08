// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	MinSeverity     = 1
	MaxSeverity     = 4
	DefaultSeverity = 3
)

func clockOrNow(c time.Time) time.Time {
	if c.IsZero() {
		return time.Now().UTC()
	}
	return c.UTC()
}

func nowOrDefault(now string) string {
	if now == "" {
		return FormatTime(time.Now())
	}
	return now
}

func FormatTime(t time.Time) string {
	return t.UTC().Format(filterTimeFormat)
}

func validateSeverity(n int) error {
	if n < MinSeverity || n > MaxSeverity {
		return &ValidationError{Msg: fmt.Sprintf("severity must be between %d and %d", MinSeverity, MaxSeverity)}
	}
	return nil
}

func checkExpedite(ctx context.Context, tx *sql.Tx, id string, span time.Duration, reason string, now time.Time) (string, error) {
	if reason == "" {
		return "", &ValidationError{Msg: "--expedite requires --reason"}
	}
	if span <= 0 {
		return "", &ValidationError{Msg: "--expedite must be a positive duration"}
	}
	coefficients, err := LoadCoefficients(ctx, tx)
	if err != nil {
		return "", err
	}
	if maxAge := coefficients.Duration(CoefficientExpediteMaxAge); span > maxAge {
		return "", &ValidationError{Msg: fmt.Sprintf("--expedite %s exceeds the coefficient expedite_max_age %s", span, maxAge)}
	}
	var open int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM beads WHERE expedite_until > ? AND status IN (?, ?) AND id <> ?`,
		FormatTime(now), StatusOpen, StatusInProgress, id).Scan(&open); err != nil {
		return "", fmt.Errorf("expedite: count open expedites: %w", err)
	}
	if maxOpen := coefficients.Int(CoefficientExpediteMaxOpen); open >= maxOpen {
		return "", &ValidationError{Msg: fmt.Sprintf("%d expedites are already active; the coefficient expedite_max_open is %d", open, maxOpen)}
	}
	return FormatTime(now.Add(span)), nil
}
