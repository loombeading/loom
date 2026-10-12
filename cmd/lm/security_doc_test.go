// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityDocReadOnlyBoundary(t *testing.T) {
	for _, path := range []string{"SECURITY.md", "skills/loom/docs/env.md"} {
		b, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, want := range []string{"読み取り専用でマウント", "書き込み権限の無い利用者"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: 境界が OS の手段であることの記述 %q が無い", path, want)
			}
		}
		if strings.Contains(s, "LM_READONLY") {
			t.Errorf("%s: 削除した LM_READONLY が残っている", path)
		}
	}
}
