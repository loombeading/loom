// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"strconv"
	"strings"
	"testing"
)

func noInvisible(t *testing.T, out string) {
	t.Helper()
	for _, r := range out {
		switch {
		case r >= 0xE0000 && r <= 0xE007F:
			t.Fatalf("output contains unicode tag character %U: %q", r, out)
		case r == 0x200B || r == 0x200C || r == 0x200D || r == 0x2060 || r == 0xFEFF:
			t.Fatalf("output contains zero-width character %U: %q", r, out)
		case (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069):
			t.Fatalf("output contains bidi control character %U: %q", r, out)
		case r <= 0x1F && r != 0x09 && r != 0x0A:
			t.Fatalf("output contains C0 control character (or CR) %U: %q", r, out)
		case r == 0x7F:
			t.Fatalf("output contains DEL: %q", out)
		case r >= 0x80 && r <= 0x9F:
			t.Fatalf("output contains C1 control character %U: %q", r, out)
		}
	}
}

func allEscaped(s string, ch byte) bool {
	for i := range len(s) {
		if s[i] == ch && (i == 0 || s[i-1] != '\\') {
			return false
		}
	}
	return true
}

var escapeSeeds = []string{
	"",
	"hello world",
	"a|b",
	"a`b",
	"a\nb",
	"a\r\nb",
	"a\rb",
	"a\nb\nc",
	"#title",
	"-item",
	"*item",
	">quote",
	"+item",
	"1. first",
	"12. first",
	"a#b",
	"123abc",
	"1. a|b\n`c`",
	"a\U000E0001b",
	"a\U0000200Bb",
	"a\U0000202Eb",
	"a\x07b",
	"\x1b[31mred\x1b[0m",
	"a\tb日本語😀",
	"a\U000E007Fb",
	"a\U000E0080b",
	"a\U0000009Fb",
	"a\U000000A0b",
	"a\U00002029b",
	"a\U0000202Ab",
	"\U0001F468\U0000200D\U0001F469\U0000200D\U0001F467",
	"a\U0000200Cb",
	"a\U00002060b",
	"a\U0000FEFFb",
	"a\U00002066b",
	"a\U00002069b",
	"-a\U0000200Bb",
	"line one\nline two\nline three",
	"line one\r\nline two",
	"line one\rline two",
	"#heading\nbody",
	"   #heading",
	"    #heading",
	"```\ncode\n```",
	"~~~\ncode\n~~~",
	"   ```\ncode",
	"- item one\n- item two",
	"| a | b |\n| --- | --- |\n| 1 | 2 |",
	"use `lm show`",
	"body\n\n\n",
	"body",
	"a\U0000200Bb\nc\x07d",
	`a "b" c`,
	`a\b`,
	"a\tb",
	"a\x01b",
	"a\x7fb",
	"a \"quote\", a \\backslash\\, a\nline, a\ttab",
	"日本語",
	"a\U0000200Bb\U000E0001c",
	"# h",
	"   ```",
	"a\u200bb",
	"\x1b[31m",
	"a\r\nb|c`d",
	"\xff",
}

func FuzzEscape(f *testing.F) {
	for _, s := range escapeSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := Escape(s)
		noInvisible(t, out)
		if strings.ContainsAny(out, "\n\r") {
			t.Fatalf("Escape(%q) = %q contains a newline or CR", s, out)
		}
		if !allEscaped(out, '|') {
			t.Fatalf("Escape(%q) = %q has an unescaped |", s, out)
		}
		if !allEscaped(out, '`') {
			t.Fatalf("Escape(%q) = %q has an unescaped `", s, out)
		}
		if leadingMarkdownRe.MatchString(out) {
			t.Fatalf("Escape(%q) = %q still matches leadingMarkdownRe", s, out)
		}
	})
}

func FuzzEscapeInline(f *testing.F) {
	for _, s := range escapeSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := escapeInline(s)
		noInvisible(t, out)
		if strings.ContainsAny(out, "\n\r") {
			t.Fatalf("escapeInline(%q) = %q contains a newline or CR", s, out)
		}
		if !allEscaped(out, '|') {
			t.Fatalf("escapeInline(%q) = %q has an unescaped |", s, out)
		}
		if !allEscaped(out, '`') {
			t.Fatalf("escapeInline(%q) = %q has an unescaped `", s, out)
		}
	})
}

func FuzzEscapeBlock(f *testing.F) {
	for _, s := range escapeSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := EscapeBlock(s)
		noInvisible(t, out)
		if strings.Contains(out, "\r") {
			t.Fatalf("EscapeBlock(%q) = %q contains a CR", s, out)
		}
		if !strings.HasSuffix(out, "\n") {
			t.Fatalf("EscapeBlock(%q) = %q does not end with a newline", s, out)
		}
		if strings.HasSuffix(out, "\n\n") {
			t.Fatalf("EscapeBlock(%q) = %q ends with a blank line", s, out)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if leadingHeadingRe.MatchString(line) {
				t.Fatalf("EscapeBlock(%q) = %q has an unescaped heading line %q", s, out, line)
			}
			if leadingFenceRe.MatchString(line) {
				t.Fatalf("EscapeBlock(%q) = %q has an unescaped fence line %q", s, out, line)
			}
		}
	})
}

func FuzzQuoteYAML(f *testing.F) {
	for _, s := range escapeSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := QuoteYAML(s)
		if !strings.HasPrefix(out, `"`) || !strings.HasSuffix(out, `"`) {
			t.Fatalf("QuoteYAML(%q) = %q is not quote-delimited", s, out)
		}
		unq, err := strconv.Unquote(out)
		if err != nil {
			t.Fatalf("QuoteYAML(%q) = %q: strconv.Unquote failed: %v", s, out, err)
		}
		want := strings.ReplaceAll(strings.ReplaceAll(normalizeInvisible(s), "\r\n", "\n"), "\r", "\n")
		if unq != want {
			t.Fatalf("QuoteYAML(%q) = %q: unquoted = %q, want %q", s, out, unq, want)
		}
	})
}
