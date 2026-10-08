// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

const stdinReasonBody = "first line of the reason\nsecond line of the reason"

func assertShowContainsReason(t *testing.T, id string) {
	t.Helper()
	showOut, errS, code := runCmd(t, []string{"show", id, "--full"}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	for _, want := range []string{"first line of the reason", "second line of the reason"} {
		if !strings.Contains(showOut, want) {
			t.Errorf("show output = %q, want the stdin reason line %q", showOut, want)
		}
	}
	if strings.Contains(showOut, "reason: -\n") || strings.Contains(showOut, "(reason: -)") {
		t.Errorf("show output = %q, want the literal \"-\" not recorded as the reason", showOut)
	}
}

func TestReasonDashReadsStdin(t *testing.T) {
	cases := []struct {
		name string
		args func(id string) []string
	}{
		{"close", func(id string) []string { return []string{"close", id, "--reason", "-"} }},
		{"close with summary", func(id string) []string { return []string{"close", id, "--summary", "s", "--reason", "-"} }},
		{"cost add", func(id string) []string {
			return []string{"cost", "add", id, "--in", "1", "--out", "2", "--reason", "-"}
		}},
		{"rework add", func(id string) []string { return []string{"rework", "add", id, "--cause", "ci", "--reason", "-"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			initBeadsDir(t)
			id := mustCreateBead(t, "--title", "t")
			if _, errS, code := runCmd(t, c.args(id), stdinReasonBody); code != 0 {
				t.Fatalf("%s --reason -: exit code = %d, stderr = %q", c.name, code, errS)
			}
			assertShowContainsReason(t, id)
		})
	}
}

func TestReasonDashStdinReadErrorRejected(t *testing.T) {
	cases := []struct {
		name string
		args func(id string) []string
	}{
		{"close", func(id string) []string { return []string{"close", id, "--reason", "-"} }},
		{"cost add", func(id string) []string { return []string{"cost", "add", id, "--in", "1", "--reason", "-"} }},
		{"rework add", func(id string) []string { return []string{"rework", "add", id, "--cause", "ci", "--reason", "-"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			initBeadsDir(t)
			id := mustCreateBead(t, "--title", "t")
			var out, errBuf bytes.Buffer
			if code := run(c.args(id), erroringReader{}, &out, &errBuf); code != 1 {
				t.Fatalf("%s --reason - (stdin read error): exit code = %d, want 1; stdout=%q stderr=%q", c.name, code, out.String(), errBuf.String())
			}
			if !strings.Contains(errBuf.String(), "read stdin") {
				t.Errorf("stderr = %q, want a read stdin error", errBuf.String())
			}
			showOut, _, code := runCmd(t, []string{"show", id}, "")
			if code != 0 {
				t.Fatal("show failed")
			}
			if !strings.Contains(showOut, `status: "open"`) || !strings.Contains(showOut, "token_cost: 0\n") {
				t.Errorf("show output = %q, want the Bead unchanged when stdin cannot be read", showOut)
			}
		})
	}
}

func TestMultipleStdinFlagsRejected(t *testing.T) {
	cases := []struct {
		name  string
		args  func(id string) []string
		flags string
	}{
		{"create", func(string) []string {
			return []string{"create", "--title", "t2", "--description", "-", "--reason", "-"}
		}, "--description, --reason"},
		{"update", func(id string) []string {
			return []string{"update", id, "--description", "-", "--summary", "-"}
		}, "--description, --summary"},
		{"close", func(id string) []string {
			return []string{"close", id, "--summary", "-", "--reason", "-"}
		}, "--summary, --reason"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			initBeadsDir(t)
			id := mustCreateBead(t, "--title", "t")
			out, errS, code := runCmd(t, c.args(id), stdinReasonBody)
			if code != 1 {
				t.Fatalf("%s with two stdin flags: exit code = %d, want 1 (stdout=%q stderr=%q)", c.name, code, out, errS)
			}
			if !strings.Contains(errS, "only one flag may read stdin") || !strings.Contains(errS, c.flags) {
				t.Errorf("stderr = %q, want a rejection naming %s", errS, c.flags)
			}
			listOut, _, _ := runCmd(t, []string{"list"}, "")
			if strings.Contains(listOut, "] t2\n") {
				t.Errorf("list = %q, want no Bead created", listOut)
			}
			showOut, _, _ := runCmd(t, []string{"show", id, "--full"}, "")
			if !strings.Contains(showOut, `status: "open"`) || strings.Contains(showOut, "first line of the reason") {
				t.Errorf("show output = %q, want the Bead unchanged", showOut)
			}
		})
	}
}

func TestSingleStdinFlagWithInlineOthersAccepted(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"update", id, "--description", "inline", "--reason", "-"}, stdinReasonBody); code != 0 {
		t.Fatalf("update: exit code = %d, stderr = %q", code, errS)
	}
	assertShowContainsReason(t, id)
}
