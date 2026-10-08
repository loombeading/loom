// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/storage"
)

func initBeadsDir(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)
	t.Setenv("LM_ACTOR", "test-actor")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init: exit code = %d, stderr = %q", code, stderr.String())
	}
}

func beadsDir(t *testing.T) string {
	t.Helper()
	dir, _, err := storage.Locate(os.Getenv)
	if err != nil {
		t.Fatalf("storage.Locate: %v", err)
	}
	return dir
}

func beadsDBPath(t *testing.T) string {
	t.Helper()
	return storage.DBPath(beadsDir(t))
}

func setIsolatedXDGHome(t *testing.T) {
	t.Helper()
	t.Setenv("LM_DIR", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
}

func runCmd(t *testing.T, args []string, stdin string) (string, string, int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errBuf)
	return out.String(), errBuf.String(), code
}

func frontMatterStringValue(line, key string) string {
	v := strings.TrimPrefix(line, key+": ")
	return strings.TrimSuffix(strings.TrimPrefix(v, `"`), `"`)
}

func mustCreateBead(t *testing.T, extra ...string) string {
	t.Helper()
	alias := mustCreateBeadAlias(t, extra...)
	return mustCanonicalID(t, alias)
}

func mustCreateBeadAlias(t *testing.T, extra ...string) string {
	t.Helper()
	args := append([]string{"create"}, extra...)
	out, errS, code := runCmd(t, withAdjudication(t, args), "")
	if code != 0 {
		t.Fatalf("create %v: exit code = %d, stderr = %q", extra, code, errS)
	}
	return mustParseCreatedID(t, out)
}

func mustCanonicalID(t *testing.T, alias string) string {
	t.Helper()
	out, errS, code := runCmd(t, []string{"show", alias}, "")
	if code != 0 {
		t.Fatalf("show %q: exit code = %d, stderr = %q", alias, code, errS)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "id: ") {
			return frontMatterStringValue(line, "id")
		}
	}
	t.Fatalf("show %q output missing an id line:\n%s", alias, out)
	return ""
}

func mustDisplayAlias(t *testing.T, id string) string {
	t.Helper()
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show %q: exit code = %d, stderr = %q", id, code, errS)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "alias: ") {
			return frontMatterStringValue(line, "alias")
		}
	}
	t.Fatalf("show %q output missing an alias line:\n%s", id, out)
	return ""
}

func mustParseCreatedID(t *testing.T, out string) string {
	t.Helper()
	const prefix = "- Created: "
	line := strings.TrimSpace(out)
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("create output %q: missing %q prefix", out, prefix)
	}
	rest := strings.TrimPrefix(line, prefix)
	id, _, _ := strings.Cut(rest, " ")
	if id == "" {
		t.Fatalf("create output %q: empty ID", out)
	}
	return id
}

func TestCreateShow(t *testing.T) {
	initBeadsDir(t)

	id := mustCreateBead(t, "--title", "my task", "--description", "the body", "-p", "1", "--type", "task", "--label", "b", "--label", "a")

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	for _, want := range []string{"my task", "the body", `status: "open"`, "priority: 1", `type: "task"`, `labels: ["a", "b"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestCreateWithStdinDescription(t *testing.T) {
	initBeadsDir(t)
	args := []string{"create", "--title", "t", "--description", "-"}
	out, errS, code := runCmd(t, args, "multi\nline body")
	if code != 0 {
		t.Fatalf("create: exit code = %d, stderr = %q", code, errS)
	}
	id := mustParseCreatedID(t, out)

	showOut, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, "multi\nline body") {
		t.Errorf("show output missing stdin-provided description:\n%s", showOut)
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"create"}, "")
	if code != 1 {
		t.Fatalf("create without --title: exit code = %d, want 1", code)
	}
	if !strings.Contains(errS, "Error:") {
		t.Fatalf("stderr = %q, want an Error: line", errS)
	}
}

func TestCreateGateWithoutKindLabelRejected(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"create", "--title", "g", "--type", "gate"}, "")
	if code != 1 {
		t.Fatalf("create --type gate without --label kind:...: exit code = %d, want 1", code)
	}
	if !strings.Contains(errS, "kind:") {
		t.Errorf("stderr = %q, want it to mention the kind: label requirement", errS)
	}

	out, _, code := runCmd(t, []string{"create", "--title", "g", "--type", "gate", "--label", "kind:adjudicate"}, "")
	if code != 0 {
		t.Fatalf("create --type gate with --label kind:adjudicate: exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "Created:") {
		t.Errorf("stdout = %q, want a Created: line", out)
	}
}

func TestCreateRequireMilestone(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "epic", "--label", "milestone:x")

	writeBeadsConfig(t, `{"create":{"require_milestone":true}}`)
	_, errS, code := runCmd(t, []string{"create", "--title", "orphan"}, "")
	if code != 1 {
		t.Fatalf("create without --parent: exit code = %d, want 1", code)
	}
	if !strings.Contains(errS, "--no-milestone") || !strings.Contains(errS, "lm ahead") {
		t.Errorf("stderr = %q, want the --parent / --no-milestone guidance", errS)
	}
	out, _, _ := runCmd(t, []string{"list", "--all"}, "")
	if strings.Contains(out, "orphan") {
		t.Errorf("rejected create still wrote the Bead:\n%s", out)
	}

	mustCreateBead(t, "--title", "child", "--parent", parent)
	mustCreateBead(t, "--title", "g", "--type", "gate", "--label", "kind:adjudicate")
	id := mustCreateBead(t, "--title", "loose", "--no-milestone", "one-off chore")
	out, _, code = runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(out, "no-milestone: one-off chore") {
		t.Errorf("show history missing the --no-milestone reason:\n%s", out)
	}
	if !strings.Contains(out, `"exempt:milestone"`) {
		t.Errorf("--no-milestone did not add the exempt:milestone label:\n%s", out)
	}

	writeBeadsConfig(t, `{"create":{"require_milestone":false}}`)
	unaffiliated := mustCreateBead(t, "--title", "unaffiliated epic")
	writeBeadsConfig(t, `{"create":{"require_milestone":true}}`)
	_, errS, code = runCmd(t, []string{"create", "--title", "orphan child", "--parent", unaffiliated}, "")
	if code != 1 || !strings.Contains(errS, "親が無所属") {
		t.Fatalf("create under unaffiliated parent: exit code = %d stderr = %q, want 1 and 親が無所属", code, errS)
	}
	out, _, _ = runCmd(t, []string{"list", "--all"}, "")
	if strings.Contains(out, "orphan child") {
		t.Errorf("rejected create still wrote the Bead:\n%s", out)
	}
	for _, label := range []string{"domain:loom", "exempt:milestone"} {
		if _, errS, code = runCmd(t, []string{"create", "--title", "labelled child", "--parent", unaffiliated, "--label", label}, ""); code != 1 || !strings.Contains(errS, "親が無所属") {
			t.Fatalf("create under unaffiliated parent with %s: exit code = %d stderr = %q, want 1 and 親が無所属", label, code, errS)
		}
	}
	mustCreateBead(t, "--title", "gate child", "--parent", unaffiliated, "--type", "gate", "--label", "kind:adjudicate")
	mid := mustCreateBead(t, "--title", "mid", "--parent", id)
	mustCreateBead(t, "--title", "grandchild", "--parent", mid)

	writeBeadsConfig(t, `{"create":{"require_milestone":false}}`)
	mustCreateBead(t, "--title", "unchecked")
}

func TestCreateAndUpdateRejectReleaseLabel(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"create", "--title", "orphan", "--no-milestone", "x", "--label", "release:v1"}, "")
	if code != 1 {
		t.Fatalf("create --label release:v1: exit code = %d, want 1", code)
	}
	if !strings.Contains(errS, "release:v1") || !strings.Contains(errS, "milestone:v1") {
		t.Errorf("stderr = %q, want it to name both release:v1 and milestone:v1", errS)
	}
	out, _, _ := runCmd(t, []string{"list", "--all"}, "")
	if strings.Contains(out, "orphan") {
		t.Errorf("rejected create still wrote the Bead:\n%s", out)
	}

	id := mustCreateBead(t, "--title", "t", "--no-milestone", "x")
	updateOut, errS, code := runCmd(t, []string{"update", id, "--add-label", "release:v1"}, "")
	if code != 1 {
		t.Fatalf("update --add-label release:v1: exit code = %d, want 1", code)
	}
	if !strings.Contains(updateOut, "milestone:v1") && !strings.Contains(errS, "milestone:v1") {
		t.Errorf("stdout = %q, stderr = %q, want one of them to name milestone:v1", updateOut, errS)
	}
	out, _, code = runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(out, `labels: ["exempt:milestone"]`) {
		t.Errorf("show output = %q, want only exempt:milestone (rejected update wrote nothing)", out)
	}
}

func TestCreateWithParentAndBlockedByIsAtomic(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "epic", "--type", "task")
	blocker := mustCreateBead(t, "--title", "blocker")
	child := mustCreateBead(t, "--title", "child", "--parent", parent, "--blocked-by", blocker)

	out, _, code := runCmd(t, []string{"show", child}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(out, "blocks") || !strings.Contains(out, "parent-child") {
		t.Errorf("show output missing dependency links:\n%s", out)
	}

	_, errS, code := runCmd(t, []string{"create", "--title", "orphan", "--parent", "deadbeefdeadbeef"}, "")
	if code == 0 {
		t.Fatal("create with a bad --parent = exit 0, want a rejection")
	}
	if !strings.Contains(errS, "Error:") {
		t.Fatalf("stderr = %q, want an Error: line", errS)
	}
	out, _, _ = runCmd(t, []string{"list", "--all"}, "")
	if strings.Contains(out, "orphan") {
		t.Errorf("list output contains the orphan Bead despite the failed --parent:\n%s", out)
	}
}

func TestCreateChildDoesNotInheritParentLabels(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "epic", "--type", "task", "--label", "parent-label")
	child := mustCreateBead(t, "--title", "child", "--parent", parent)

	out, errS, code := runCmd(t, []string{"show", child}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "parent-label") {
		t.Errorf("child show output contains the parent's label:\n%s", out)
	}
	if !strings.Contains(out, `labels: []`) {
		t.Errorf("show output missing empty labels for child:\n%s", out)
	}
}

func TestListDefaultAndAll(t *testing.T) {
	initBeadsDir(t)
	open := mustCreateBead(t, "--title", "open task")
	closedID := mustCreateBead(t, "--title", "closed task")
	if _, errS, code := runCmd(t, []string{"close", closedID}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatal("list failed")
	}
	if !strings.Contains(out, "open task") || strings.Contains(out, "closed task") {
		t.Errorf("default list = %q, want open task only", out)
	}
	_ = open

	out, _, code = runCmd(t, []string{"list", "--all"}, "")
	if code != 0 {
		t.Fatal("list --all failed")
	}
	if !strings.Contains(out, "open task") || !strings.Contains(out, "closed task") {
		t.Errorf("list --all = %q, want both Beads", out)
	}
}

func TestListStatusTerminalWithoutAll(t *testing.T) {
	initBeadsDir(t)
	open := mustCreateBead(t, "--title", "open task")
	closedID := mustCreateBead(t, "--title", "closed task")
	if _, errS, code := runCmd(t, []string{"close", closedID}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	cancelledID := mustCreateBead(t, "--title", "cancelled task")
	if _, errS, code := runCmd(t, []string{"update", "--status", "cancelled", cancelledID}, ""); code != 0 {
		t.Fatalf("cancel: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"list", "--status", "closed"}, "")
	if code != 0 {
		t.Fatal("list --status closed failed")
	}
	if !strings.Contains(out, "closed task") || strings.Contains(out, "open task") || strings.Contains(out, "cancelled task") {
		t.Errorf("list --status closed = %q, want closed task only", out)
	}
	if !strings.Contains(out, `Filter: status="closed"`) {
		t.Errorf("list --status closed missing Filter line:\n%s", out)
	}

	out, _, code = runCmd(t, []string{"list", "--status", "cancelled"}, "")
	if code != 0 {
		t.Fatal("list --status cancelled failed")
	}
	if !strings.Contains(out, "cancelled task") || strings.Contains(out, "open task") || strings.Contains(out, "closed task") {
		t.Errorf("list --status cancelled = %q, want cancelled task only", out)
	}

	out, _, code = runCmd(t, []string{"list", "--status", "open"}, "")
	if code != 0 {
		t.Fatal("list --status open failed")
	}
	if !strings.Contains(out, "open task") || strings.Contains(out, "closed task") || strings.Contains(out, "cancelled task") {
		t.Errorf("list --status open = %q, want open task only", out)
	}

	out, _, code = runCmd(t, []string{"list", "--all", "--status", "closed"}, "")
	if code != 0 {
		t.Fatal("list --all --status closed failed")
	}
	if !strings.Contains(out, "closed task") || strings.Contains(out, "open task") || strings.Contains(out, "cancelled task") {
		t.Errorf("list --all --status closed = %q, want closed task only", out)
	}
	_ = open
}

func TestListNamespaceFilterLine(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "web task", "--namespace", "web")
	mustCreateBead(t, "--title", "lm task")

	out, _, code := runCmd(t, []string{"list", "--namespace", "web"}, "")
	if code != 0 {
		t.Fatal("list failed")
	}
	if !strings.Contains(out, `Filter: namespace="web"`) {
		t.Errorf("list output missing Filter line:\n%s", out)
	}
	if !strings.Contains(out, "web task") || strings.Contains(out, "lm task") {
		t.Errorf("list --namespace web = %q, want just the web task", out)
	}
}

func TestUpdateClaimReleaseReopen(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	if _, errS, code := runCmd(t, []string{"update", "--claim", "--actor", "alice", id}, ""); code != 0 {
		t.Fatalf("claim: exit code = %d, stderr = %q", code, errS)
	}

	out, _, code := runCmd(t, []string{"update", "--claim", "--actor", "bob", id}, "")
	if code != 1 {
		t.Fatalf("second claim: exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "Rejected:") {
		t.Errorf("stdout = %q, want a Rejected: line", out)
	}

	if _, errS, code := runCmd(t, []string{"update", "--release", "--actor", "alice", id}, ""); code != 0 {
		t.Fatalf("release exit code = %d, stderr = %q", code, errS)
	}

	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"update", "--reopen", id}, ""); code != 0 {
		t.Fatalf("reopen: exit code = %d, stderr = %q", code, errS)
	}

	showOut, _, _ := runCmd(t, []string{"show", id}, "")
	if !strings.Contains(showOut, `status: "open"`) {
		t.Errorf("show after reopen = %q, want status: \"open\"", showOut)
	}
}

func TestUpdateReasonOnly(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	if _, errS, code := runCmd(t, []string{"update", "--reason", "handoff", id}, ""); code != 0 {
		t.Fatalf("reason-only update: exit code = %d, stderr = %q", code, errS)
	}
	out, _, code := runCmd(t, []string{"update", id}, "")
	if code != 1 {
		t.Fatalf("update with no flags at all: exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "Rejected:") {
		t.Errorf("stdout = %q, want a Rejected: line", out)
	}
}

func TestUpdateMultipleIDsPartialRejection(t *testing.T) {
	initBeadsDir(t)
	ok := mustCreateBead(t, "--title", "ok")
	gate := mustCreateBead(t, "--title", "g", "--type", "gate", "--label", "kind:human")

	out, _, code := runCmd(t, []string{"close", ok, gate}, "")
	if code != 1 {
		t.Fatalf("close [ok, gate]: exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "- Closed:") {
		t.Errorf("stdout = %q, want the ok Bead to have closed", out)
	}
	if !strings.Contains(out, "Rejected:") {
		t.Errorf("stdout = %q, want the gate Bead to be rejected", out)
	}
	if !strings.Contains(out, "## Newly ready") || !strings.Contains(out, "(none)") {
		t.Errorf("stdout = %q, want the Newly ready section", out)
	}
}

func TestExternalRefCreateUpdateAddRemove(t *testing.T) {
	initBeadsDir(t)

	id := mustCreateBead(t, "--title", "t", "--external-ref", "https://github.com/example/repo/pull/1")
	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(out, `external_refs: ["https://github.com/example/repo/pull/1"]`) {
		t.Errorf("show output missing external_refs line:\n%s", out)
	}

	if _, errS, code := runCmd(t, []string{"update", "--external-ref", "https://github.com/example/repo/pull/2", id}, ""); code != 0 {
		t.Fatalf("update external-ref: exit code = %d, stderr = %q", code, errS)
	}
	out, _, _ = runCmd(t, []string{"show", id}, "")
	if !strings.Contains(out, `external_refs: ["https://github.com/example/repo/pull/1", "https://github.com/example/repo/pull/2"]`) {
		t.Errorf("show output missing updated external_refs line:\n%s", out)
	}
	if !strings.Contains(out, "external_refs") {
		t.Errorf("show history missing external_refs audit field:\n%s", out)
	}

	if _, errS, code := runCmd(t, []string{"update", "--remove-external-ref", "https://github.com/example/repo/pull/1", id}, ""); code != 0 {
		t.Fatalf("update remove-external-ref: exit code = %d, stderr = %q", code, errS)
	}
	out, _, _ = runCmd(t, []string{"show", id}, "")
	if !strings.Contains(out, `external_refs: ["https://github.com/example/repo/pull/2"]`) {
		t.Errorf(`show output after removing = %q, want external_refs: ["https://github.com/example/repo/pull/2"]`, out)
	}
}

func TestUpdatePartialCreatesFollowUpBead(t *testing.T) {
	initBeadsDir(t)

	epic := mustCreateBead(t, "--title", "epic")
	src := mustCreateBead(t, "--title", "src", "--parent", epic, "--priority", "1")

	out, errS, code := runCmd(t, []string{
		"update", src,
		"--external-ref", "https://github.com/example/repo/pull/1",
		"--partial", "leftover work",
	}, "")
	if code != 0 {
		t.Fatalf("update --partial: exit code = %d, stderr = %q", code, errS)
	}
	var partialID string
	for line := range strings.SplitSeq(out, "\n") {
		if after, ok := strings.CutPrefix(line, "- Created: "); ok {
			partialID = mustCanonicalID(t, strings.SplitN(after, " ", 2)[0])
		}
	}
	if partialID == "" {
		t.Fatalf("update --partial output missing a Created line:\n%s", out)
	}

	show, _, code := runCmd(t, []string{"show", partialID}, "")
	if code != 0 {
		t.Fatalf("show partial: exit code = %d", code)
	}
	if !strings.Contains(show, `title: "leftover work"`) {
		t.Errorf("partial show missing title:\n%s", show)
	}
	if !strings.Contains(show, `priority: 1`) {
		t.Errorf("partial show priority mismatch (want same as src, priority 1):\n%s", show)
	}
	if !strings.Contains(show, "parent-child ← "+mustDisplayAlias(t, epic)) {
		t.Errorf("partial show missing parent-child link to epic:\n%s", show)
	}
	if !strings.Contains(show, "blocks ← "+mustDisplayAlias(t, src)) {
		t.Errorf("partial show missing blocks link to src:\n%s", show)
	}
	if !strings.Contains(show, "discovered-from ← "+mustDisplayAlias(t, src)) {
		t.Errorf("partial show missing discovered-from link to src:\n%s", show)
	}

	readyBefore, _, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready before close: exit code = %d", code)
	}
	if strings.Contains(readyBefore, partialID) {
		t.Errorf("ready before src closes = %q, should not list the partial Bead yet", readyBefore)
	}

	if _, errS, code := runCmd(t, []string{"close", src, "--reason", "done"}, ""); code != 0 {
		t.Fatalf("close src: exit code = %d, stderr = %q", code, errS)
	}

	readyAfter, _, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready after close: exit code = %d", code)
	}
	if !strings.Contains(readyAfter, mustDisplayAlias(t, partialID)) {
		t.Errorf("ready after src closes = %q, want the partial Bead listed", readyAfter)
	}
}

func TestUpdatePartialRequiresExternalRef(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	_, errS, code := runCmd(t, []string{"update", id, "--partial", "leftover"}, "")
	if code == 0 {
		t.Fatalf("update --partial without --external-ref: exit code = 0, want non-zero; stderr = %q", errS)
	}
}

func TestUpdatePartialRejectsEmptyValue(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	_, errS, code := runCmd(t, []string{
		"update", id,
		"--external-ref", "https://github.com/example/repo/pull/1",
		"--partial", "",
	}, "")
	if code == 0 {
		t.Fatalf("update --partial \"\": exit code = 0, want non-zero; stderr = %q", errS)
	}
}

func TestExternalRefDefaultNone(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(out, `external_refs: []`) {
		t.Errorf(`show output = %q, want external_refs: []`, out)
	}
}

func TestExternalRefEscapedInShow(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--external-ref", "a|b\nc")
	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if strings.Contains(out, "a|b\nc") {
		t.Errorf("show output contains unescaped external-ref value:\n%s", out)
	}
}

func TestExternalRefInList(t *testing.T) {
	initBeadsDir(t)
	prID := mustCreateBead(t, "--title", "t1", "--external-ref", "https://github.com/example/repo/pull/105")
	refID := mustCreateBead(t, "--title", "t2", "--external-ref", "https://example.com/pr/1")
	noneID := mustCreateBead(t, "--title", "t3")
	prID, refID, noneID = mustDisplayAlias(t, prID), mustDisplayAlias(t, refID), mustDisplayAlias(t, noneID)

	out, _, code := runCmd(t, []string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit code = %d", code)
	}
	if !strings.Contains(out, prID+" [#105/P2] t1\n") {
		t.Errorf("list output missing #105 for the GitHub PR URL:\n%s", out)
	}
	if !strings.Contains(out, refID+" [open/P2] t2\n") {
		t.Errorf("list output must show status for a non-PR external_ref:\n%s", out)
	}
	if !strings.Contains(out, noneID+" [open/P2] t3\n") {
		t.Errorf("list output must show status when external_ref is unset:\n%s", out)
	}
}

func TestCloseGateRejected(t *testing.T) {
	initBeadsDir(t)
	gate := mustCreateBead(t, "--title", "g", "--type", "gate", "--label", "kind:human")
	out, _, code := runCmd(t, []string{"close", gate}, "")
	if code != 1 {
		t.Fatalf("close gate: exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "Rejected:") {
		t.Errorf("stdout = %q, want a Rejected: line", out)
	}
}

func TestCostAddAndShow(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, errS, code := runCmd(t, []string{"cost", "add", id, "--in", "100", "--out", "50"}, "")
	if code != 0 {
		t.Fatalf("cost add: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "Token cost recorded:") {
		t.Errorf("cost add stdout = %q, want a Token cost recorded: line", out)
	}

	_, errS, code = runCmd(t, []string{"cost", "add", id, "--in", "10", "--out", "0"}, "")
	if code != 0 {
		t.Fatalf("cost add (2nd): exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code = runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "token_cost: 160") {
		t.Errorf("show output missing token cost summary:\n%s", out)
	}
	if !strings.Contains(out, "token_cost") {
		t.Errorf("show output missing token_cost history entry:\n%s", out)
	}
}

func TestCostAddNoRecordsShowsZero(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "token_cost: 0\n") {
		t.Errorf("show output missing zero token cost:\n%s", out)
	}
}

func TestCostAddRejectsBothZero(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"cost", "add", id}, "")
	if code != 1 {
		t.Fatalf("cost add with no --in/--out: exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "Rejected:") {
		t.Errorf("stdout = %q, want a Rejected: line", out)
	}
}

func TestCostAddRejectsNegative(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"cost", "add", id, "--in", "-1"}, "")
	if code != 1 {
		t.Fatalf("cost add with negative --in: exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "Rejected:") {
		t.Errorf("stdout = %q, want a Rejected: line", out)
	}
}

func TestSummaryCreateUpdate(t *testing.T) {
	initBeadsDir(t)

	id := mustCreateBead(t, "--title", "t", "--summary", "S1")
	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(out, "## Summary\n\nS1\n") {
		t.Errorf("show output missing Summary section:\n%s", out)
	}

	if _, errS, code := runCmd(t, []string{"update", "--summary", "S2", id}, ""); code != 0 {
		t.Fatalf("update summary: exit code = %d, stderr = %q", code, errS)
	}
	out, _, _ = runCmd(t, []string{"show", id}, "")
	if !strings.Contains(out, "## Summary\n\nS2\n") {
		t.Errorf("show output missing updated Summary section:\n%s", out)
	}
	if !strings.Contains(out, "summary") {
		t.Errorf("show history missing summary audit field:\n%s", out)
	}
}

func TestSummaryDefaultNone(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if strings.Contains(out, "## Summary") {
		t.Errorf("show output = %q, want the Summary section omitted when unset", out)
	}
}

func TestSummaryStdin(t *testing.T) {
	initBeadsDir(t)
	out, errS, code := runCmd(t, []string{"create", "--title", "t", "--summary", "-"}, "from stdin")
	if code != 0 {
		t.Fatalf("create --summary -: exit code = %d, stderr = %q", code, errS)
	}
	id := mustParseCreatedID(t, out)
	out, _, _ = runCmd(t, []string{"show", id}, "")
	if !strings.Contains(out, "## Summary\n\nfrom stdin\n") {
		t.Errorf("show output = %q, want Summary read from stdin", out)
	}
}

func TestSearchMatchesSummary(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "t", "--summary", "uniquesummaryword")

	out, errS, code := runCmd(t, []string{"search", "uniquesummaryword"}, "")
	if code != 0 {
		t.Fatalf("search: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- "+domain.DefaultNamespace+"-") {
		t.Errorf("search output = %q, want a match on the summary", out)
	}
}

func TestShowRejectsInvalidBDNow(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	t.Setenv("LM_NOW", "not-a-time")
	_, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 1 {
		t.Fatalf("show with invalid LM_NOW: exit code = %d, want 1", code)
	}
	if !strings.Contains(errS, "Error:") {
		t.Errorf("stderr = %q, want an Error: line", errS)
	}
}

func TestShowAcceptsValidBDNow(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	t.Setenv("LM_NOW", "2026-01-02T03:04:05Z")
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show with valid LM_NOW: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, `title: "t"`) {
		t.Errorf("show output = %q, want the Bead's own attributes", out)
	}
}

func TestBDNowDoesNotAffectRecordedTimestamps(t *testing.T) {
	initBeadsDir(t)

	t.Setenv("LM_NOW", "2000-01-01T00:00:00Z")
	id := mustCreateBead(t, "--title", "t")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if strings.Contains(out, "2000-01-01") {
		t.Errorf("show output = %q, want created_at unaffected by LM_NOW", out)
	}
	currentYear := time.Now().UTC().Format("2006")
	if !strings.Contains(out, `created_at: "`+currentYear) {
		t.Errorf("show output = %q, want a created_at line from the real clock (year %s)", out, currentYear)
	}
}

func TestCreateSetsDepth(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--reasoning-depth", "4")

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "reasoning_depth: 4") {
		t.Errorf("show output missing %q:\n%s", "reasoning_depth: 4", out)
	}
}

func TestCreateInvalidDepthRejected(t *testing.T) {
	initBeadsDir(t)
	_, errS, code := runCmd(t, []string{"create", "--title", "t", "--reasoning-depth", "9"}, "")
	if code == 0 {
		t.Fatalf("create --reasoning-depth 9: exit code = 0, want non-zero")
	}
	if !strings.Contains(errS, "Error:") {
		t.Errorf("create --reasoning-depth 9 stderr = %q, want an Error: line", errS)
	}
}
