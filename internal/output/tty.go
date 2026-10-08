// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"io"
	"os"
)

const (
	NoColorEnv = "NO_COLOR"
	LMColorEnv = "LM_COLOR"
)

func ColorEnabled(w io.Writer, getenv func(string) string) bool {
	if getenv(NoColorEnv) != "" {
		return false
	}
	if getenv(LMColorEnv) == "always" {
		return true
	}
	return isTerminal(w)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
