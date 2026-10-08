// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"encoding/json"
	"testing"
)

func TestParseBeadCol(t *testing.T) {
	at := map[string]json.RawMessage{}
	cases := []struct {
		col, key, raw string
		keep, wantErr bool
	}{
		{"labels", "labels", `["a"]`, true, false},
		{"labels", "labels", `[]`, false, false},
		{"external_refs", "external_refs", `1`, false, true},
		{"priority", "priority", `"x"`, false, true},
		{"reasoning_depth", "reasoning_depth", `2`, true, false},
		{"title", "title", `1`, false, true},
		{"title", "title", `"t"`, true, false},
	}
	for _, tc := range cases {
		_, keep, err := parseBeadCol(tc.col, tc.key, json.RawMessage(tc.raw), at)
		if (err != nil) != tc.wantErr || keep != tc.keep {
			t.Errorf("parseBeadCol(%s, %s) = keep %v err %v; want keep %v err %v", tc.col, tc.raw, keep, err, tc.keep, tc.wantErr)
		}
	}
}
