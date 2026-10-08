// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestCloseNewlyReadyHonorsNamespace(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker", "--namespace", "web")
	webDown := mustCreateBead(t, "--title", "web-down", "--namespace", "web")
	appDown := mustCreateBead(t, "--title", "app-down", "--namespace", "app")
	for _, d := range []string{webDown, appDown} {
		if _, errS, code := runCmd(t, []string{"dep", "add", d, blocker, "--type", "blocks"}, ""); code != 0 {
			t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
		}
	}

	t.Setenv("LM_NAMESPACE", "web")
	out, errS, code := runCmd(t, []string{"close", blocker, "--reason", "done"}, "")
	if code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}
	_, newly, _ := strings.Cut(out, "## Newly ready")
	if !strings.Contains(newly, `Filter: namespace="web"`) || !strings.Contains(newly, "web-down") || strings.Contains(newly, "app-down") {
		t.Fatalf("close under LM_NAMESPACE=web: Newly ready = %q, want only web-down with a Filter line", newly)
	}
}

func TestGateListHonorsNamespace(t *testing.T) {
	initBeadsDir(t)
	webTarget := mustCreateBead(t, "--title", "web-target", "--namespace", "web")
	appTarget := mustCreateBead(t, "--title", "app-target", "--namespace", "app")
	mustGateCreateCmd(t, []string{webTarget}, "web-gate", "--namespace", "web")
	mustGateCreateCmd(t, []string{appTarget}, "app-gate", "--namespace", "app")

	out, errS, code := runCmd(t, []string{"gate", "--namespace", "web"}, "")
	if code != 0 {
		t.Fatalf("gate --namespace web: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.HasPrefix(out, `Filter: namespace="web"`+"\n") || !strings.Contains(out, "web-gate") || strings.Contains(out, "app-gate") {
		t.Fatalf("gate --namespace web = %q, want only web-gate after a Filter line", out)
	}

	t.Setenv("LM_NAMESPACE", "app")
	out, _, _ = runCmd(t, []string{"gate"}, "")
	if !strings.Contains(out, "app-gate") || strings.Contains(out, "web-gate") {
		t.Fatalf("gate under LM_NAMESPACE=app = %q, want only app-gate", out)
	}

	out, _, _ = runCmd(t, []string{"gate", "--namespace", ""}, "")
	if strings.Contains(out, "Filter:") || !strings.Contains(out, "app-gate") || !strings.Contains(out, "web-gate") {
		t.Fatalf(`gate --namespace "" = %q, want both Gates and no Filter line`, out)
	}
}

func TestReadyAndBlockedHonorConfigListLimit(t *testing.T) {
	initBeadsDir(t)
	blocker := mustCreateBead(t, "--title", "blocker")
	for range 3 {
		d := mustCreateBead(t, "--title", "t")
		if _, errS, code := runCmd(t, []string{"dep", "add", d, blocker, "--type", "blocks"}, ""); code != 0 {
			t.Fatalf("dep add: exit code = %d, stderr = %q", code, errS)
		}
		mustCreateBead(t, "--title", "r")
	}
	writeBeadsConfig(t, `{"list":{"limit":2}}`)

	for _, cmd := range []string{"ready", "blocked"} {
		out, errS, code := runCmd(t, []string{cmd}, "")
		if code != 0 {
			t.Fatalf("%s: exit code = %d, stderr = %q", cmd, code, errS)
		}
		if !strings.Contains(out, "Truncated: showing 2 of ") {
			t.Errorf("%s stdout = %q, want truncation under config list.limit=2", cmd, out)
		}
		out, _, _ = runCmd(t, []string{cmd, "--limit", "0"}, "")
		if strings.Contains(out, "Truncated") {
			t.Errorf("%s --limit 0 stdout = %q, want --limit to override config", cmd, out)
		}
	}
}

func TestGateResolveAcceptsMultipleIDs(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")
	ga := mustGateCreateCmd(t, []string{a}, "ga")
	gb := mustGateCreateCmd(t, []string{b}, "gb")

	out, errS, code := runCmd(t, []string{"gate", "resolve", ga, "no-such-id", gb, "--reason", "go"}, "")
	if code != 1 {
		t.Fatalf("gate resolve with one bad ID: exit code = %d, want 1 (stderr = %q)", code, errS)
	}
	if strings.Count(out, "- Resolved: ") != 2 || !strings.Contains(out, "no-such-id") {
		t.Fatalf("gate resolve = %q, want two Resolved lines and the rejected ID named", out)
	}
	_, newly, _ := strings.Cut(out, "## Newly ready")
	if !strings.Contains(newly, "] a") || !strings.Contains(newly, "] b") {
		t.Fatalf("gate resolve Newly ready = %q, want both a and b", newly)
	}
}

func TestGateRejectAcceptsMultipleIDs(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	b := mustCreateBead(t, "--title", "b")
	ga := mustGateCreateCmd(t, []string{a}, "ga")
	gb := mustGateCreateCmd(t, []string{b}, "gb")

	out, errS, code := runCmd(t, []string{"gate", "reject", ga, gb, "--reason", "no"}, "")
	if code != 0 {
		t.Fatalf("gate reject: exit code = %d, stderr = %q", code, errS)
	}
	cancelled := out[strings.Index(out, "## Cancelled"):strings.Index(out, "## Newly ready")]
	if strings.Count(out, "- Rejected: ") != 2 || !strings.Contains(cancelled, "] a") || !strings.Contains(cancelled, "] b") {
		t.Fatalf("gate reject = %q, want two Rejected lines and both a and b cancelled", out)
	}
}

func TestEnumFilterErrorPaths(t *testing.T) {
	initBeadsDir(t)
	a := mustCreateBead(t, "--title", "a")
	ga := mustGateCreateCmd(t, []string{a}, "ga")

	if _, errS, code := runCmd(t, []string{"gate", "--namespace", "Bad"}, ""); code != 1 || !strings.Contains(errS, "namespace") {
		t.Errorf("gate --namespace Bad: exit code = %d, stderr = %q, want 1 naming the namespace", code, errS)
	}

	t.Setenv("LM_READONLY", "1")
	out, errS, code := runCmd(t, []string{"gate", "resolve", ga, "--reason", "go"}, "")
	if code != 1 || strings.Contains(out, "- Resolved: ") {
		t.Errorf("gate resolve under LM_READONLY: exit code = %d, stdout = %q, stderr = %q, want 1 and nothing resolved", code, out, errS)
	}
	t.Setenv("LM_READONLY", "")

	t.Setenv("LM_NAMESPACE", "Bad")
	if _, errS, code := runCmd(t, []string{"gate", "resolve", ga, "--reason", "go"}, ""); code != 1 || !strings.Contains(errS, "namespace") {
		t.Errorf("gate resolve under an invalid LM_NAMESPACE: exit code = %d, stderr = %q, want 1 naming the namespace", code, errS)
	}
	t.Setenv("LM_NAMESPACE", "")

	writeBeadsConfig(t, `{"list":{"bogus":1}}`)
	for _, cmd := range []string{"ready", "blocked"} {
		if _, errS, code := runCmd(t, []string{cmd}, ""); code != 1 || !strings.Contains(errS, "bogus") {
			t.Errorf("%s with a broken config: exit code = %d, stderr = %q, want 1 naming the key", cmd, code, errS)
		}
	}
}
