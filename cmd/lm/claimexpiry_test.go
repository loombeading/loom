// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestClaimCLIClaimTakeoverListShowAndRoundTrip(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "claimed")
	t.Setenv("LM_NOW", "2026-09-28T00:00:00Z")

	if _, errS, code := runCmd(t, []string{"update", id, "--claim", "--claim-ttl", "30m", "--actor", "alice"}, ""); code != 0 {
		t.Fatalf("claim --claim-ttl: exit %d, stderr = %q", code, errS)
	}
	out, _, _ := runCmd(t, []string{"show", id}, "")
	if !strings.Contains(out, `claim_expires_at: "2026-09-28T00:30:00.000Z"`) {
		t.Fatalf("show front matter lacks the claim:\n%s", out)
	}
	if out, _, _ := runCmd(t, []string{"list", "--claim-expired"}, ""); strings.Contains(out, "claimed") {
		t.Fatalf("list --claim-expired before expiry = %q, want no row", out)
	}
	first := exportString(t)
	if !strings.Contains(first, `"claim_expires_at":"2026-09-28T00:30:00.000Z"`) {
		t.Fatalf("export lacks claim_expires_at:\n%s", first)
	}

	t.Setenv("LM_NOW", "2026-09-28T00:30:00Z")
	if out, _, _ := runCmd(t, []string{"list", "--claim-expired"}, ""); !strings.Contains(out, "claimed") {
		t.Fatalf("list --claim-expired at expiry = %q, want the row", out)
	}
	if out, _, _ := runCmd(t, []string{"update", id, "--claim", "--actor", "bob"}, ""); !strings.Contains(out, "Updated") {
		t.Fatalf("takeover = %q, want Updated", out)
	}
	show, _, _ := runCmd(t, []string{"show", id}, "")
	if !strings.Contains(show, `claimed_by: "bob"`) || !strings.Contains(show, "claim_expires_at: null") || !strings.Contains(show, "claim_takeover") {
		t.Fatalf("show after takeover:\n%s", show)
	}

	initBeadsDir(t)
	mustImport(t, first)
	if second := exportString(t); first != second {
		t.Fatalf("export -> import -> export differs:\n%s\n---\n%s", first, second)
	}
}

func TestClaimCLIReadyClaim(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "claimed via ready")
	t.Setenv("LM_NOW", "2026-09-28T00:00:00Z")

	out, errS, code := runCmd(t, []string{"ready", "--claim", "--claim-ttl", "20m", "--actor", "alice"}, "")
	if code != 0 {
		t.Fatalf("ready --claim --claim-ttl: exit %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "Claimed:") {
		t.Fatalf("ready --claim --claim-ttl output = %q, want a Claimed line", out)
	}
	show, _, _ := runCmd(t, []string{"show", id}, "")
	if !strings.Contains(show, `claim_expires_at: "2026-09-28T00:20:00.000Z"`) {
		t.Fatalf("show front matter lacks the claim from ready --claim:\n%s", show)
	}

	if out, _, _ := runCmd(t, []string{"list", "--claim-expired"}, ""); strings.Contains(out, "claimed via ready") {
		t.Fatalf("list --claim-expired before expiry = %q, want no row", out)
	}
	t.Setenv("LM_NOW", "2026-09-28T00:20:00Z")
	if out, _, _ := runCmd(t, []string{"list", "--claim-expired"}, ""); !strings.Contains(out, "claimed via ready") {
		t.Fatalf("list --claim-expired at expiry = %q, want the row", out)
	}
}

func TestClaimCLIReadyRejectsClaimWithoutClaim(t *testing.T) {
	initBeadsDir(t)
	mustCreateBead(t, "--title", "t")
	_, errS, code := runCmd(t, []string{"ready", "--claim-ttl", "20m"}, "")
	if code == 0 || !strings.Contains(errS, "--claim-ttl") {
		t.Fatalf("ready --claim-ttl without --claim: exit %d stderr %q, want a --claim-ttl rejection", code, errS)
	}
}

func TestClaimCLIRejectsBadFlags(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	for _, args := range [][]string{
		{"update", id, "--claim", "--claim-ttl", "0s"},
		{"update", id, "--claim", "--claim-ttl", "-5m"},
		{"update", id, "--claim-ttl", "5m"},
	} {
		out, _, code := runCmd(t, args, "")
		if code == 0 || !strings.Contains(out, "--claim-ttl") {
			t.Errorf("%v: exit %d out %q, want a --claim-ttl rejection", args, code, out)
		}
	}
	t.Setenv("LM_NOW", "not-a-time")
	if _, _, code := runCmd(t, []string{"list", "--claim-expired"}, ""); code == 0 {
		t.Error("list --claim-expired with invalid LM_NOW: want non-zero exit")
	}
	if _, _, code := runCmd(t, []string{"update", id, "--claim"}, ""); code == 0 {
		t.Error("update with invalid LM_NOW: want non-zero exit")
	}
	if _, _, code := runCmd(t, []string{"ready", "--claim", "--claim-ttl", "20m"}, ""); code == 0 {
		t.Error("ready --claim with invalid LM_NOW: want non-zero exit")
	}
}
