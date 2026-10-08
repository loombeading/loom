// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
)

var ErrAlreadyInitialized = errors.New("database already exists")

type InitResult struct {
	Dir    string
	DBPath string
}

func Init(ctx context.Context, dir string, opts OpenOptions) (*InitResult, error) {
	if opts.Env != nil && IsReadOnlyEnv(opts.Env("LM_READONLY")) {
		return nil, ErrReadOnlyWrite
	}

	dbPath := DBPath(dir)
	if _, err := os.Stat(dbPath); err == nil {
		return nil, ErrAlreadyInitialized
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", dbPath, err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}

	opts.AllowCreate = true
	db, err := Open(ctx, dir, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	return &InitResult{Dir: dir, DBPath: dbPath}, nil
}
