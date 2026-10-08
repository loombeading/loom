// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoredPriorityZeroIsRejectedOnEveryWritePath(t *testing.T) {
	initBeadsDir(t)
	want := "priority must be between 1 and 4"

	_, errS, code := runCmd(t, []string{"create", "--title", "t", "--priority", "0"}, "")
	if code == 0 || !strings.Contains(errS, want) {
		t.Errorf("create --priority 0: exit = %d, stderr = %q, want non-zero with %q", code, errS, want)
	}

	id := mustCreateBead(t, "--title", "t")
	out, errS, code := runCmd(t, []string{"update", id, "--priority", "0"}, "")
	if code == 0 || !strings.Contains(out+errS, want) {
		t.Errorf("update --priority 0: exit = %d, stdout = %q, stderr = %q, want non-zero with %q", code, out, errS, want)
	}

	line := `{"_type":"bead","id":"0000000000000000000000000z","namespace":"default","title":"x","status":"open","priority":0,"type":"task","created_at":"2026-01-01T00:00:00.000Z","updated_at":"2026-01-01T00:00:00.000Z"}` + "\n"
	path := filepath.Join(t.TempDir(), "p0.jsonl")
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errS, code = runCmd(t, []string{"import", path}, "")
	if code == 0 || !strings.Contains(errS, want) {
		t.Errorf("import priority:0: exit = %d, stderr = %q, want non-zero with %q", code, errS, want)
	}
}

func TestExpediteAboveMaxAgeIsRejected(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"create", "--title", "t", "--expedite", "25h", "--reason", "r"}, "")
	if code == 0 || !strings.Contains(errS, "expedite_max_age") {
		t.Errorf("create --expedite 25h: exit = %d, stderr = %q, want non-zero naming expedite_max_age", code, errS)
	}
	mustCreateBead(t, "--title", "ok", "--expedite", "24h", "--reason", "r")
}

func TestExpediteBeyondMaxOpenIsRejectedUntilOneExpires(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "one", "--expedite", "1h", "--reason", "r")
	mustCreateBead(t, "--title", "two", "--expedite", "1h", "--reason", "r")

	_, errS, code := runCmd(t, []string{"create", "--title", "three", "--expedite", "1h", "--reason", "r"}, "")
	if code == 0 || !strings.Contains(errS, "expedite_max_open") {
		t.Fatalf("third expedite: exit = %d, stderr = %q, want non-zero naming expedite_max_open", code, errS)
	}

	t.Setenv("LM_NOW", time.Now().UTC().Add(2*time.Hour).Format(time.RFC3339))
	mustCreateBead(t, "--title", "three", "--expedite", "1h", "--reason", "r")
}

func TestExpediteRequiresReason(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"create", "--title", "t", "--expedite", "1h"}, "")
	if code == 0 || !strings.Contains(errS, "requires --reason") {
		t.Errorf("create --expedite without --reason: exit = %d, stderr = %q, want non-zero", code, errS)
	}
	id := mustCreateBead(t, "--title", "t")
	out, errS, code := runCmd(t, []string{"update", id, "--expedite", "1h"}, "")
	if code == 0 || !strings.Contains(out+errS, "requires --reason") {
		t.Errorf("update --expedite without --reason: exit = %d, stdout = %q, stderr = %q, want non-zero", code, out, errS)
	}
}

func TestReadyFailsWhenPolicyRowIsMissing(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "t")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), `DELETE FROM coefficients WHERE key = 'due_lead'`); err != nil {
		t.Fatal(err)
	}
	_, errS, code := runCmd(t, []string{"ready"}, "")
	if code == 0 || !strings.Contains(errS, "due_lead") {
		t.Errorf("ready with a missing policy row: exit = %d, stderr = %q, want non-zero naming due_lead", code, errS)
	}
}

func TestExpeditedBeadLeadsReadyWithEffExpediteAndRaisesItsBlocker(t *testing.T) {
	initBeadsDir(t)
	plain := mustCreateBead(t, "--title", "plain p1", "--priority", "1")
	blocker := mustCreateBead(t, "--title", "blocker p3", "--priority", "3")
	urgent := mustCreateBead(t, "--title", "urgent", "--priority", "4", "--expedite", "1h", "--reason", "r", "--blocked-by", blocker)
	solo := mustCreateBead(t, "--title", "solo", "--priority", "3", "--expedite", "1h", "--reason", "r")

	out := mustRun(t, "ready")
	var titles []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "- ") {
			titles = append(titles, line)
		}
	}
	if len(titles) < 3 {
		t.Fatalf("ready output = %q, want at least 3 rows", out)
	}
	if !strings.Contains(titles[0], "blocker p3") || !strings.Contains(titles[0], "eff=P0(") {
		t.Errorf("first ready row = %q, want the blocker raised to P0 by the expedited Bead", titles[0])
	}
	if !strings.Contains(titles[1], "solo") || !strings.Contains(titles[1], "eff=P0(expedite)") {
		t.Errorf("second ready row = %q, want the expedited Bead marked eff=P0(expedite)", titles[1])
	}
	if !strings.Contains(titles[2], "plain p1") {
		t.Errorf("third ready row = %q, want the plain P1 Bead", titles[2])
	}
	mustShowField(t, solo, `effective_priority_source: "expedite"`+"\n")
	mustShowField(t, solo, "effective_priority: 0\n")
	mustShowField(t, urgent, "expedite_reason: \"r\"\n")
	_ = plain
}

func TestExpiredExpediteIsMarkedOnReadyAndShow(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "soon stale", "--priority", "2", "--expedite", "1h", "--reason", "r")
	t.Setenv("LM_NOW", time.Now().UTC().Add(2*time.Hour).Format(time.RFC3339))
	out := mustRun(t, "ready")
	if !strings.Contains(out, "soon stale") || !strings.Contains(out, " expedite-expired") || strings.Contains(out, "eff=P0") {
		t.Errorf("ready output = %q, want the expired expedite marked and not P0", out)
	}
	if !strings.Contains(mustRun(t, "show", id), " expedite-expired") {
		t.Errorf("show output lacks the expedite-expired mark")
	}
}

func TestCreateRejectsBadScheduleFlagsAndClock(t *testing.T) {
	initBeadsDir(t)
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"severity-9":      {[]string{"--severity", "9"}, "--severity must be between 1 and 4"},
		"due-garbage":     {[]string{"--due", "tomorrow"}, "--due must be RFC 3339"},
		"expedite-word":   {[]string{"--expedite", "soon", "--reason", "r"}, "--expedite"},
		"expedite-negate": {[]string{"--expedite", "-1h", "--reason", "r"}, "--expedite must be a positive duration"},
	} {
		_, errS, code := runCmd(t, append([]string{"create", "--title", "t"}, c.args...), "")
		if code == 0 || !strings.Contains(errS, c.want) {
			t.Errorf("%s: exit = %d, stderr = %q, want non-zero with %q", name, code, errS, c.want)
		}
	}
	id := mustCreateBead(t, "--title", "t")
	out, errS, code := runCmd(t, []string{"update", id, "--severity", "9"}, "")
	if code == 0 || !strings.Contains(out+errS, "--severity must be between 1 and 4") {
		t.Errorf("update --severity 9: exit = %d, stdout = %q, stderr = %q", code, out, errS)
	}
	t.Setenv("LM_NOW", "not-a-time")
	_, errS, code = runCmd(t, []string{"create", "--title", "t2"}, "")
	if code == 0 || errS == "" {
		t.Errorf("create with LM_NOW garbage: exit = %d, stderr = %q, want non-zero with a message", code, errS)
	}
}
