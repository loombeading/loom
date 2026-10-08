// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

const CanonicalIDLen = 26

const crockfordAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

var crockfordIndex = func() map[byte]byte {
	m := make(map[byte]byte, len(crockfordAlphabet))
	for i := range len(crockfordAlphabet) {
		m[crockfordAlphabet[i]] = byte(i)
	}
	return m
}()

func EncodeCrockford128(b [16]byte) string {
	n := new(big.Int).SetBytes(b[:])
	n.Lsh(n, 2)

	out := make([]byte, CanonicalIDLen)
	mask := big.NewInt(0x1f)
	group := new(big.Int)
	for i := CanonicalIDLen - 1; i >= 0; i-- {
		group.And(n, mask)
		out[i] = crockfordAlphabet[group.Int64()]
		n.Rsh(n, 5)
	}
	return string(out)
}

func NewCanonicalID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("domain: crypto/rand unavailable: %v", err))
	}
	return EncodeCrockford128(b)
}

func NormalizeIDInput(s string) string {
	lower := strings.ToLower(s)
	var sb strings.Builder
	sb.Grow(len(lower))
	for i := range len(lower) {
		switch c := lower[i]; c {
		case 'i', 'l':
			sb.WriteByte('1')
		case 'o':
			sb.WriteByte('0')
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}
