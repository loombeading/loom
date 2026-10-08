// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/storage"
)

type erroringReader struct{}

func (erroringReader) Read([]byte) (int, error) {
	return 0, errors.New("boom: stdin read failed")
}

func TestCloseSummaryDashStdinReadErrorRejected(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	var out, errBuf bytes.Buffer
	code := run([]string{"close", id, "--summary", "-"}, erroringReader{}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("close --summary - (stdin read error): exit code = %d, want 1; stdout=%q stderr=%q", code, out.String(), errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "Error:") {
		t.Errorf("stderr = %q, want an Error: line", errBuf.String())
	}

	showOut, _, showCode := runCmd(t, []string{"show", id}, "")
	if showCode != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, `status: "open"`) {
		t.Errorf("show output = %q, want the Bead left open when stdin cannot be read", showOut)
	}
}

func TestCloseSummarySetsSummaryAndAuditsIt(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, errS, code := runCmd(t, []string{"close", id, "--summary", "wrapped up", "--reason", "done"}, "")
	if code != 0 {
		t.Fatalf("close --summary: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Closed:") {
		t.Fatalf("close --summary output = %q, want a Closed line", out)
	}

	showOut, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(showOut, "wrapped up") {
		t.Errorf("show output = %q, want the summary recorded by close --summary", showOut)
	}
	if !strings.Contains(showOut, `status: "closed"`) {
		t.Errorf("show output = %q, want the Bead closed", showOut)
	}
	if !strings.Contains(showOut, "field summary = wrapped up") {
		t.Errorf("show output = %q, want an audit record for the summary change", showOut)
	}
}

func TestCloseSummaryDoesNotResetPriority(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t", "--priority", "3")

	if _, errS, code := runCmd(t, []string{"close", id, "--summary", "s"}, ""); code != 0 {
		t.Fatalf("close --summary: exit code = %d, stderr = %q", code, errS)
	}

	showOut, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, "priority: 3") {
		t.Errorf("show output = %q, want priority left at 3 after close --summary", showOut)
	}
	if strings.Contains(showOut, "field priority") {
		t.Errorf("show output = %q, want no priority audit record from close --summary", showOut)
	}
}

func TestCloseSummaryDashReadsStdin(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	_, errS, code := runCmd(t, []string{"close", id, "--summary", "-"}, "from stdin")
	if code != 0 {
		t.Fatalf("close --summary -: exit code = %d, stderr = %q", code, errS)
	}

	showOut, _, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, "from stdin") {
		t.Errorf("show output = %q, want the stdin-provided summary recorded", showOut)
	}
}

func TestCloseSummaryWithMultipleIDsRejectedAndNothingChanges(t *testing.T) {
	initBeadsDir(t)
	id1 := mustCreateBead(t, "--title", "t1")
	id2 := mustCreateBead(t, "--title", "t2")

	out, errS, code := runCmd(t, []string{"close", id1, id2, "--summary", "x"}, "")
	if code != 1 {
		t.Fatalf("close <id1> <id2> --summary x: exit code = %d, want 1; stdout=%q stderr=%q", code, out, errS)
	}
	if !strings.Contains(errS, "Error:") {
		t.Errorf("stderr = %q, want an Error: line", errS)
	}
	if strings.Contains(out, "- Closed:") {
		t.Errorf("stdout = %q, want no Closed line when --summary is rejected for multiple IDs", out)
	}

	for _, id := range []string{id1, id2} {
		showOut, _, code := runCmd(t, []string{"show", id}, "")
		if code != 0 {
			t.Fatal("show failed")
		}
		if !strings.Contains(showOut, `status: "open"`) {
			t.Errorf("show output for %s = %q, want it left open", id, showOut)
		}
	}
}

func TestCloseSummaryEmptyRejected(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	out, errS, code := runCmd(t, []string{"close", id, "--summary", ""}, "")
	if code != 1 {
		t.Fatalf("close --summary '': exit code = %d, want 1; stdout=%q stderr=%q", code, out, errS)
	}
	showOut, _, showCode := runCmd(t, []string{"show", id}, "")
	if showCode != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, `status: "open"`) {
		t.Errorf("show output = %q, want the Bead left open when --summary is empty", showOut)
	}
}

func TestCloseSummaryEmptyStdinRejected(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")

	_, _, code := runCmd(t, []string{"close", id, "--summary", "-"}, "")
	if code != 1 {
		t.Fatalf("close --summary - (empty stdin): exit code = %d, want 1", code)
	}
	showOut, _, showCode := runCmd(t, []string{"show", id}, "")
	if showCode != 0 {
		t.Fatal("show failed")
	}
	if !strings.Contains(showOut, `status: "open"`) {
		t.Errorf("show output = %q, want the Bead left open when --summary reads an empty stdin", showOut)
	}
}

func TestCloseAutoResolvesGateWhenSoleBlockedBeadCloses(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	gateID := mustGateCreateCmd(t, []string{target}, "CI green")

	out, errS, code := runCmd(t, []string{"close", target}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Auto-resolved: "+gateID) {
		t.Errorf("close output = %q, want an Auto-resolved line for %q", out, gateID)
	}

	showOut, _, code := runCmd(t, []string{"show", gateID}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(showOut, `status: "closed"`) {
		t.Errorf("gate show output = %q, want status closed", showOut)
	}
	if !strings.Contains(showOut, "自動: 塞いだ Bead が全て終端") {
		t.Errorf("gate show output = %q, want the auto-resolve reason in its history", showOut)
	}
}

func TestCloseDoesNotAutoResolveGateWithRemainingOpenBlocker(t *testing.T) {
	initBeadsDir(t)
	target1 := mustCreateBead(t, "--title", "target1")
	target2 := mustCreateBead(t, "--title", "target2")
	gateID := mustGateCreateCmd(t, []string{target1, target2}, "CI green")

	out, errS, code := runCmd(t, []string{"close", target1}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Auto-resolved") {
		t.Errorf("close output = %q, want no Auto-resolved line while target2 is still open", out)
	}

	showOut, _, code := runCmd(t, []string{"show", gateID}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d", code)
	}
	if !strings.Contains(showOut, `status: "open"`) {
		t.Errorf("gate show output = %q, want the gate left open", showOut)
	}
}

func TestCloseReportsErrorWhenAutoResolveGatesForBeadFails(t *testing.T) {
	initBeadsDir(t)
	target := mustCreateBead(t, "--title", "target")
	targetCanonical := mustCanonicalID(t, target)
	gateID := mustGateCreateCmd(t, []string{target}, "CI green")
	corruptLabels(t, gateID)

	out, _, code := runCmd(t, []string{"close", target}, "")
	if code == 0 {
		t.Fatalf("close with the blocking gate's labels malformed: exit code = 0, want non-zero; stdout=%q", out)
	}
	if !strings.Contains(out, "Rejected") {
		t.Errorf("close with the blocking gate's labels malformed: stdout = %q, want a Rejected line", out)
	}

	status := rawBeadStatus(t, targetCanonical)
	if status != "open" {
		t.Errorf("target status = %q, want it left open when close fails", status)
	}
}

func TestCloseOutputsOpenDerivedTargetsOneLine(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")
	found := mustCreateBead(t, "--title", "found while working")
	if _, errS, code := runCmd(t, []string{"dep", "add", found, origin, "--type", "discovered-from"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"close", origin}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "- Closed:") {
		t.Fatalf("close output = %q, want a Closed line (close is not blocked by an open derived target)", out)
	}
	want := "- Derived (open): " + mustDisplayAlias(t, found)
	if !strings.Contains(out, want) {
		t.Errorf("close output = %q, want line %q", out, want)
	}
}

func TestCloseOmitsDerivedLineWhenNoOpenDerivedTargets(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")

	out, errS, code := runCmd(t, []string{"close", origin}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Derived") {
		t.Errorf("close output = %q, want no Derived line for a Bead with no discovered-from targets", out)
	}
}

func TestCloseOmitsDerivedLineWhenDerivedTargetAlreadyClosed(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")
	found := mustCreateBead(t, "--title", "found while working")
	if _, errS, code := runCmd(t, []string{"dep", "add", found, origin, "--type", "discovered-from"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", found}, ""); code != 0 {
		t.Fatalf("close found: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"close", origin}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Derived") {
		t.Errorf("close output = %q, want no Derived line once the only derived target is already closed", out)
	}
}

func TestCloseOmitsDerivedLineForDanglingDiscoveredFromTarget(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")
	ghost := "zzzzzzzzzzzzzzzzzzzzzzzzzz"
	jsonl := `{"_type":"dependency","bead_id":"` + ghost + `","depends_on_id":"` + origin + `","type":"discovered-from","created_at":"2026-01-01T00:00:00.000Z","removed":false}`
	mustImport(t, jsonl)

	out, errS, code := runCmd(t, []string{"close", origin}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(out, "Derived") {
		t.Errorf("close output = %q, want no Derived line for a dangling discovered-from target", out)
	}
}

func TestCloseShowsParentAliasForDerivedTarget(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")
	epic := mustCreateBead(t, "--title", "epic", "--type", "task")
	found := mustCreateBead(t, "--title", "found while working", "--parent", epic)
	if _, errS, code := runCmd(t, []string{"dep", "add", found, origin, "--type", "discovered-from"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"close", origin}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	want := "- Derived (open): " + mustDisplayAlias(t, epic) + ".1"
	if !strings.Contains(out, want) {
		t.Errorf("close output = %q, want line %q", out, want)
	}
}

func TestDerivedOpenDisplayIDsReturnsErrorWhenSentLinksQueryFails(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")
	found := mustCreateBead(t, "--title", "found while working")
	if _, errS, code := runCmd(t, []string{"dep", "add", found, origin, "--type", "discovered-from"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}
	originCanonical := mustCanonicalID(t, origin)
	dropTable(t, "dependencies")

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := derivedOpenDisplayIDs(context.Background(), db, originCanonical, domain.ShortIDs{}); err == nil {
		t.Fatal("derivedOpenDisplayIDs with the dependencies table dropped: err = nil, want an error")
	}
}

func TestDerivedOpenDisplayIDsReturnsErrorWhenAliasResolverFails(t *testing.T) {
	initBeadsDir(t)
	origin := mustCreateBead(t, "--title", "origin task")
	found := mustCreateBead(t, "--title", "found while working")
	if _, errS, code := runCmd(t, []string{"dep", "add", found, origin, "--type", "discovered-from"}, ""); code != 0 {
		t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
	}
	originCanonical := mustCanonicalID(t, origin)
	dropTable(t, "beads")

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(beadsDBPath(t)))
	if err != nil {
		t.Fatalf("open loom.db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := derivedOpenDisplayIDs(context.Background(), db, originCanonical, domain.ShortIDs{}); err == nil {
		t.Fatal("derivedOpenDisplayIDs with the beads table dropped: err = nil, want an error")
	}
}

func TestCloseRejectsUnknownID(t *testing.T) {
	initBeadsDir(t)

	out, _, code := runCmd(t, []string{"close", "no-such-bead"}, "")
	if code != 1 {
		t.Fatalf("close unknown ID: exit code = %d, want 1; stdout=%q", code, out)
	}
	if !strings.Contains(out, "Rejected") {
		t.Errorf("close unknown ID: stdout = %q, want a Rejected line", out)
	}
}

func TestCloseInTxReturnsErrorsForMissingBead(t *testing.T) {
	initBeadsDir(t)
	db, _, err := openDB(os.Getenv, "")
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	req := closeRequest{reason: "r", actor: "a", now: storage.NowRFC3339Milli()}

	if _, err := closeInTx(ctx, db, req, "missing"); err == nil {
		t.Error("closeInTx(missing): err = nil, want the namespace lookup error")
	}
	for _, summarySet := range []bool{false, true} {
		r := req
		r.summarySet, r.summary = summarySet, "s"
		err := db.WithWrite(ctx, func(tx *sql.Tx) error {
			_, err := closeBeadTx(ctx, tx, r, "missing")
			return err
		})
		if err == nil {
			t.Errorf("closeBeadTx(missing, summarySet=%v): err = nil, want an error", summarySet)
		}
	}
}
