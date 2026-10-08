// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"errors"
	"fmt"
	"math/big"
	"testing"
)

func decodeCrockford128(s string) ([16]byte, error) {
	var out [16]byte
	if len(s) != CanonicalIDLen {
		return out, fmt.Errorf("decode crockford128: want %d characters, got %d", CanonicalIDLen, len(s))
	}

	n := new(big.Int)
	for i := range len(s) {
		v, ok := crockfordIndex[s[i]]
		if !ok {
			return out, fmt.Errorf("decode crockford128: invalid character %q at position %d", s[i], i)
		}
		n.Lsh(n, 5)
		n.Or(n, big.NewInt(int64(v)))
	}
	n.Rsh(n, 2)

	raw := n.Bytes()
	if len(raw) > 16 {
		return out, errors.New("decode crockford128: value overflows 128 bits")
	}
	copy(out[16-len(raw):], raw)
	return out, nil
}

func isCanonicalID(s string) bool {
	if len(s) != CanonicalIDLen {
		return false
	}
	for i := range len(s) {
		if _, ok := crockfordIndex[s[i]]; !ok {
			return false
		}
	}
	last := crockfordIndex[s[len(s)-1]]
	return last&0x3 == 0
}

func TestNewCanonicalIDFormat(t *testing.T) {
	id := NewCanonicalID()
	if len(id) != CanonicalIDLen {
		t.Fatalf("len(id) = %d, want %d", len(id), CanonicalIDLen)
	}
	if !isCanonicalID(id) {
		t.Fatalf("isCanonicalID(%q) = false, want true", id)
	}
	for i := range len(id) {
		if _, ok := crockfordIndex[id[i]]; !ok {
			t.Fatalf("id %q contains character %q outside the Crockford Base32 alphabet", id, id[i])
		}
	}
}

func TestNewCanonicalIDUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := range 10000 {
		id := NewCanonicalID()
		if seen[id] {
			t.Fatalf("collision at iteration %d: %s", i, id)
		}
		seen[id] = true
	}
}

func TestIsCanonicalID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{repeat26("0"), true},
		{zeros25() + "w", true},
		{zeros25() + "1", false},
		{zeros25(), false},
		{repeat26("0") + "0", false},
		{zeros25() + "u", false},
		{zeros25() + "I", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isCanonicalID(c.in); got != c.want {
			t.Errorf("isCanonicalID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func repeat26(s string) string { return repeatN(s, 26) }
func zeros25() string          { return repeatN("0", 25) }
func repeatN(s string, n int) string {
	out := make([]byte, 0, n*len(s))
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

func TestEncodeCrockford128KnownVectors(t *testing.T) {
	cases := []struct {
		name string
		in   [16]byte
		want string
	}{
		{"all-zero", [16]byte{}, "00000000000000000000000000"},
		{"all-0xff", func() [16]byte {
			var b [16]byte
			for i := range b {
				b[i] = 0xff
			}
			return b
		}(), "zzzzzzzzzzzzzzzzzzzzzzzzzw"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EncodeCrockford128(c.in)
			if got != c.want {
				t.Fatalf("EncodeCrockford128(%x) = %q, want %q", c.in, got, c.want)
			}
			if len(got) != CanonicalIDLen {
				t.Fatalf("len(EncodeCrockford128(%x)) = %d, want %d", c.in, len(got), CanonicalIDLen)
			}
			if !isCanonicalID(got) {
				t.Fatalf("isCanonicalID(EncodeCrockford128(%x)) = false, want true", c.in)
			}

			back, err := decodeCrockford128(got)
			if err != nil {
				t.Fatalf("decodeCrockford128(%q): %v", got, err)
			}
			if back != c.in {
				t.Fatalf("decodeCrockford128(EncodeCrockford128(%x)) = %x, want %x", c.in, back, c.in)
			}
		})
	}
}

func TestCrockford128RoundTrip(t *testing.T) {
	for range 1000 {
		id := NewCanonicalID()
		b, err := decodeCrockford128(id)
		if err != nil {
			t.Fatalf("decodeCrockford128(%q): %v", id, err)
		}
		back := EncodeCrockford128(b)
		if back != id {
			t.Fatalf("round-trip mismatch: %q -> %x -> %q", id, b, back)
		}
	}
}

func TestDecodeCrockford128Errors(t *testing.T) {
	cases := []string{
		"",
		zeros25(),
		repeat26("0") + "0",
		zeros25() + "u",
		zeros25() + "I",
	}
	for _, in := range cases {
		if _, err := decodeCrockford128(in); err == nil {
			t.Errorf("decodeCrockford128(%q) = nil error, want error", in)
		}
	}
}

func TestNormalizeIDInput(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ABCDEF", "abcdef"},
		{"I", "1"},
		{"L", "1"},
		{"O", "0"},
		{"i", "1"},
		{"l", "1"},
		{"o", "0"},
		{"IlO", "110"},
		{"u", "u"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeIDInput(c.in); got != c.want {
			t.Errorf("NormalizeIDInput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
