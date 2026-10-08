// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

const DefaultBusyTimeoutMS = 5000

const (
	ModeReadOnly  = "ro"
	ModeImmutable = "immutable"
)

var ErrReadOnlyWrite = errors.New("write attempted on a read-only database (LM_READONLY is set)")

var ErrDBNotFound = errors.New("loom.db not found (set LM_DIR or run \"lm init\")")

func IsReadOnlyEnv(v string) bool {
	switch strings.ToLower(v) {
	case "", "0", "false", "no":
		return false
	default:
		return true
	}
}

type OpenOptions struct {
	Env Env

	BusyTimeoutMS int

	Actor string

	AllowCreate bool
}

type DB struct {
	SQL      *sql.DB
	ReadOnly bool

	Mode string

	BusyTimeoutMS int
}

func (d *DB) Close() error {
	return d.SQL.Close()
}

func Open(ctx context.Context, dir string, opts OpenOptions) (*DB, error) {
	if opts.Env == nil {
		return nil, errors.New("storage.Open: OpenOptions.Env is required")
	}

	busyTimeoutMS := opts.BusyTimeoutMS
	if busyTimeoutMS == 0 {
		busyTimeoutMS = DefaultBusyTimeoutMS
	}

	dbPath := DBPath(dir)

	if !opts.AllowCreate {
		if _, err := os.Stat(dbPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: %s", ErrDBNotFound, dbPath)
			}
			return nil, fmt.Errorf("stat %s: %w", dbPath, err)
		}
	}

	readOnly := IsReadOnlyEnv(opts.Env("LM_READONLY"))

	var mode string
	if readOnly {
		shmPath := dbPath + "-shm"
		_, shmErr := os.Stat(shmPath)
		shmMissing := errors.Is(shmErr, os.ErrNotExist)
		if shmMissing && !dirWritable(dir) {
			mode = ModeImmutable
		} else {
			mode = ModeReadOnly
		}
	}

	dsn, err := buildDSN(dbPath, readOnly, mode, busyTimeoutMS)
	if err != nil {
		return nil, err
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := ensureSchema(ctx, sqlDB, readOnly); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return &DB{SQL: sqlDB, ReadOnly: readOnly, Mode: mode, BusyTimeoutMS: busyTimeoutMS}, nil
}

func buildDSN(dbPath string, readOnly bool, mode string, busyTimeoutMS int) (string, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return "", fmt.Errorf("resolve database path: %w", err)
	}

	q := url.Values{}
	q.Set("_busy_timeout", strconv.Itoa(busyTimeoutMS))
	q.Set("_foreign_keys", "off")
	q.Add("_pragma", "cache_size(-65536)")
	q.Add("_pragma", "temp_store(MEMORY)")
	q.Add("_pragma", "mmap_size(268435456)")

	switch {
	case !readOnly:
		q.Set("_journal_mode", "WAL")
		q.Set("_synchronous", "NORMAL")
		q.Set("_txlock", "immediate")
	case mode == ModeImmutable:
		q.Set("immutable", "1")
	default:
		q.Set("mode", "ro")
	}

	return "file:" + filepath.ToSlash(absPath) + "?" + q.Encode(), nil
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".bd-writable-check-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
