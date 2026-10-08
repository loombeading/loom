// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import "testing"

func TestQuoteYAML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", `""`},
		{"plain", "hello world", `"hello world"`},
		{"double quote", `a "b" c`, `"a \"b\" c"`},
		{"backslash", `a\b`, `"a\\b"`},
		{"newline lf", "a\nb", `"a\nb"`},
		{"newline crlf", "a\r\nb", `"a\nb"`},
		{"newline cr", "a\rb", `"a\nb"`},
		{"tab", "a\tb", `"a\tb"`},
		{"other control char", "a\x01b", "\"a\U0000FFFDb\""},
		{"del", "a\x7fb", "\"a\U0000FFFDb\""},
		{"combined", "a \"quote\", a \\backslash\\, a\nline, a\ttab", `"a \"quote\", a \\backslash\\, a\nline, a\ttab"`},
		{"unicode preserved", "日本語", `"日本語"`},
		{"tag and zero width stripped", "a\U0000200Bb\U000E0001c", `"abc"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := QuoteYAML(c.in)
			if got != c.want {
				t.Errorf("QuoteYAML(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
