// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"os"
	"testing"
)

func TestColorEnabled(t *testing.T) {
	blank := func(string) string { return "" }
	noColor := func(k string) string {
		if k == NoColorEnv {
			return "1"
		}
		return ""
	}
	bdColorAlways := func(k string) string {
		if k == LMColorEnv {
			return "always"
		}
		return ""
	}
	noColorAndAlways := func(k string) string {
		switch k {
		case NoColorEnv:
			return "1"
		case LMColorEnv:
			return "always"
		}
		return ""
	}

	t.Run("non-*os.File writer is never a terminal", func(t *testing.T) {
		var buf bytes.Buffer
		if got := ColorEnabled(&buf, blank); got {
			t.Errorf("ColorEnabled(bytes.Buffer, no env) = true, want false")
		}
	})

	t.Run("NO_COLOR suppresses regardless of terminal", func(t *testing.T) {
		if got := ColorEnabled(os.Stdout, noColor); got {
			t.Errorf("ColorEnabled(os.Stdout, NO_COLOR set) = true, want false")
		}
	})

	t.Run("LM_COLOR=always forces on a non-terminal writer", func(t *testing.T) {
		var buf bytes.Buffer
		if got := ColorEnabled(&buf, bdColorAlways); !got {
			t.Errorf("ColorEnabled(bytes.Buffer, LM_COLOR=always) = false, want true")
		}
	})

	t.Run("NO_COLOR takes precedence over LM_COLOR=always", func(t *testing.T) {
		var buf bytes.Buffer
		if got := ColorEnabled(&buf, noColorAndAlways); got {
			t.Errorf("ColorEnabled(bytes.Buffer, NO_COLOR + LM_COLOR=always) = true, want false")
		}
	})

	t.Run("no env vars falls back to the TTY check", func(t *testing.T) {
		var buf bytes.Buffer
		if got := ColorEnabled(&buf, blank); got {
			t.Errorf("ColorEnabled(bytes.Buffer, no env) = true, want false")
		}
	})
}

func TestIsTerminal(t *testing.T) {
	var buf bytes.Buffer
	if isTerminal(&buf) {
		t.Errorf("isTerminal(bytes.Buffer) = true, want false")
	}

	f, err := os.CreateTemp(t.TempDir(), "isterminal")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if isTerminal(f) {
		t.Errorf("isTerminal(regular file) = true, want false")
	}
}
