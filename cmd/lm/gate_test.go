// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func dropTable(t *testing.T, table string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), "DROP TABLE "+table); err != nil {
		t.Fatalf("drop table %s: %v", table, err)
	}
}

func corruptLabels(t *testing.T, id string) {
	t.Helper()
	canonical := mustCanonicalID(t, id)
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()
	res, err := db.ExecContext(t.Context(), "UPDATE beads SET labels = ? WHERE id = ?", "not-json", canonical)
	if err != nil {
		t.Fatalf("corrupt labels for %s: %v", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("corrupt labels for %s: RowsAffected = %d, err = %v, want exactly 1 row updated", id, n, err)
	}
}

func corruptDepth(t *testing.T, id string) {
	t.Helper()
	canonical := mustCanonicalID(t, id)
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()
	res, err := db.ExecContext(t.Context(), "UPDATE beads SET reasoning_depth = ? WHERE id = ?", "not-json", canonical)
	if err != nil {
		t.Fatalf("corrupt depth for %s: %v", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("corrupt depth for %s: RowsAffected = %d, err = %v, want exactly 1 row updated", id, n, err)
	}
}

func corruptCreatedAt(t *testing.T, id string) {
	t.Helper()
	canonical := mustCanonicalID(t, id)
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()
	res, err := db.ExecContext(t.Context(), "UPDATE beads SET created_at = ? WHERE id = ?", "not-a-timestamp", canonical)
	if err != nil {
		t.Fatalf("corrupt created_at for %s: %v", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("corrupt created_at for %s: RowsAffected = %d, err = %v, want exactly 1 row updated", id, n, err)
	}
}

func rawBeadStatus(t *testing.T, canonical string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()
	var status string
	if err := db.QueryRowContext(t.Context(), "SELECT status FROM beads WHERE id = ?", canonical).Scan(&status); err != nil {
		t.Fatalf("read status for %s: %v", canonical, err)
	}
	return status
}

func mustGateCreateCmd(t *testing.T, blocks []string, subject string, extra ...string) string {
	t.Helper()
	args := []string{"gate", "create"}
	for _, b := range blocks {
		args = append(args, "--blocks", b)
	}
	args = append(args, "--subject", subject)
	hasKind := slices.Contains(extra, "--kind")
	isExternal := false
	for i, a := range extra {
		if a == "--kind" && i+1 < len(extra) && extra[i+1] == "external" {
			isExternal = true
		}
	}
	hasResolver := slices.Contains(extra, "--resolver")
	args = append(args, extra...)
	if !hasKind {
		args = append(args, "--kind", "human")
	}
	if isExternal && !hasResolver {
		args = append(args, "--resolver", "watcher")
	}
	out, errS, code := runCmd(t, withAdjudication(t, args), "")
	if code != 0 {
		t.Fatalf("gate create: exit code = %d, stderr = %q, stdout = %q", code, errS, out)
	}
	return mustParseCreatedID(t, out)
}

func withAdjudication(t *testing.T, args []string) []string {
	t.Helper()
	person := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--kind" && (args[i+1] == "human" || args[i+1] == "confirm") ||
			args[i] == "--label" && (args[i+1] == "kind:human" || args[i+1] == "kind:confirm") {
			person = true
		}
	}
	if !person || slices.Contains(args, "--adjudicated-by") {
		return args
	}
	judge := mustClosedJudge(t)
	if args[0] == "create" {
		return append(args, "--label", "adjudicated-by:"+judge)
	}
	return append(args, "--adjudicated-by", judge)
}

func mustClosedJudge(t *testing.T) string {
	t.Helper()
	out, errS, code := runCmd(t, []string{"create", "--title", "judge", "--type", "gate", "--label", "kind:adjudicate"}, "")
	if code != 0 {
		t.Fatalf("create judge: exit code = %d, stderr = %q", code, errS)
	}
	id := mustCanonicalID(t, mustParseCreatedID(t, out))
	if _, errS, code := runCmd(t, []string{"gate", "resolve", id, "--reason", "judged"}, ""); code != 0 {
		t.Fatalf("resolve judge: exit code = %d, stderr = %q", code, errS)
	}
	return id
}

func TestGateCreateBlocksTarget(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	mustGateCreateCmd(t, []string{target}, "CI green")

	readyOut, _, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d", code)
	}
	if strings.Contains(readyOut, "target") {
		t.Errorf("ready output = %q, must not list the gate-blocked target", readyOut)
	}
}

func TestGateCreateKindConfirmAddsLabel(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	id := mustGateCreateCmd(t, []string{target}, "CI green", "--kind", "confirm")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(out, `", "kind:confirm"]`+"\n") {
		t.Errorf("show output = %q, want labels: [\"kind:confirm\"]", out)
	}
}

func TestGateCreateKindAgentRejected(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	kindVal := "agent"
	_, errS, code := runCmd(t, []string{"gate", "create", "--blocks", target, "--subject", "CI green; pipeline will resolve", "--kind", kindVal}, "")
	if code != 1 {
		t.Fatalf("gate create with kind=%s: exit code = %d, want 1", kindVal, code)
	}
	wantErr := `invalid --kind "agent", must be one of: human, external, adjudicate, confirm`
	if !strings.Contains(errS, wantErr) {
		t.Errorf("stderr = %q, want it to contain %q", errS, wantErr)
	}
}

func TestGateCreateKindExternalAddsLabel(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	id := mustGateCreateCmd(t, []string{target}, "webhook fired", "--kind", "external")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(out, `labels: ["kind:external"]`+"\n") {
		t.Errorf("show output = %q, want labels: [\"kind:external\"]", out)
	}
}

func TestGateCreateKindBogusRejected(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	_, errS, code := runCmd(t, []string{"gate", "create", "--blocks", target, "--subject", "x", "--kind", "bogus"}, "")
	if code == 0 {
		t.Fatalf("gate create --kind bogus succeeded, stderr = %q", errS)
	}
	for _, want := range []string{"human", "external", "adjudicate", "confirm"} {
		if !strings.Contains(errS, want) {
			t.Errorf("stderr = %q, want it to list the allowed --kind value %q", errS, want)
		}
	}
}

func TestGateCreateKindHumanAddsLabel(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	id := mustGateCreateCmd(t, []string{target}, "CI green", "--kind", "human")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(out, `", "kind:human"]`+"\n") {
		t.Errorf("show output = %q, want labels: [\"kind:human\"]", out)
	}
}

func TestGateCreateKindAdjudicateAddsLabel(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	id := mustGateCreateCmd(t, []string{target}, "判定待ち", "--kind", "adjudicate")

	out, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(out, `labels: ["kind:adjudicate"]`+"\n") {
		t.Errorf("show output = %q, want labels: [\"kind:adjudicate\"]", out)
	}
}

func TestGateCreateKindMissingRejected(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	out, errS, code := runCmd(t, []string{"gate", "create", "--blocks", target, "--subject", "CI green"}, "")
	if code == 0 {
		t.Fatalf("gate create without --kind succeeded, stdout = %q", out)
	}
	if !strings.Contains(errS, "--kind is required") {
		t.Errorf("stderr = %q, want it to contain %q", errS, "--kind is required")
	}

	listOut, _, code := runCmd(t, []string{"gate", "--all"}, "")
	if code != 0 {
		t.Fatalf("lm gate --all: exit code = %d", code)
	}
	if strings.TrimSpace(listOut) != "(none)" {
		t.Errorf("lm gate --all after a rejected create = %q, want (none) (no Gate was created)", listOut)
	}
}

func TestGateCreateRequiresBlocksAndSubject(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	if _, errS, code := runCmd(t, []string{"gate", "create", "--subject", "x"}, ""); code == 0 {
		t.Fatalf("gate create without --blocks succeeded, stderr = %q", errS)
	}
	if _, errS, code := runCmd(t, []string{"gate", "create", "--blocks", target}, ""); code == 0 {
		t.Fatalf("gate create without --subject succeeded, stderr = %q", errS)
	}
}

func TestGateCreateKindExternalWithoutResolverRejected(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	_, errS, code := runCmd(t, []string{"gate", "create", "--blocks", target, "--subject", "別セッションの完了待ち", "--kind", "external"}, "")
	if code == 0 {
		t.Fatalf("gate create --kind external without --resolver succeeded, stderr = %q", errS)
	}
	if !strings.Contains(errS, "--resolver is required") {
		t.Errorf("stderr = %q, want it to contain %q", errS, "--resolver is required")
	}

	listOut, _, code := runCmd(t, []string{"gate", "--all"}, "")
	if code != 0 {
		t.Fatalf("lm gate --all: exit code = %d", code)
	}
	if strings.TrimSpace(listOut) != "(none)" {
		t.Errorf("lm gate --all after a rejected create = %q, want (none) (no Gate was created)", listOut)
	}
}

func TestGateCreateKindExternalWithResolverSucceeds(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	_, errS, code := runCmd(t, []string{"gate", "create", "--blocks", target, "--subject", "PR の受入待ち", "--kind", "external", "--resolver", "受入を終えたセッション"}, "")
	if code != 0 {
		t.Fatalf("gate create: exit code = %d, stderr = %q", code, errS)
	}
	if errS != "" {
		t.Errorf("stderr = %q, want empty", errS)
	}
}

func TestPersonGateWithoutAdjudicationRejectedOnEveryEntry(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	open := mustGateCreateCmd(t, []string{target}, "judge", "--kind", "adjudicate")
	const want = "Rejected: new (a human/confirm Gate needs adjudicated-by:<closed adjudicate Gate>; create it with --kind adjudicate)\n"
	for _, args := range [][]string{
		{"gate", "create", "--blocks", target, "--subject", "s", "--kind", "human"},
		{"gate", "create", "--blocks", target, "--subject", "s", "--kind", "confirm", "--adjudicated-by", open},
		{"create", "--title", "g", "--type", "gate", "--label", "kind:human"},
	} {
		out, errS, code := runCmd(t, args, "")
		if code != 1 || errS != want || out != "" {
			t.Errorf("%v: exit=%d stdout=%q stderr=%q, want exit 1 and stderr %q", args, code, out, errS, want)
		}
	}
	out, errS, code := runCmd(t, []string{"update", open, "--add-label", "kind:human"}, "")
	if code != 1 || !strings.HasPrefix(errS, "Rejected: "+open+" (a human/confirm Gate needs adjudicated-by:") || strings.Contains(out, "Updated") {
		t.Errorf("update --add-label kind:human: exit=%d stdout=%q stderr=%q", code, out, errS)
	}

	judge := mustClosedJudge(t)
	if _, errS, code := runCmd(t, []string{"gate", "create", "--blocks", target, "--subject", "s", "--kind", "human", "--adjudicated-by", judge}, ""); code != 0 {
		t.Fatalf("gate create with a closed judge: exit=%d stderr=%q", code, errS)
	}
}

func TestGateCreateKindHumanWithoutResolverSucceeds(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	_, errS, code := runCmd(t, withAdjudication(t, []string{"gate", "create", "--blocks", target, "--subject", "承認待ち", "--kind", "human"}), "")
	if code != 0 {
		t.Fatalf("gate create: exit code = %d, stderr = %q", code, errS)
	}
	if errS != "" {
		t.Errorf("stderr = %q, want empty", errS)
	}
}

func TestGateResolveRequiresReason(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "x")

	if _, errS, code := runCmd(t, []string{"gate", "resolve", gateID}, ""); code == 0 {
		t.Fatalf("gate resolve without --reason succeeded, stderr = %q", errS)
	}
}

func TestGateResolveOpensTargetAndReportsNewlyReady(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "x")

	out, errS, code := runCmd(t, []string{"gate", "resolve", gateID, "--reason", "external event fired"}, "")
	if code != 0 {
		t.Fatalf("gate resolve: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "Newly ready") {
		t.Errorf("gate resolve output missing Newly ready section:\n%s", out)
	}
	if !strings.Contains(out, "target") {
		t.Errorf("gate resolve output missing the newly-ready target:\n%s", out)
	}

	readyOut, _, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d", code)
	}
	if !strings.Contains(readyOut, "target") {
		t.Errorf("ready output = %q, want the target now Ready", readyOut)
	}
}

func TestGateCannotBeClaimed(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "x")

	if _, errS, code := runCmd(t, []string{"update", gateID, "--claim"}, ""); code == 0 {
		t.Fatalf("update --claim on a gate succeeded, stderr = %q", errS)
	}

	out, errS, code := runCmd(t, []string{"ready", "--claim"}, "")
	if code != 0 {
		t.Fatalf("ready --claim: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Claimed:") {
		t.Errorf("ready --claim claimed something, want nothing claimable:\n%s", out)
	}
}

func TestGateCloseAndUpdateStatusClosedRejected(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "x")

	if _, errS, code := runCmd(t, []string{"close", gateID}, ""); code == 0 {
		t.Fatalf("lm close on a gate succeeded, stderr = %q", errS)
	}
	if _, errS, code := runCmd(t, []string{"update", gateID, "--status", "closed"}, ""); code == 0 {
		t.Fatalf("lm update --status closed on a gate succeeded, stderr = %q", errS)
	}
}

func TestGateCancelAndReopenRejected(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "x")

	wantMsg := "a Gate can only be resolved with lm gate resolve"

	if _, errS, code := runCmd(t, []string{"update", gateID, "--status", "cancelled"}, ""); code != 1 {
		t.Fatalf("update --status cancelled on a gate: exit code = %d, stderr = %q, want 1", code, errS)
	} else if !strings.Contains(errS, wantMsg) {
		t.Errorf("update --status cancelled on a gate: stderr = %q, want it to contain %q", errS, wantMsg)
	}

	if _, errS, code := runCmd(t, []string{"update", gateID, "--reopen"}, ""); code != 1 {
		t.Fatalf("update --reopen on a gate: exit code = %d, stderr = %q, want 1", code, errS)
	} else if !strings.Contains(errS, wantMsg) {
		t.Errorf("update --reopen on a gate: stderr = %q, want it to contain %q", errS, wantMsg)
	}
}

func TestGateRejectRequiresReason(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "x")

	if _, errS, code := runCmd(t, []string{"gate", "reject", gateID}, ""); code == 0 {
		t.Fatalf("gate reject without --reason succeeded, stderr = %q", errS)
	}
}

func TestGateRejectNonGateRejected(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")

	if _, errS, code := runCmd(t, []string{"gate", "reject", target, "--reason", "no"}, ""); code == 0 {
		t.Fatalf("gate reject on a non-gate Bead succeeded, stderr = %q", errS)
	}
}

func TestGateRejectCancelsBlockedBeadsAndTheGate(t *testing.T) {
	initBeadsDir(t)
	target1 := mustCreateBead(t, "--title", "target1")
	target2 := mustCreateBead(t, "--title", "target2")
	gateID := mustGateCreateCmd(t, []string{target1, target2}, "x")

	out, errS, code := runCmd(t, []string{"gate", "reject", gateID, "--reason", "won't do"}, "")
	if code != 0 {
		t.Fatalf("gate reject: exit code = %d, stderr = %q, stdout = %q", code, errS, out)
	}
	if !strings.Contains(out, "- Rejected: "+gateID) {
		t.Errorf("gate reject output = %q, want a Rejected line for %q", out, gateID)
	}
	if !strings.Contains(out, "## Cancelled") {
		t.Errorf("gate reject output missing ## Cancelled section:\n%s", out)
	}
	if !strings.Contains(out, "target1") || !strings.Contains(out, "target2") {
		t.Errorf("gate reject output missing cancelled targets:\n%s", out)
	}
	if !strings.Contains(out, "## Newly ready") {
		t.Errorf("gate reject output missing ## Newly ready section:\n%s", out)
	}

	showOut, _, code := runCmd(t, []string{"show", target1}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(showOut, "status: \"cancelled\"") {
		t.Errorf("target1 show output = %q, want status cancelled", showOut)
	}

	gateShowOut, _, code := runCmd(t, []string{"show", gateID}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(gateShowOut, "status: \"cancelled\"") {
		t.Errorf("gate show output = %q, want status cancelled", gateShowOut)
	}
}

func TestGateRejectReleasesInProgressClaimBeforeCancelling(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "x")

	if out, _, code := runCmd(t, []string{"update", target, "--claim", "--actor", "alice"}, ""); code != 1 || !strings.Contains(out, "blocked by "+gateID) {
		t.Fatalf("claim past the open gate: code = %d, stdout = %q, want exit 1 and blocked by %s", code, out, gateID)
	}
	if _, errS, code := runCmd(t, []string{"update", target, "--claim", "--force", "--reason", "setup", "--actor", "alice"}, ""); code != 0 {
		t.Fatalf("claim target past the gate with --force failed to set up the test: %v", errS)
	}

	out, errS, code := runCmd(t, []string{"gate", "reject", gateID, "--reason", "won't do"}, "")
	if code != 0 {
		t.Fatalf("gate reject: exit code = %d, stderr = %q, stdout = %q", code, errS, out)
	}

	showOut, _, code := runCmd(t, []string{"show", target}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(showOut, "status: \"cancelled\"") {
		t.Errorf("target show output = %q, want status cancelled", showOut)
	}
	if strings.Contains(showOut, "claimed_by: \"alice\"") {
		t.Errorf("target show output = %q, want claim released (no claimed_by alice)", showOut)
	}
}

func TestGateRejectReportsNewlyReady(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "downstream")
	target := mustCreateBead(t, "--title", "target", "--parent", parent)
	gateID := mustGateCreateCmd(t, []string{target}, "x")

	out, errS, code := runCmd(t, []string{"gate", "reject", gateID, "--reason", "won't do"}, "")
	if code != 0 {
		t.Fatalf("gate reject: exit code = %d, stderr = %q, stdout = %q", code, errS, out)
	}
	if !strings.Contains(out, "downstream") {
		t.Errorf("gate reject output = %q, want %q listed under Newly ready", out, "downstream")
	}

	readyOut, _, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d", code)
	}
	if !strings.Contains(readyOut, "downstream") {
		t.Errorf("ready output = %q, want downstream now Ready", readyOut)
	}
}

func TestGateRejectRefusesUnfinishedDownstream(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	down := mustCreateBead(t, "--title", "downstream", "--blocked-by", target)
	gateID := mustGateCreateCmd(t, []string{target}, "x")

	out, errS, code := runCmd(t, []string{"gate", "reject", gateID, "--reason", "won't do"}, "")
	if code != 1 || !strings.Contains(out, "unfinished dependents: "+mustDisplayAlias(t, down)) || !strings.Contains(errS, mustDisplayAlias(t, down)) {
		t.Fatalf("gate reject = (%q, %q, %d), want refusal naming the downstream on stdout and stderr, exit 1", out, errS, code)
	}
	mustShowField(t, target, "status: \"open\"\n")
	mustShowField(t, gateID, "status: \"open\"\n")
}

func TestGateListFixedLine(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "wait for it")
	targetAlias := mustDisplayAlias(t, target)

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	wantPrefix := "- " + gateID + " [gate/open/P2] wait for it  → blocks: " + targetAlias
	if !strings.Contains(out, wantPrefix) {
		t.Errorf("lm gate = %q, want a line starting with %q", out, wantPrefix)
	}

	if _, errS, code := runCmd(t, []string{"gate", "resolve", gateID, "--reason", "done"}, ""); code != 0 {
		t.Fatalf("gate resolve: exit code = %d, stderr = %q", code, errS)
	}
	out, errS, code = runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate after resolve: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, gateID) {
		t.Errorf("lm gate after resolve = %q, must not list the resolved gate", out)
	}
}

func TestGateListSubjectFollowsTitle(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "push to old/loom")
	if _, errS, code := runCmd(t, []string{"update", gateID, "--title", "gate: push to new/loom", "--reason", "rename"}, ""); code != 0 {
		t.Fatalf("update title: exit code = %d, stderr = %q", code, errS)
	}
	mustShowField(t, gateID, "title: \"gate: push to new/loom\"\n")

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	if want := "- " + gateID + " [gate/open/P2] push to new/loom  → blocks: "; !strings.Contains(out, want) {
		t.Errorf("lm gate = %q, want a line containing %q", out, want)
	}
	if strings.Contains(out, "old/loom") {
		t.Errorf("lm gate = %q, must not show the description's first line", out)
	}
}

func TestGateBareThreeSections(t *testing.T) {
	initBeadsDir(t)
	humanTargetID := mustCreateBead(t, "--title", "human-target")
	gHumanID := mustCanonicalID(t, mustGateCreateCmd(t, []string{humanTargetID}, "デプロイ確認", "--kind", "human", "--check", "本番 URL が 200 を返すこと"))

	parentID := mustCreateBead(t, "--title", "external-parent")
	child1ID := mustCreateBead(t, "--title", "child1", "--parent", parentID)
	_ = mustCreateBead(t, "--title", "child2", "--parent", parentID)
	if _, errS, code := runCmd(t, []string{"close", child1ID}, ""); code != 0 {
		t.Fatalf("close child1: exit code = %d, stderr = %q", code, errS)
	}
	gWithChildrenID := mustCanonicalID(t, mustGateCreateCmd(t, []string{parentID}, "子 Bead の完了待ち: 誰かが lm gate resolve する", "--kind", "external"))

	externalTargetID := mustCreateBead(t, "--title", "external-target")
	gExternalID := mustCanonicalID(t, mustGateCreateCmd(t, []string{externalTargetID}, "webhook fired", "--kind", "external"))

	unknownTargetID := mustCreateBead(t, "--title", "unknown-target")
	gUnknownID := mustCanonicalID(t, mustGateCreateCmd(t, []string{unknownTargetID}, "wait z", "--kind", "human"))
	if _, errS, code := runCmd(t, []string{"update", gUnknownID, "--remove-label", "kind:human"}, ""); code != 0 {
		t.Fatalf("update --remove-label kind:human: exit code = %d, stderr = %q", code, errS)
	}

	gHuman := mustDisplayAlias(t, gHumanID)
	gWithChildren := mustDisplayAlias(t, gWithChildrenID)
	gExternal := mustDisplayAlias(t, gExternalID)
	gUnknown := mustDisplayAlias(t, gUnknownID)
	parent := mustDisplayAlias(t, parentID)
	externalTarget := mustDisplayAlias(t, externalTargetID)

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	humanHeading := strings.Index(out, "## Needs you")
	autoHeading := strings.Index(out, "## Opens automatically")
	unknownHeading := strings.Index(out, "## Unknown kind (fix labels)")
	if humanHeading == -1 || autoHeading == -1 || unknownHeading == -1 {
		t.Fatalf("lm gate output missing a section heading:\n%s", out)
	}
	if humanHeading >= autoHeading || autoHeading >= unknownHeading {
		t.Errorf("lm gate section order wrong (human=%d, auto=%d, unknown=%d):\n%s", humanHeading, autoHeading, unknownHeading, out)
	}

	wantHumanAux := "  - When resolved: 本番 URL が 200 を返すこと\n" +
		`  - Run: lm gate resolve ` + gHuman + ` --reason "go"`
	if !strings.Contains(out, wantHumanAux) {
		t.Errorf("lm gate output = %q, want human aux lines %q", out, wantHumanAux)
	}

	wantWithChildrenWait := "  - Waiting for: " + parent + " open（子 1/2 終端・残り "
	if !strings.Contains(out, wantWithChildrenWait) {
		t.Errorf("lm gate output = %q, want an auto-open wait line with a children summary for %s starting %q", out, parent, wantWithChildrenWait)
	}

	wantExternalWait := "  - Waiting for: " + externalTarget + " open）"
	if strings.Contains(out, wantExternalWait) {
		t.Errorf("lm gate output = %q, must not print a child summary for %s (no children)", out, externalTarget)
	}
	if !strings.Contains(out, "  - Waiting for: "+externalTarget+" open\n") {
		t.Errorf("lm gate output = %q, want a plain wait line for %s (no children)", out, externalTarget)
	}

	wantUnknownAux := "  - Fix: lm update " + gUnknown + " --add-label kind:adjudicate|kind:external"
	if !strings.Contains(out, wantUnknownAux) {
		t.Errorf("lm gate output = %q, want unknown aux line %q", out, wantUnknownAux)
	}
	for _, id := range []string{gHuman, gWithChildren, gExternal, gUnknown} {
		if !strings.Contains(out, id) {
			t.Errorf("lm gate output = %q, want gate %s listed", out, id)
		}
	}

	allOut, errS, code := runCmd(t, []string{"gate", "--all"}, "")
	if code != 0 {
		t.Fatalf("lm gate --all: exit code = %d, stderr = %q", code, errS)
	}
	if allOut != out {
		t.Errorf("lm gate --all output differs from lm gate (--all is deprecated and must have no effect):\ngate:      %q\ngate --all: %q", out, allOut)
	}
}

func TestGateBareEmptySectionsOmitted(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	if strings.TrimSpace(out) != "(none)" {
		t.Errorf("lm gate with no open Gates = %q, want (none)", out)
	}

	target := mustCreateBead(t, "--title", "target")
	gHuman := mustGateCreateCmd(t, []string{target}, "wait a", "--kind", "human")

	out, errS, code = runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "## Needs you") {
		t.Errorf("lm gate output = %q, want the human section heading", out)
	}
	for _, heading := range []string{"## Opens automatically", "## Unknown kind (fix labels)"} {
		if strings.Contains(out, heading) {
			t.Errorf("lm gate output = %q, must not print the empty %q heading", out, heading)
		}
	}
	if !strings.Contains(out, gHuman) {
		t.Errorf("lm gate output = %q, want gate %s listed", out, gHuman)
	}
}

func TestGateBareHumanWithUnknownWaitLabelIsUnknownKind(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gID := mustCanonicalID(t, mustGateCreateCmd(t, []string{target}, "runner 復帰", "--kind", "human"))
	for _, l := range []string{"wait:runners-online:o", "wait:any", "wait:file:/x", "wait:zz:1"} {
		if _, errS, code := runCmd(t, []string{"update", gID, "--add-label", l}, ""); code != 0 {
			t.Fatalf("update --add-label %s: exit code = %d, stderr = %q", l, code, errS)
		}
	}
	g := mustDisplayAlias(t, gID)

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	for _, heading := range []string{"## Needs you", "## Blocked by other work", "## Opens automatically"} {
		if strings.Contains(out, heading) {
			t.Errorf("lm gate output = %q, must not print %q for a human Gate with an unknown wait: label", out, heading)
		}
	}
	unknown := strings.Index(out, "## Unknown kind (fix labels)")
	if unknown == -1 || !strings.Contains(out[unknown:], g) {
		t.Fatalf("lm gate output = %q, want %s under ## Unknown kind (fix labels)", out, g)
	}
	wantFix := "  - Fix: unknown wait label: wait:runners-online:o, wait:zz:1\n"
	if !strings.Contains(out, wantFix) {
		t.Errorf("lm gate output = %q, want %q", out, wantFix)
	}

	aheadOut, errS, code := runCmd(t, []string{"ahead"}, "")
	if code != 0 {
		t.Fatalf("lm ahead: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(aheadOut, "- Unknown kind (fix labels): 1 gates, ") || strings.Contains(aheadOut, "### Needs you") {
		t.Errorf("lm ahead output = %q, want the Gate folded into Unknown kind (fix labels) and no Needs you", aheadOut)
	}
}

func TestGateBarePrereqSectionThenPromotedToHuman(t *testing.T) {
	initBeadsDir(t)
	prereqID := mustCreateBead(t, "--title", "prereq")
	targetID := mustCreateBead(t, "--title", "target", "--blocked-by", prereqID)
	gID := mustCanonicalID(t, mustGateCreateCmd(t, []string{targetID}, "出荷判定", "--kind", "human", "--check", "タグを push する"))
	gAlias := mustDisplayAlias(t, gID)
	prereqAlias := mustDisplayAlias(t, prereqID)

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "## Needs you") {
		t.Errorf("lm gate output = %q, must not show the human section while a prerequisite is still open", out)
	}
	if !strings.Contains(out, "## Blocked by other work") {
		t.Errorf("lm gate output = %q, want a Blocked by other work section", out)
	}
	wantPrereqAux := "  - When resolved: タグを push する\n" +
		"  - Blocked by: " + prereqAlias
	if !strings.Contains(out, wantPrereqAux) {
		t.Errorf("lm gate output = %q, want prereq aux lines %q", out, wantPrereqAux)
	}
	if strings.Contains(out, "Run: lm gate resolve "+gAlias) {
		t.Errorf("lm gate output = %q, must not offer `lm gate resolve` while a prerequisite is still open", out)
	}

	if _, errS, code := runCmd(t, []string{"close", prereqID}, ""); code != 0 {
		t.Fatalf("close prereq: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code = runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "## Blocked by other work") {
		t.Errorf("lm gate output = %q, must not show Blocked by other work once the prerequisite is closed", out)
	}
	if !strings.Contains(out, "## Needs you") {
		t.Errorf("lm gate output = %q, want the Gate promoted to the human section", out)
	}
	if !strings.Contains(out, `Run: lm gate resolve `+gAlias+` --reason "go"`) {
		t.Errorf("lm gate output = %q, want the resolve command now that the prerequisite is closed", out)
	}
}

func TestGateListShowsBlocksLessGate(t *testing.T) {
	initBeadsDir(t)
	gateCanonical := mustCreateBead(t, "--title", "bare gate", "--type", "gate", "--label", "kind:human")
	gateID := mustDisplayAlias(t, gateCanonical)

	readyOut, _, code := runCmd(t, []string{"ready"}, "")
	if code != 0 {
		t.Fatalf("ready: exit code = %d", code)
	}
	if strings.Contains(readyOut, gateID) {
		t.Errorf("ready output = %q, must not list the blocks-less gate", readyOut)
	}

	blockedOut, _, code := runCmd(t, []string{"blocked"}, "")
	if code != 0 {
		t.Fatalf("blocked: exit code = %d", code)
	}
	if strings.Contains(blockedOut, gateID) {
		t.Errorf("blocked output = %q, must not list the blocks-less gate", blockedOut)
	}

	listOut, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(listOut, gateID) {
		t.Errorf("lm gate output = %q, want the blocks-less gate listed", listOut)
	}
}

func TestGateAndBlockedShowArrowAndAlias(t *testing.T) {
	initBeadsDir(t)
	parent := mustCreateBead(t, "--title", "Epic", "--type", "task")
	child := mustCreateBead(t, "--title", "Child", "--parent", parent)

	createOut, errS, code := runCmd(t, withAdjudication(t, []string{"gate", "create", "--blocks", child, "--subject", "wait for it", "--kind", "human"}), "")
	if code != 0 {
		t.Fatalf("gate create: exit code = %d, stderr = %q", code, errS)
	}
	wantSuffix := "  → blocks: " + mustDisplayAlias(t, parent) + ".1"
	if !strings.HasSuffix(strings.TrimRight(createOut, "\n"), wantSuffix) {
		t.Errorf("gate create output = %q, want a line ending with %q", createOut, wantSuffix)
	}

	listOut, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	found := false
	for line := range strings.SplitSeq(listOut, "\n") {
		if strings.HasSuffix(line, wantSuffix) || strings.Contains(line, wantSuffix+"  via ") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("lm gate output = %q, want a line ending with %q", listOut, wantSuffix)
	}

	x := mustCreateBead(t, "--title", "X", "--blocked-by", child)
	blockedOut, errS, code := runCmd(t, []string{"blocked"}, "")
	if code != 0 {
		t.Fatalf("blocked: exit code = %d, stderr = %q", code, errS)
	}
	xAlias := mustDisplayAlias(t, x)
	wantBlockedSuffix := "  ← blocked by: " + mustDisplayAlias(t, parent) + ".1"
	xFound := false
	for line := range strings.SplitSeq(blockedOut, "\n") {
		if strings.HasPrefix(line, "- "+xAlias+" ") && strings.HasSuffix(line, wantBlockedSuffix) {
			xFound = true
			break
		}
	}
	if !xFound {
		t.Errorf("blocked output = %q, want a line for %s ending with %q", blockedOut, xAlias, wantBlockedSuffix)
	}
}

func TestGateSweepResolvesGateWithAllBlockedBeadsAlreadyTerminal(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	if _, errS, code := runCmd(t, []string{"close", target}, ""); code != 0 {
		t.Fatalf("close target: exit code = %d, stderr = %q", code, errS)
	}
	gateID := mustGateCreateCmd(t, []string{target}, "CI green")

	out, errS, code := runCmd(t, []string{"gate", "sweep"}, "")
	if code != 0 {
		t.Fatalf("gate sweep: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Auto-resolved: "+gateID) {
		t.Errorf("gate sweep output = %q, want an Auto-resolved line for %q", out, gateID)
	}

	showOut, _, code := runCmd(t, []string{"show", gateID}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(showOut, `status: "closed"`) {
		t.Errorf("gate show output = %q, want status closed", showOut)
	}
}

func TestGateSweepNoOpenGatesExitsZeroWithNoOutput(t *testing.T) {
	initBeadsDir(t)

	out, errS, code := runCmd(t, []string{"gate", "sweep"}, "")
	if code != 0 {
		t.Fatalf("gate sweep: exit code = %d, stderr = %q", code, errS)
	}
	if out != "" {
		t.Errorf("gate sweep output = %q, want empty", out)
	}
}

func TestGateSweepRejectsPositionalArgs(t *testing.T) {
	initBeadsDir(t)

	_, errS, code := runCmd(t, []string{"gate", "sweep", "extra"}, "")
	if code == 0 {
		t.Fatalf("gate sweep extra: exit code = 0, want non-zero")
	}
	if !strings.Contains(errS, "takes no positional arguments") {
		t.Errorf("gate sweep extra stderr = %q, want the no-positional-arguments message", errS)
	}
}

func TestGateSweepWriteTransactionFailureReported(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	_ = mustGateCreateCmd(t, []string{target}, "CI green")
	dropTable(t, "dependencies")

	_, errS, code := runCmd(t, []string{"gate", "sweep"}, "")
	if code == 0 {
		t.Fatalf("gate sweep with dependencies table dropped: exit code = 0, want non-zero")
	}
	if errS == "" {
		t.Errorf("gate sweep with dependencies table dropped: stderr is empty, want an error message")
	}
}

func TestGateSweepReportsOpenDBError(t *testing.T) {
	t.Chdir(t.TempDir())
	setIsolatedXDGHome(t)

	out, errS, code := runCmd(t, []string{"gate", "sweep"}, "")
	if code == 0 {
		t.Fatalf("gate sweep without a .loom directory: exit code = 0, want non-zero; stdout=%q", out)
	}
	if !strings.Contains(errS, "Error:") {
		t.Errorf("gate sweep without a .loom directory: stderr = %q, want an Error: line", errS)
	}
}

func TestGateSweepReportsShortLenError(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	_ = mustGateCreateCmd(t, []string{target}, "CI green")
	dropTable(t, "beads")

	_, errS, code := runCmd(t, []string{"gate", "sweep"}, "")
	if code == 0 {
		t.Fatalf("gate sweep with beads table dropped: exit code = 0, want non-zero")
	}
	if errS == "" {
		t.Errorf("gate sweep with beads table dropped: stderr is empty, want an error message")
	}
}

func TestGateSweepReportsGateSweepError(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "CI green")
	if _, errS, code := runCmd(t, []string{"update", target, "--status", "cancelled"}, ""); code != 0 {
		t.Fatalf("update --status cancelled: exit code = %d, stderr = %q", code, errS)
	}
	corruptLabels(t, gateID)

	_, errS, code := runCmd(t, []string{"gate", "sweep"}, "")
	if code == 0 {
		t.Fatalf("gate sweep with the gate's labels malformed: exit code = 0, want non-zero")
	}
	if errS == "" {
		t.Errorf("gate sweep with the gate's labels malformed: stderr is empty, want an error message")
	}
}

func TestGateExportImportRoundTrip(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "wait for it")
	gateCanonical := canonicalID(t, gateID)
	targetCanonical := canonicalID(t, target)

	jsonl := exportString(t)

	initBeadsDir(t)
	mustImport(t, jsonl)

	showOut, errS, code := runCmd(t, []string{"show", gateCanonical}, "")
	if code != 0 {
		t.Fatalf("show gate after import: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(showOut, `type: "gate"`) {
		t.Errorf(`show gate after import = %q, want type: "gate"`, showOut)
	}

	blockedOut, errS, code := runCmd(t, []string{"blocked"}, "")
	if code != 0 {
		t.Fatalf("blocked after import: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(blockedOut, "← blocked by: gate:") {
		t.Errorf("blocked output after import = %q, want the target listed as blocked by the gate", blockedOut)
	}
	if !strings.Contains(blockedOut, targetCanonical[:6]) && !strings.Contains(blockedOut, target) {
		t.Logf("blocked output after import:\n%s", blockedOut)
	}
}

func TestGateBareAdjudicateSection(t *testing.T) {
	initBeadsDir(t)
	humanTarget := mustCreateBead(t, "--title", "human target")
	mustGateCreateCmd(t, []string{humanTarget}, "人の判断", "--kind", "human")
	adjTarget := mustCreateBead(t, "--title", "adjudicate target")
	gAdjID := mustCanonicalID(t, mustGateCreateCmd(t, []string{adjTarget}, "判定の件", "--kind", "adjudicate", "--check", "判定者が開く"))
	prereqID := mustCreateBead(t, "--title", "prereq")
	adjTarget2 := mustCreateBead(t, "--title", "adjudicate target 2", "--blocked-by", prereqID)
	gAdj2ID := mustCanonicalID(t, mustGateCreateCmd(t, []string{adjTarget2}, "前提つき判定", "--kind", "adjudicate"))
	extTarget := mustCreateBead(t, "--title", "external target")
	mustGateCreateCmd(t, []string{extTarget}, "外部待ち", "--kind", "external", "--resolver", "watcher")
	gAdj, gAdj2 := mustDisplayAlias(t, gAdjID), mustDisplayAlias(t, gAdj2ID)

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	humanIdx := strings.Index(out, "## Needs you")
	adjIdx := strings.Index(out, "## Waiting on judge")
	autoIdx := strings.Index(out, "## Opens automatically")
	if humanIdx == -1 || adjIdx == -1 || autoIdx == -1 || humanIdx >= adjIdx || adjIdx >= autoIdx {
		t.Fatalf("lm gate section order wrong (human=%d, adjudicate=%d, auto=%d):\n%s", humanIdx, adjIdx, autoIdx, out)
	}
	if strings.Contains(out, "## Blocked by other work") {
		t.Errorf("lm gate output = %q, an adjudicate Gate with an open prerequisite must not go to Blocked by other work", out)
	}
	human := out[humanIdx:adjIdx]
	adj := out[adjIdx:autoIdx]
	if strings.Contains(human, gAdj) || strings.Contains(human, gAdj2) {
		t.Errorf("Needs you = %q, must not list adjudicate Gates", human)
	}
	for _, want := range []string{
		gAdj + " [gate/open/P2] 判定の件",
		"  - When resolved: 判定者が開く\n",
		`  - Run: lm gate resolve ` + gAdj + ` --reason "go"`,
		gAdj2 + " [gate/open/P2] 前提つき判定",
	} {
		if !strings.Contains(adj, want) {
			t.Errorf("Waiting on judge = %q, want %q", adj, want)
		}
	}
}

func TestGateBareWaitLabelSection(t *testing.T) {
	initBeadsDir(t)
	confirmTarget := mustCreateBead(t, "--title", "confirm target")
	gConfirmID := mustCanonicalID(t, mustGateCreateCmd(t, []string{confirmTarget}, "期日待ちの確認", "--kind", "confirm"))
	adjTarget := mustCreateBead(t, "--title", "adjudicate target")
	gAdjID := mustCanonicalID(t, mustGateCreateCmd(t, []string{adjTarget}, "文書 PR の裁定", "--kind", "adjudicate"))
	mustCreateBead(t, "--title", "doc PR owner", "--external-ref", "https://github.com/o/r/pull/1")
	gConfirm, gAdj := mustDisplayAlias(t, gConfirmID), mustDisplayAlias(t, gAdjID)
	for _, u := range [][]string{
		{"update", gConfirmID, "--add-label", "wait:date:2999-01-01"},
		{"update", gAdjID, "--add-label", "wait:pr-merged:https://github.com/o/r/pull/1"},
	} {
		if out, errS, code := runCmd(t, u, ""); code != 0 {
			t.Fatalf("%v: exit=%d stdout=%q stderr=%q", u, code, out, errS)
		}
	}

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	adjIdx := strings.Index(out, "## Waiting on judge")
	autoIdx := strings.Index(out, "## Opens automatically")
	if adjIdx == -1 || autoIdx == -1 || adjIdx >= autoIdx {
		t.Fatalf("lm gate section order wrong (adjudicate=%d, auto=%d):\n%s", adjIdx, autoIdx, out)
	}
	if strings.Contains(out, "## Needs you") {
		t.Errorf("lm gate output = %q, a confirm Gate with wait:date: must not go to Needs you", out)
	}
	adj, auto := out[adjIdx:autoIdx], out[autoIdx:]
	if !strings.Contains(adj, gAdj+" [gate/open/P2] 文書 PR の裁定") {
		t.Errorf("Waiting on judge = %q, want the adjudicate Gate %s", adj, gAdj)
	}
	if strings.Contains(auto, gAdj) || strings.Contains(auto, "wait:pr-merged:") {
		t.Errorf("Opens automatically = %q, must not list the adjudicate Gate or its wait:pr-merged:", auto)
	}
	for _, want := range []string{
		gConfirm + " [gate/open/P2] 期日待ちの確認",
		"  - Waiting for: wait:date:2999-01-01\n",
	} {
		if !strings.Contains(auto, want) {
			t.Errorf("Opens automatically = %q, want %q", auto, want)
		}
	}
}

func TestGateHumanPRMergedStaysHuman(t *testing.T) {
	initBeadsDir(t)
	const pr = "https://github.com/o/r/pull/1"
	mustCreateBead(t, "--title", "doc PR owner", "--external-ref", pr)
	kinds := []string{"human", "confirm"}
	gates := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		target := mustCreateBead(t, "--title", kind+" target")
		id := mustCanonicalID(t, mustGateCreateCmd(t, []string{target}, kind+" の承認", "--kind", kind))
		if out, errS, code := runCmd(t, []string{"update", id, "--add-label", "wait:pr-merged:" + pr}, ""); code != 0 {
			t.Fatalf("update %s: exit=%d stdout=%q stderr=%q", id, code, out, errS)
		}
		gates = append(gates, mustDisplayAlias(t, id))
	}

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "## Opens automatically") {
		t.Errorf("lm gate output = %q, a human Gate's wait:pr-merged: must not go to Opens automatically", out)
	}
	humanIdx := strings.Index(out, "## Needs you")
	if humanIdx == -1 {
		t.Fatalf("lm gate output = %q, want ## Needs you", out)
	}
	for _, g := range gates {
		if !strings.Contains(out[humanIdx:], g+" [gate/open/P2]") {
			t.Errorf("Needs you = %q, want %s", out[humanIdx:], g)
		}
	}
}

func TestGateHumanWaitFileGoesToNeedsYou(t *testing.T) {
	initBeadsDir(t)
	humanTarget := mustCreateBead(t, "--title", "human target")
	gHumanID := mustCanonicalID(t, mustGateCreateCmd(t, []string{humanTarget}, "初回 push を端末で実行する", "--kind", "human"))
	adjTarget := mustCreateBead(t, "--title", "adjudicate target")
	gAdjID := mustCanonicalID(t, mustGateCreateCmd(t, []string{adjTarget}, "成果物の到着を裁定する", "--kind", "adjudicate"))
	for _, id := range []string{gHumanID, gAdjID} {
		if out, errS, code := runCmd(t, []string{"update", id, "--add-label", "wait:file:/tmp/signal"}, ""); code != 0 {
			t.Fatalf("update %s: exit=%d stdout=%q stderr=%q", id, code, out, errS)
		}
	}
	gHuman, gAdj := mustDisplayAlias(t, gHumanID), mustDisplayAlias(t, gAdjID)

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	humanIdx := strings.Index(out, "## Needs you")
	autoIdx := strings.Index(out, "## Opens automatically")
	if humanIdx == -1 || autoIdx == -1 || humanIdx >= autoIdx {
		t.Fatalf("lm gate section order wrong (human=%d, auto=%d):\n%s", humanIdx, autoIdx, out)
	}
	if !strings.Contains(out[humanIdx:autoIdx], gHuman+" [gate/open/P2] 初回 push を端末で実行する") {
		t.Errorf("Needs you = %q, want the human Gate %s", out[humanIdx:autoIdx], gHuman)
	}
	auto := out[autoIdx:]
	if strings.Contains(auto, gHuman) {
		t.Errorf("Opens automatically = %q, must not list the human Gate %s", auto, gHuman)
	}
	if !strings.Contains(auto, gAdj+" [gate/open/P2] 成果物の到着を裁定する") {
		t.Errorf("Opens automatically = %q, want the adjudicate Gate %s", auto, gAdj)
	}
}

func TestGateHumanWaitFileWithPrereqGoesToBlockedByOtherWork(t *testing.T) {
	initBeadsDir(t)
	prereqID := mustCreateBead(t, "--title", "prereq")
	targetID := mustCreateBead(t, "--title", "target", "--blocked-by", prereqID)
	gID := mustCanonicalID(t, mustGateCreateCmd(t, []string{targetID}, "初回 push を端末で実行する", "--kind", "human"))
	if out, errS, code := runCmd(t, []string{"update", gID, "--add-label", "wait:file:/tmp/signal"}, ""); code != 0 {
		t.Fatalf("update %s: exit=%d stdout=%q stderr=%q", gID, code, out, errS)
	}
	g := mustDisplayAlias(t, gID)

	out, errS, code := runCmd(t, []string{"gate"}, "")
	if code != 0 {
		t.Fatalf("lm gate: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "## Opens automatically") {
		t.Errorf("lm gate output = %q, a human Gate's wait:file: must not go to Opens automatically", out)
	}
	if strings.Contains(out, "## Needs you") {
		t.Errorf("lm gate output = %q, must not show Needs you while a prerequisite is still open", out)
	}
	prereqIdx := strings.Index(out, "## Blocked by other work")
	if prereqIdx == -1 || !strings.Contains(out[prereqIdx:], g+" [gate/open/P2]") {
		t.Errorf("lm gate output = %q, want %s under ## Blocked by other work", out, g)
	}
}

func TestGateCreateMaterialCLI(t *testing.T) {
	initBeadsDir(t)
	const pr = "https://github.com/o/r/pull/7"
	owner := mustCreateBead(t, "--title", "owner", "--external-ref", pr)
	impl := mustCreateBead(t, "--title", "impl")

	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"owner blocked", []string{"gate", "create", "--blocks", owner, "--subject", "裁定", "--kind", "adjudicate", "--material", pr}, "adding this wait link would create a cycle: "},
		{"ownerless", []string{"gate", "create", "--blocks", impl, "--subject", "裁定", "--kind", "adjudicate", "--material", "https://github.com/o/r/pull/99"}, `Rejected: new (wait target "https://github.com/o/r/pull/99" has no owner;`},
		{"no material", withAdjudication(t, []string{"gate", "create", "--blocks", impl, "--subject", "PR の merge", "--kind", "human"}), "Rejected: new (a human/confirm Gate that mentions a PR needs --material <ref>)"},
	} {
		out, errS, code := runCmd(t, c.args, "")
		if code != 1 || !strings.Contains(errS, c.want) || out != "" {
			t.Errorf("%s: exit=%d stdout=%q stderr=%q, want exit 1 and %q", c.name, code, out, errS, c.want)
		}
	}
	if out, _, _ := runCmd(t, []string{"gate"}, ""); strings.Contains(out, "裁定") || strings.Contains(out, "PR の merge") {
		t.Fatalf("a rejected gate create left a Gate behind: %q", out)
	}

	g := mustGateCreateCmd(t, []string{impl}, "PR の merge", "--material", pr)
	show, _, _ := runCmd(t, []string{"show", g}, "")
	if !strings.Contains(show, `"material:`+pr+`"`) {
		t.Fatalf("show = %q, want the material: label", show)
	}
	_, errS, code := runCmd(t, []string{"update", impl, "--external-ref", "handoff/x.md"}, "")
	if code != 0 {
		t.Fatalf("unrelated external ref: exit=%d stderr=%q", code, errS)
	}
	_, errS, code = runCmd(t, []string{"update", owner, "--status", "cancelled", "--reason", "x"}, "")
	if code != 0 {
		t.Fatalf("cancel owner: exit=%d stderr=%q", code, errS)
	}
	out, _, code := runCmd(t, []string{"update", impl, "--external-ref", pr}, "")
	if code != 1 || !strings.Contains(out, "adding this wait link would create a cycle: ") {
		t.Fatalf("impl taking the PR it waits on: exit=%d stdout=%q, want the wait cycle", code, out)
	}
}
