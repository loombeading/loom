// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func tokenCostLine(t *testing.T) string {
	t.Helper()
	for _, l := range exportLines(t) {
		if strings.Contains(l, `"_type":"token_cost"`) {
			return l
		}
	}
	t.Fatal("no token_cost row in export")
	return ""
}

var launchKeys = []string{"model", "effort", "kind", "slot", "tokens_cache_creation", "tokens_cache_read", "exit_reason", "duration_ms"}

func TestCostAddExportsOnlyTheSixColumnsAndRoundTrips(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"cost", "add", id, "--in", "10", "--out", "5"}, ""); code != 0 {
		t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
	}
	line := tokenCostLine(t)
	var row map[string]any
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"_type", "actor", "bead_id", "id", "recorded_at", "tokens_in", "tokens_out"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("token_cost keys = %v, want %v", keys, want)
	}

	initBeadsDir(t)
	if _, errS, code := runCmd(t, []string{"import", "-"}, line+"\n"); code != 0 {
		t.Fatalf("import: exit code = %d, stderr = %q", code, errS)
	}
	if got := tokenCostLine(t); got != line {
		t.Errorf("round trip changed the row:\n got %s\nwant %s", got, line)
	}
}

func TestCostAddRejectsLaunchFlags(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	for _, f := range []string{"--model", "--effort", "--kind", "--slot", "--cache-creation", "--cache-read", "--exit-reason", "--duration-ms"} {
		if _, _, code := runCmd(t, []string{"cost", "add", id, "--in", "1", f, "1"}, ""); code == 0 {
			t.Errorf("%s: exit code = 0, want non-zero", f)
		}
	}
	for _, l := range exportLines(t) {
		if strings.Contains(l, `"_type":"token_cost"`) {
			t.Errorf("a rejected cost add wrote a row: %s", l)
		}
	}
}

func TestImportRejectsTokenCostWithLaunchKeys(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"cost", "add", id, "--in", "10", "--out", "5"}, ""); code != 0 {
		t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
	}
	line := tokenCostLine(t)
	for _, k := range launchKeys {
		bad := strings.TrimSuffix(line, "}") + `,"` + k + `":"x"}`
		initBeadsDir(t)
		_, errS, code := runCmd(t, []string{"import", "-"}, bad+"\n")
		if code == 0 {
			t.Errorf("import with %s: exit code = 0, want non-zero", k)
			continue
		}
		if !strings.Contains(errS, `unknown key "`+k+`"`) {
			t.Errorf("import with %s: stderr = %q, want unknown key", k, errS)
		}
	}
}
