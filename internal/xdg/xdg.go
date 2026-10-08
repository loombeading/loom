// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package xdg

import (
	"errors"
	"path/filepath"
)

type LookupFunc = func(string) string

func DataHome(env LookupFunc, appName string) (string, string, error) {
	return resolve(env, "XDG_DATA_HOME", appName, ".local", "share")
}

func ConfigHome(env LookupFunc, appName string) (string, string, error) {
	return resolve(env, "XDG_CONFIG_HOME", appName, ".config")
}

func resolve(env LookupFunc, envVar, appName string, defaultRel ...string) (string, string, error) {
	if v := env(envVar); v != "" && filepath.IsAbs(v) {
		return filepath.Join(filepath.Clean(v), appName), envVar, nil
	}
	home := env("HOME")
	if home == "" {
		return "", "", errors.New("cannot resolve $" + envVar + "/" + appName + ": $HOME is not set")
	}
	parts := append(append([]string{home}, defaultRel...), appName)
	return filepath.Join(parts...), "default", nil
}
