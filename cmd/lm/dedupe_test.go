// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func showField(t *testing.T, id, key string) string {
	t.Helper()
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show %s: exit = %d, stderr = %q", id, code, errS)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(line, key+": "); ok {
			return v
		}
	}
	t.Fatalf("show %s: no %s line in %q", id, key, out)
	return ""
}

func TestDedupeKeyMatchAdvancesLastSeenWithoutCreating(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "first", "--redetect-key", "k1")
	if got := showField(t, id, "redetect_key"); got != `"k1"` {
		t.Fatalf("redetect_key = %s, want \"k1\"", got)
	}
	if got := showField(t, id, "last_redetected_at"); got != "null" {
		t.Fatalf("last_redetected_at after create = %s, want null", got)
	}

	out, errS, code := runCmd(t, []string{"create", "--title", "second", "--redetect-key", "k1"}, "")
	if code != 0 {
		t.Fatalf("create with matching key: exit = %d, stderr = %q", code, errS)
	}
	seen, ok := strings.CutPrefix(strings.TrimSuffix(out, "\n"), "- Redetected: ")
	if !ok || strings.Contains(seen, "\n") {
		t.Fatalf("stdout = %q, want a single '- Redetected: <ID>' line", out)
	}
	if got := showField(t, seen, "id"); got != `"`+id+`"` {
		t.Errorf("Seen %s resolves to %s, want %s", seen, got, id)
	}
	if got := showField(t, id, "last_redetected_at"); got == "null" {
		t.Error("last_redetected_at not advanced on match")
	}
	list, _, _ := runCmd(t, []string{"list"}, "")
	if strings.Contains(list, "second") {
		t.Errorf("matching key created a new Bead: %q", list)
	}
}

func TestDedupeKeyMismatchCreatesNewBead(t *testing.T) {
	initBeadsDir(t)
	first := mustCreateBead(t, "--title", "first", "--redetect-key", "k1")
	second := mustCreateBead(t, "--title", "second", "--redetect-key", "k2")
	if first == second {
		t.Fatalf("different keys returned the same ID %s", first)
	}
	if got := showField(t, first, "last_redetected_at"); got != "null" {
		t.Errorf("unrelated key advanced last_redetected_at of %s to %s", first, got)
	}
}

func TestDedupeKeyOfTerminalBeadCreatesNewBead(t *testing.T) {
	initBeadsDir(t)
	first := mustCreateBead(t, "--title", "first", "--redetect-key", "k1")
	if _, errS, code := runCmd(t, []string{"close", first, "--reason", "done"}, ""); code != 0 {
		t.Fatalf("close: exit = %d, stderr = %q", code, errS)
	}
	out, errS, code := runCmd(t, []string{"create", "--title", "again", "--redetect-key", "k1"}, "")
	if code != 0 {
		t.Fatalf("create after close: exit = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, "- Created: ") || strings.Contains(out, first) {
		t.Errorf("stdout = %q, want a new Bead other than %s", out, first)
	}
}

func TestReviveRequiresReason(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	out, errS, code := runCmd(t, []string{"update", id, "--revive"}, "")
	if code == 0 || !strings.Contains(out+errS, "--revive requires --reason") {
		t.Errorf("update --revive without --reason: exit = %d, stdout = %q, stderr = %q, want non-zero", code, out, errS)
	}
	if got := showField(t, id, "revived_at"); got != "null" {
		t.Errorf("revived_at = %s after rejected --revive, want null", got)
	}
}

func TestReviveRecordsRevivedAt(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"update", id, "--revive", "--reason", "needed again"}, ""); code != 0 {
		t.Fatalf("update --revive: exit = %d, stderr = %q", code, errS)
	}
	if got := showField(t, id, "revived_at"); got == "null" {
		t.Error("revived_at not set by --revive")
	}
}
