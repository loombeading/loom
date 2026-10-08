// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func exportLines(t *testing.T) []string {
	t.Helper()
	out, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export: exit code = %d, stderr = %q", code, errS)
	}
	out = strings.TrimSuffix(out, "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func TestExportIsDeterministicByteForByte(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "a", "--label", "z", "--label", "a")
	mustCreateBead(t, "--title", "b")

	first, errS1, code1 := runCmd(t, []string{"export"}, "")
	second, errS2, code2 := runCmd(t, []string{"export"}, "")
	if code1 != 0 || code2 != 0 {
		t.Fatalf("export: exit codes = %d, %d; stderr = %q, %q", code1, code2, errS1, errS2)
	}
	if first != second {
		t.Fatalf("export is not byte-identical across runs:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestExportIgnoresBDNamespace(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "bead-in-aaa", "--namespace", "aaa")
	t.Setenv("LM_NAMESPACE", "aaa")
	mustCreateBead(t, "--title", "bead-in-bbb", "--namespace", "bbb")

	lines := exportLines(t)
	foundTitles := map[string]bool{}
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("decode line %q: %v", l, err)
		}
		if m["_type"] == "bead" {
			title, ok := m["title"].(string)
			if !ok {
				t.Fatalf("title = %#v, want string", m["title"])
			}
			foundTitles[title] = true
		}
	}
	if !foundTitles["bead-in-aaa"] {
		t.Error("bead-in-aaa (namespace aaa) missing from export under LM_NAMESPACE=aaa")
	}
	if !foundTitles["bead-in-bbb"] {
		t.Error("bead-in-bbb (namespace bbb) missing from export under LM_NAMESPACE=aaa")
	}
}

func TestExportRunsUnderReadOnly(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "a")

	t.Setenv("LM_READONLY", "1")
	out, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export under LM_READONLY=1: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, `"_type":"bead"`) {
		t.Errorf("export under LM_READONLY=1 produced no bead row: %q", out)
	}
}

func TestExportOutputHasNoAuditLogFields(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "a", "--description", "d")

	runCmd(t, []string{"update", id, "--title", "a2", "--reason", "why"}, "")

	allowed := map[string]bool{"bead": true, "dependency": true, "token_cost": true, "coefficient": true}
	for _, l := range exportLines(t) {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("decode line %q: %v", l, err)
		}
		typ, _ := m["_type"].(string)
		if !allowed[typ] {
			t.Errorf("line has unexpected _type %q: %s", typ, l)
		}
		for _, auditOnlyKey := range []string{"occurred_at", "kind", "new_value", "origin", "reason"} {
			if _, ok := m[auditOnlyKey]; ok {
				t.Errorf("line carries audit_log-only key %q: %s", auditOnlyKey, l)
			}
		}
	}
}

func TestExportTakesNoOutputFlag(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"export", "--output", "/tmp/x.jsonl"}, "")
	if code != 1 {
		t.Errorf("export --output ...: exit code = %d, want 1", code)
	}
	if errS == "" {
		t.Error("export --output ...: no error message on stderr")
	}
}
