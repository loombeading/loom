// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestIsBusyErr(t *testing.T) {
	dir := mustMkdirBeads(t)
	db1 := openWritable(t, dir)
	db2, err := Open(context.Background(), dir, OpenOptions{Env: envMap(nil), Actor: "test", BusyTimeoutMS: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()

	holding := make(chan struct{})
	release := make(chan struct{})
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- db1.WithWrite(context.Background(), func(*sql.Tx) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	busyErr := db2.WithWrite(context.Background(), func(*sql.Tx) error { return nil })
	close(release)
	if err := <-writeErr; err != nil {
		t.Fatalf("first writer: %v", err)
	}

	if busyErr == nil {
		t.Fatal("second writer: want SQLITE_BUSY, got nil")
	}
	if !IsBusyErr(busyErr) {
		t.Fatalf("IsBusyErr(%v) = false, want true", busyErr)
	}
	if !IsBusyErr(fmt.Errorf("wrapped: %w", busyErr)) {
		t.Fatal("IsBusyErr(wrapped busy) = false, want true")
	}
	if IsBusyErr(nil) {
		t.Fatal("IsBusyErr(nil) = true, want false")
	}
	if IsBusyErr(errors.New("not sqlite")) {
		t.Fatal("IsBusyErr(non-sqlite) = true, want false")
	}

	_, constraintErr := db1.SQL.ExecContext(t.Context(), `INSERT INTO audit_log (occurred_at) VALUES (NULL)`)
	if constraintErr == nil {
		t.Fatal("constraint violation: want error, got nil")
	}
	if IsBusyErr(constraintErr) {
		t.Fatalf("IsBusyErr(%v) = true, want false", constraintErr)
	}
}

func TestNowRFC3339Milli(t *testing.T) {
	got := NowRFC3339Milli()
	parsed, err := time.Parse(time.RFC3339Nano, got)
	if err != nil {
		t.Fatalf("parse %q: %v", got, err)
	}
	if len(got) != len("2006-01-02T15:04:05.000Z") || got[len(got)-1] != 'Z' {
		t.Fatalf("NowRFC3339Milli = %q, want millisecond UTC form", got)
	}
	if d := time.Since(parsed); d < 0 || d > time.Minute {
		t.Fatalf("NowRFC3339Milli = %q, %v away from now", got, d)
	}
}
