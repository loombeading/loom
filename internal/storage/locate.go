// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/loombeading/loom/internal/xdg"
)

const BeadsDirName = ".loom"

const DBFileName = "loom.db"

const xdgAppName = "loom"

var ErrNotFound = errors.New("no loom data directory found (set LM_DIR or run \"lm init\")")

type Env func(string) string

func resolveLMDirEnv(env Env) (string, bool, error) {
	v := env("LM_DIR")
	if v == "" {
		return "", false, nil
	}
	if !filepath.IsAbs(v) {
		return "", false, fmt.Errorf("LM_DIR must be an absolute path, got %q", v)
	}
	return filepath.Clean(v), true, nil
}

func Locate(env Env) (string, string, error) {
	if dir, ok, err := resolveLMDirEnv(env); err != nil {
		return "", "", err
	} else if ok {
		return dir, "LM_DIR", nil
	}

	dataDir, via, err := xdg.DataHome(env, xdgAppName)
	if err != nil {
		return "", "", err
	}
	if info, statErr := os.Stat(dataDir); statErr == nil && info.IsDir() {
		return dataDir, via, nil
	}

	return "", "", ErrNotFound
}

func ResolveInitDir(env Env) (string, string, error) {
	if dir, ok, err := resolveLMDirEnv(env); err != nil {
		return "", "", err
	} else if ok {
		return dir, "LM_DIR", nil
	}

	return xdg.DataHome(env, xdgAppName)
}

func DBPath(dir string) string {
	return filepath.Join(dir, DBFileName)
}
