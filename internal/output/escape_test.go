// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import "testing"

func TestEscape(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "hello world", "hello world"},
		{"pipe", "a|b", `a\|b`},
		{"backtick", "a`b", "a\\`b"},
		{"newline lf", "a\nb", "a b"},
		{"newline crlf", "a\r\nb", "a b"},
		{"newline cr", "a\rb", "a b"},
		{"multiple newlines", "a\nb\nc", "a b c"},
		{"leading hash", "#title", "\\#title"},
		{"leading dash", "-item", "\\-item"},
		{"leading star", "*item", "\\*item"},
		{"leading gt", ">quote", "\\>quote"},
		{"leading plus", "+item", "\\+item"},
		{"leading ordered list", "1. first", "\\1. first"},
		{"leading ordered list multi digit", "12. first", "\\12. first"},
		{"mid string hash not escaped", "a#b", "a#b"},
		{"digits without dot not escaped", "123abc", "123abc"},
		{"combined", "1. a|b\n`c`", "\\1. a\\|b \\`c\\`"},
		{"unicode tag character deleted", "a\U000E0001b", "ab"},
		{"zero width space deleted", "a\U0000200Bb", "ab"},
		{"bidi override deleted", "a\U0000202Eb", "ab"},
		{"bel control char replaced", "a\x07b", "a\U0000FFFDb"},
		{"ansi escape neutralized", "\x1b[31mred\x1b[0m", "\U0000FFFD[31mred\U0000FFFD[0m"},
		{"tab japanese emoji preserved", "a\tb日本語😀", "a\tb日本語😀"},
		{"tag boundary max deleted", "a\U000E007Fb", "ab"},
		{"tag boundary above kept", "a\U000E0080b", "a\U000E0080b"},
		{"c1 boundary max replaced", "a\U0000009Fb", "a\U0000FFFDb"},
		{"c1 boundary above kept", "a\U000000A0b", "a\U000000A0b"},
		{"paragraph separator kept", "a\U00002029b", "a\U00002029b"},
		{"bidi boundary deleted", "a\U0000202Ab", "ab"},
		{"zwj emoji sequence decomposed", "\U0001F468\U0000200D\U0001F469\U0000200D\U0001F467", "\U0001F468\U0001F469\U0001F467"},
		{"zero width non-joiner deleted", "a\U0000200Cb", "ab"},
		{"word joiner deleted", "a\U00002060b", "ab"},
		{"byte order mark deleted", "a\U0000FEFFb", "ab"},
		{"left-to-right isolate deleted", "a\U00002066b", "ab"},
		{"pop directional isolate deleted", "a\U00002069b", "ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Escape(c.in)
			if got != c.want {
				t.Errorf("Escape(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestEscapeInline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "hello world", "hello world"},
		{"pipe", "a|b", `a\|b`},
		{"backtick", "a`b", "a\\`b"},
		{"newline lf", "a\nb", "a b"},
		{"zero width space deleted", "a\U0000200Bb", "ab"},
		{"bel control char replaced", "a\x07b", "a\U0000FFFDb"},
		{"leading dash not escaped", "-a\U0000200Bb", "-ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := escapeInline(c.in)
			if got != c.want {
				t.Errorf("escapeInline(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestEscapeBlock(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "\n"},
		{"plain", "hello world", "hello world\n"},
		{"newlines preserved", "line one\nline two\nline three", "line one\nline two\nline three\n"},
		{"crlf normalized", "line one\r\nline two", "line one\nline two\n"},
		{"lone cr normalized", "line one\rline two", "line one\nline two\n"},
		{"leading hash escaped", "#heading\nbody", "\\#heading\nbody\n"},
		{"leading hash with indent escaped", "   #heading", "   \\#heading\n"},
		{"leading hash with too much indent not escaped", "    #heading", "    #heading\n"},
		{"leading fence backtick escaped", "```\ncode\n```", "\\```\ncode\n\\```\n"},
		{"leading fence tilde escaped", "~~~\ncode\n~~~", "\\~~~\ncode\n\\~~~\n"},
		{"leading fence with indent escaped", "   ```\ncode", "   \\```\ncode\n"},
		{"mid-line hash not escaped", "a#b", "a#b\n"},
		{"mid-line pipe not escaped", "a|b", "a|b\n"},
		{"list preserved", "- item one\n- item two", "- item one\n- item two\n"},
		{"table preserved", "| a | b |\n| --- | --- |\n| 1 | 2 |", "| a | b |\n| --- | --- |\n| 1 | 2 |\n"},
		{"inline code preserved", "use `lm show`", "use `lm show`\n"},
		{"trailing newline normalized to one", "body\n\n\n", "body\n"},
		{"no trailing newline still gets one", "body", "body\n"},
		{"zero width and control preserved newlines", "a\U0000200Bb\nc\x07d", "ab\nc\U0000FFFDd\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EscapeBlock(c.in)
			if got != c.want {
				t.Errorf("EscapeBlock(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
