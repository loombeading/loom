// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dropBeadsColumn(t *testing.T, column string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), "ALTER TABLE beads DROP COLUMN "+column); err != nil {
		t.Fatalf("drop column %s: %v", column, err)
	}
}

func TestListParentFilter(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent", "--type", "task")
	child := mustCreateBead(t, "--title", "child", "--parent", parent)
	_ = mustCreateBead(t, "--title", "grandchild", "--parent", child)
	_ = mustCreateBead(t, "--title", "unrelated")

	out, errS, code := runCmd(t, []string{"list", "--parent", parent}, "")
	if code != 0 {
		t.Fatalf("list --parent: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "child") || strings.Contains(out, "grandchild") {
		t.Fatalf("list --parent output = %q, want just the direct child", out)
	}
	if strings.Contains(out, "unrelated") {
		t.Fatalf("list --parent output = %q, must not list the unrelated Bead", out)
	}
	if !strings.HasPrefix(out, "Filter: status=\"unfinished\", parent=\"") {
		t.Fatalf("list --parent output = %q, want it to start with a status and parent Filter: line", out)
	}
}

func TestListParentFilterUnknownID(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"list", "--parent", "no-such-id"}, "")
	if code == 0 {
		t.Fatalf("list --parent no-such-id: exit code = 0, want non-zero")
	}
	if !strings.Contains(errS, "Error:") {
		t.Fatalf("list --parent no-such-id stderr = %q, want an Error: line", errS)
	}
}

func TestListUpdatedFilters(t *testing.T) {
	initBeadsDir(t)
	early := mustCreateBead(t, "--title", "early")
	time.Sleep(20 * time.Millisecond)
	mid := time.Now().UTC().Format(time.RFC3339Nano)
	time.Sleep(20 * time.Millisecond)
	late := mustCreateBead(t, "--title", "late")
	_ = early

	beforeOut, errS, code := runCmd(t, []string{"list", "--updated-before", mid}, "")
	if code != 0 {
		t.Fatalf("list --updated-before: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(beforeOut, "early") || strings.Contains(beforeOut, "late") {
		t.Fatalf("list --updated-before output = %q, want just the early Bead", beforeOut)
	}
	if !strings.Contains(beforeOut, `updated_before="`+mid+`"`) {
		t.Fatalf("list --updated-before output = %q, want the raw value on the Filter: line", beforeOut)
	}

	afterOut, errS, code := runCmd(t, []string{"list", "--updated-after", mid}, "")
	if code != 0 {
		t.Fatalf("list --updated-after: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(afterOut, "late") || strings.Contains(afterOut, "early") {
		t.Fatalf("list --updated-after output = %q, want just the late Bead", afterOut)
	}
	_ = late
}

func TestListUpdatedFilterDateOnly(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "any")

	futureDate := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	out, errS, code := runCmd(t, []string{"list", "--updated-before", futureDate}, "")
	if code != 0 {
		t.Fatalf("list --updated-before <date>: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "any") {
		t.Fatalf("list --updated-before <future date> output = %q, want the Bead listed", out)
	}
}

func TestListUpdatedFilterInvalid(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"list", "--updated-before", "not-a-time"}, "")
	if code == 0 {
		t.Fatalf("list --updated-before not-a-time: exit code = 0, want non-zero")
	}
	if !strings.Contains(errS, "Error:") {
		t.Fatalf("list --updated-before not-a-time stderr = %q, want an Error: line", errS)
	}
}

func TestFilterLineOrderAndCombination(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent", "--type", "task", "--namespace", "web")
	mustCreateBead(t, "--title", "child", "--parent", parent, "--namespace", "web")

	past := "2020-01-01"
	future := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	out, errS, code := runCmd(t, []string{"list", "--namespace", "web", "--parent", parent, "--updated-before", future, "--updated-after", past}, "")
	if code != 0 {
		t.Fatalf("list combined filters: exit code = %d, stderr = %q", code, errS)
	}
	line, _, _ := strings.Cut(out, "\n")
	if !strings.HasPrefix(line, `Filter: namespace="web", status="unfinished", parent="`) {
		t.Fatalf("Filter: line = %q, want namespace before status before parent", line)
	}
	if !strings.Contains(line, `updated_before="`+future+`", updated_after="`+past+`"`) {
		t.Fatalf("Filter: line = %q, want updated_before before updated_after", line)
	}
}

func TestReadyAndBlockedAcceptParentAndUpdatedFilters(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "parent", "--type", "task")
	child := mustCreateBead(t, "--title", "ready child", "--parent", parent)
	blocker := mustCreateBead(t, "--title", "blocker")
	mustCreateBead(t, "--title", "blocked child", "--parent", parent, "--blocked-by", blocker)
	mustCreateBead(t, "--title", "unrelated ready")

	readyOut, errS, code := runCmd(t, []string{"ready", "--parent", parent}, "")
	if code != 0 {
		t.Fatalf("ready --parent: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(readyOut, "ready child") || strings.Contains(readyOut, "unrelated ready") {
		t.Fatalf("ready --parent output = %q, want just the direct ready child", readyOut)
	}
	if !strings.HasPrefix(readyOut, "Filter: parent=\"") {
		t.Fatalf("ready --parent output = %q, want a parent Filter: line", readyOut)
	}
	_ = child

	blockedOut, errS, code := runCmd(t, []string{"blocked", "--parent", parent}, "")
	if code != 0 {
		t.Fatalf("blocked --parent: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(blockedOut, "blocked child") {
		t.Fatalf("blocked --parent output = %q, want the blocked child", blockedOut)
	}
	if strings.Contains(blockedOut, "unrelated") {
		t.Fatalf("blocked --parent output = %q, must not list the unrelated Bead", blockedOut)
	}
}

func TestListReturnsErrorWhenRowQueryFails(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "t")
	dropBeadsColumn(t, "external_refs")

	_, errS, code := runCmd(t, []string{"list"}, "")
	if code != 1 {
		t.Fatalf("list: exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(errS, "Error: ") {
		t.Errorf("list stderr = %q, want an Error: line", errS)
	}
}

func TestReadyReturnsErrorWhenRowQueryFails(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "t")
	dropBeadsColumn(t, "external_refs")

	_, errS, code := runCmd(t, []string{"ready"}, "")
	if code != 1 {
		t.Fatalf("ready: exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(errS, "Error: ") {
		t.Errorf("ready stderr = %q, want an Error: line", errS)
	}
}

func TestReadyClaimReturnsErrorWhenRowQueryFails(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "t")
	dropBeadsColumn(t, "external_refs")

	_, errS, code := runCmd(t, []string{"ready", "--claim", "--actor", "tester"}, "")
	if code != 1 {
		t.Fatalf("ready --claim: exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(errS, "Error: ") {
		t.Errorf("ready --claim stderr = %q, want an Error: line", errS)
	}
}
