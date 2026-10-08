// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	CoefficientExpediteMaxOpen = "expedite_max_open"
	CoefficientExpediteMaxAge  = "expedite_max_age"
	CoefficientCutoffPriority  = "cutoff_priority"
)

var coefficientDurationKeys = []string{
	"promote_after", "promote_after_gate", "decay_after", "hold_after", "cancel_after",
	"redetect_window", "due_lead", CoefficientExpediteMaxAge,
}

func CoefficientKeys() []string {
	keys := append([]string{}, coefficientDurationKeys...)
	keys = append(keys, CoefficientExpediteMaxOpen, CoefficientCutoffPriority, "severity_1_ceiling")
	for n := 2; n <= 4; n++ {
		keys = append(keys, fmt.Sprintf("severity_%d_ceiling", n), fmt.Sprintf("severity_%d_floor", n))
	}
	return keys
}

func ParseSpan(s string) (time.Duration, error) {
	if num, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}

func ValidateCoefficientValue(key, value string) error {
	for _, k := range coefficientDurationKeys {
		if k == key {
			d, err := ParseSpan(value)
			if err != nil {
				return &ValidationError{Msg: fmt.Sprintf("coefficient %s: %v", key, err)}
			}
			if d <= 0 {
				return &ValidationError{Msg: fmt.Sprintf("coefficient %s must be a positive duration, got %q", key, value)}
			}
			return nil
		}
	}
	known := false
	for _, k := range CoefficientKeys() {
		known = known || k == key
	}
	if !known {
		return &ValidationError{Msg: fmt.Sprintf("unknown coefficient key %q", key)}
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return &ValidationError{Msg: fmt.Sprintf("coefficient %s must be an integer, got %q", key, value)}
	}
	lo, hi := 1, MaxPriority
	if key == CoefficientExpediteMaxOpen {
		hi = 1 << 30
	}
	if n < lo || n > hi {
		return &ValidationError{Msg: fmt.Sprintf("coefficient %s must be between %d and %d, got %d", key, lo, hi, n)}
	}
	return nil
}

type Coefficients map[string]string

func LoadCoefficients(ctx context.Context, q Querier) (Coefficients, error) {
	rows, err := q.QueryContext(ctx, `SELECT key, value FROM coefficients`)
	if err != nil {
		return nil, fmt.Errorf("load coefficients: %w", err)
	}
	defer func() { _ = rows.Close() }()
	p := Coefficients{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("load coefficients: scan: %w", err)
		}
		p[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load coefficients: iterate: %w", err)
	}
	var problems []error
	for _, k := range CoefficientKeys() {
		v, ok := p[k]
		if !ok {
			problems = append(problems, fmt.Errorf("coefficient key %q is missing", k))
			continue
		}
		if err := ValidateCoefficientValue(k, v); err != nil {
			problems = append(problems, err)
		}
	}
	for n := 2; n <= 4; n++ {
		c, f := p[fmt.Sprintf("severity_%d_ceiling", n)], p[fmt.Sprintf("severity_%d_floor", n)]
		ci, cerr := strconv.Atoi(c)
		fi, ferr := strconv.Atoi(f)
		if cerr == nil && ferr == nil && ci > fi {
			problems = append(problems, fmt.Errorf("coefficient severity_%d_ceiling %d must not exceed severity_%d_floor %d", n, ci, n, fi))
		}
	}
	if err := errors.Join(problems...); err != nil {
		return nil, fmt.Errorf("load coefficients: %w", err)
	}
	return p, nil
}

func (p Coefficients) Duration(key string) time.Duration {
	d, _ := ParseSpan(p[key])
	return d
}

func (p Coefficients) Int(key string) int {
	n, _ := strconv.Atoi(p[key])
	return n
}
