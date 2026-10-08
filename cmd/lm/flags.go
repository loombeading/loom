// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/loombeading/loom/internal/domain"
)

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }

func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func readBodyFlag(value string, stdin io.Reader) (string, error) {
	if value != "-" {
		return value, nil
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return string(b), nil
}

func rejectMultipleStdin(fs *flag.FlagSet, names ...string) error {
	var stdinFlags []string
	for _, name := range names {
		if f := fs.Lookup(name); f != nil && f.Value.String() == "-" {
			stdinFlags = append(stdinFlags, "--"+name)
		}
	}
	if len(stdinFlags) > 1 {
		return fmt.Errorf("only one flag may read stdin (\"-\"), got %s", strings.Join(stdinFlags, ", "))
	}
	return nil
}

func validatePriority(priority int) error {
	if priority != -1 && (priority < domain.MinPriority || priority > domain.MaxPriority) {
		return fmt.Errorf("--priority must be between %d and %d", domain.MinPriority, domain.MaxPriority)
	}
	return nil
}

type scheduleFlags struct {
	severity    *int
	due         *time.Time
	expediteFor *time.Duration
}

func parseScheduleFlags(severity int, severitySet bool, due, expedite string) (scheduleFlags, error) {
	var sf scheduleFlags
	if severitySet {
		if severity < domain.MinSeverity || severity > domain.MaxSeverity {
			return sf, fmt.Errorf("--severity must be between %d and %d", domain.MinSeverity, domain.MaxSeverity)
		}
		sf.severity = &severity
	}
	if due != "" {
		t, err := time.Parse(time.RFC3339, due)
		if err != nil {
			return sf, fmt.Errorf("--due must be RFC 3339, got %q", due)
		}
		sf.due = &t
	}
	if expedite != "" {
		d, err := domain.ParseSpan(expedite)
		if err != nil {
			return sf, fmt.Errorf("--expedite: %w", err)
		}
		if d <= 0 {
			return sf, errors.New("--expedite must be a positive duration")
		}
		sf.expediteFor = &d
	}
	return sf, nil
}

func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i:]...)
			break
		}
		if len(a) == 0 || a[0] != '-' || a == "-" {
			positional = append(positional, a)
			continue
		}

		flags = append(flags, a)

		name := strings.TrimLeft(a, "-")
		if found := strings.Contains(name, "="); found {
			continue
		}

		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		type boolFlag interface {
			IsBoolFlag() bool
		}
		if bf, ok := f.Value.(boolFlag); ok && bf.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positional...)
}
