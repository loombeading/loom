// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func closedAtFrom(t *testing.T, out string) time.Time {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if after, ok := strings.CutPrefix(line, "closed_at: "); ok {
			v := after
			v = strings.TrimSuffix(strings.TrimPrefix(v, `"`), `"`)
			ts, err := time.Parse("2006-01-02T15:04:05.000Z07:00", v)
			if err != nil {
				t.Fatalf("parse closed_at %q: %v", v, err)
			}
			return ts
		}
	}
	t.Fatalf("show output = %q, want a closed_at front-matter line", out)
	return time.Time{}
}

func descriptionSection(t *testing.T, out string) string {
	t.Helper()
	start := strings.Index(out, "## Description")
	if start < 0 {
		t.Fatalf("show output = %q, want a ## Description section", out)
	}
	rest := out[start:]
	before, _, ok := strings.Cut(rest, "## Dependencies")
	if !ok {
		t.Fatalf("show output = %q, want a ## Dependencies section after ## Description", out)
	}
	return before
}

func TestCompactionExceedingRetentionOmitsDescription(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"compaction":{"retention_days":1}}`)

	id := mustCreateBead(t, "--title", "t", "--summary", "S1")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	desc := descriptionSection(t, out)
	if strings.Contains(desc, "long body text") {
		t.Errorf("description section = %q, want the description omitted once compacted", desc)
	}
	if !strings.Contains(desc, "S1") {
		t.Errorf("description section = %q, want the summary shown in the description's place", desc)
	}
	if !strings.Contains(desc, "Compacted: description omitted") {
		t.Errorf("description section = %q, want the fixed Compacted: notice line", desc)
	}
}

func TestCompactionWithinRetentionShowsDescription(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"compaction":{"retention_days":30}}`)

	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(out, "long body text") {
		t.Errorf("show output = %q, want the description shown within the retention period", out)
	}
	if strings.Contains(out, "Compacted:") {
		t.Errorf("show output = %q, want no Compacted: notice within the retention period", out)
	}
}

func TestCompactionSameBDNowRepeatedShowIsIdentical(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"compaction":{"retention_days":1}}`)

	id := mustCreateBead(t, "--title", "t", "--summary", "S1")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")

	out1, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show (1st): exit code = %d, stderr = %q", code, errS)
	}
	out2, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show (2nd): exit code = %d, stderr = %q", code, errS)
	}
	if out1 != out2 {
		t.Errorf("show output differs across two calls with the same LM_NOW:\n1st: %q\n2nd: %q", out1, out2)
	}
}

func TestCompactionDoesNotChangeDBFileOrExportBytes(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"compaction":{"retention_days":1}}`)

	id := mustCreateBead(t, "--title", "t", "--summary", "S1")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	dbPath := beadsDBPath(t)
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read loom.db before: %v", err)
	}
	exportBefore, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export (before): exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(descriptionSection(t, out), "Compacted:") {
		t.Fatalf("show output = %q, want it compacted so this test exercises the projection path", out)
	}

	after, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read loom.db after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("loom.db bytes changed after a compacted `lm show`")
	}

	exportAfter, errS, code := runCmd(t, []string{"export"}, "")
	if code != 0 {
		t.Fatalf("export (after): exit code = %d, stderr = %q", code, errS)
	}
	if exportBefore != exportAfter {
		t.Errorf("export JSONL bytes changed after a compacted `lm show`:\nbefore: %q\nafter:  %q", exportBefore, exportAfter)
	}
}

func TestCompactionWithoutSummaryNeverCompacts(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"compaction":{"retention_days":1}}`)

	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	desc := descriptionSection(t, out)
	if !strings.Contains(desc, "long body text") {
		t.Errorf("description section = %q, want the full description shown even long after closing when the Bead has no attached summary", desc)
	}
	if strings.Contains(desc, "Compacted:") {
		t.Errorf("description section = %q, want no Compacted: notice when the Bead has no attached summary", desc)
	}
}

func TestCompactionDefaultRetentionCompactsImmediatelyWithSummary(t *testing.T) {
	initBeadsDir(t)

	id := mustCreateBead(t, "--title", "t", "--summary", "S1")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	preOut, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show (pre): exit code = %d, stderr = %q", code, errS)
	}
	closedAt := closedAtFrom(t, preOut)
	t.Setenv("LM_NOW", closedAt.Add(time.Second).Format(time.RFC3339))

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	desc := descriptionSection(t, out)
	if strings.Contains(desc, "long body text") {
		t.Errorf("description section = %q, want the description omitted once closed under the default retention_days=0", desc)
	}
	if !strings.Contains(desc, "S1") {
		t.Errorf("description section = %q, want the summary shown in the description's place", desc)
	}
	if !strings.Contains(desc, "retention_days=0") {
		t.Errorf("description section = %q, want the Compacted: notice to report retention_days=0", desc)
	}
}

func TestCompactionRetentionDays30DelaysCompactionUntilExceededWithSummary(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"compaction":{"retention_days":30}}`)

	id := mustCreateBead(t, "--title", "t", "--summary", "S1")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	preOut, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show (pre): exit code = %d, stderr = %q", code, errS)
	}
	closedAt := closedAtFrom(t, preOut)

	t.Setenv("LM_NOW", closedAt.Add(29*24*time.Hour).Format(time.RFC3339))
	within, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show (within): exit code = %d, stderr = %q", code, errS)
	}
	if strings.Contains(within, "Compacted:") {
		t.Errorf("show output = %q, want no Compacted: notice within the 30-day retention period even with a summary", within)
	}

	t.Setenv("LM_NOW", closedAt.Add(31*24*time.Hour).Format(time.RFC3339))
	after, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show (after): exit code = %d, stderr = %q", code, errS)
	}
	if !strings.Contains(after, "Compacted:") {
		t.Errorf("show output = %q, want a Compacted: notice after the 30-day retention period with a summary", after)
	}
}

func TestCompactionWithSummaryShowsSummaryInsteadOfDescription(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"compaction":{"retention_days":1}}`)

	id := mustCreateBead(t, "--title", "t", "--summary", "the summary text")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	desc := descriptionSection(t, out)
	if strings.Contains(desc, "long body text") {
		t.Errorf("description section = %q, want the description omitted", desc)
	}
	if !strings.Contains(desc, "the summary text") {
		t.Errorf("description section = %q, want the summary shown in the description's place", desc)
	}
}

func TestShowFullBypassesCompactionProjection(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"compaction":{"retention_days":1}}`)

	id := mustCreateBead(t, "--title", "t", "--summary", "S1")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")

	out, errS, code := runCmd(t, []string{"show", "--full", id}, "")
	if code != 0 {
		t.Fatalf("show --full: exit code = %d, stderr = %q", code, errS)
	}
	desc := descriptionSection(t, out)
	if !strings.Contains(desc, "long body text") {
		t.Errorf("description section = %q, want the full description shown under --full", desc)
	}
	if strings.Contains(desc, "Compacted:") {
		t.Errorf("description section = %q, want no Compacted: notice under --full", desc)
	}
}

func TestShowFullSameOutputWhenNotCompacted(t *testing.T) {
	initBeadsDir(t)

	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}

	without, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	with, errS, code := runCmd(t, []string{"show", "--full", id}, "")
	if code != 0 {
		t.Fatalf("show --full: exit code = %d, stderr = %q", code, errS)
	}
	if without != with {
		t.Errorf("show output differs with/without --full for a non-compacted Bead:\nwithout: %q\nwith:    %q", without, with)
	}
}

func TestShowMultilineDescriptionPreservesNewlines(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	if _, errS, code := runCmd(t, []string{"update", "--description", "line one\nline two\nline three", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	desc := descriptionSection(t, out)
	if !strings.Contains(desc, "line one\nline two\nline three\n") {
		t.Errorf("description section = %q, want the newlines preserved across three lines", desc)
	}
}

func TestShowDescriptionHijackAttemptsNeutralized(t *testing.T) {
	initBeadsDir(t)
	id := mustCreateBead(t, "--title", "t")
	body := "# fake section\n" +
		"```\n" +
		"fake fence\n" +
		"```\n" +
		"mid-line # and | are fine\n" +
		"- a list item\n" +
		"| a | table |\n" +
		"| --- | --- |\n"
	if _, errS, code := runCmd(t, []string{"update", "--description", body, id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}

	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	desc := descriptionSection(t, out)
	if !strings.Contains(desc, "\\# fake section") {
		t.Errorf("description section = %q, want a leading \"#\" escaped", desc)
	}
	if !strings.Contains(desc, "\\```\nfake fence\n\\```") {
		t.Errorf("description section = %q, want leading code fences escaped", desc)
	}
	if !strings.Contains(desc, "mid-line # and | are fine") {
		t.Errorf("description section = %q, want mid-line \"#\" and \"|\" untouched", desc)
	}
	if !strings.Contains(desc, "- a list item") {
		t.Errorf("description section = %q, want list syntax untouched", desc)
	}
	if !strings.Contains(desc, "| a | table |") {
		t.Errorf("description section = %q, want table syntax untouched", desc)
	}
}

func TestShowCompactedSummaryPreservesNewlines(t *testing.T) {
	initBeadsDir(t)
	writeBeadsConfig(t, `{"compaction":{"retention_days":1}}`)

	id := mustCreateBead(t, "--title", "t", "--summary", "summary line one\nsummary line two")
	if _, errS, code := runCmd(t, []string{"update", "--description", "long body text", id}, ""); code != 0 {
		t.Fatalf("update --description: exit code = %d, stderr = %q", code, errS)
	}
	if _, errS, code := runCmd(t, []string{"close", id}, ""); code != 0 {
		t.Fatalf("close: exit code = %d, stderr = %q", code, errS)
	}

	t.Setenv("LM_NOW", "2099-01-01T00:00:00Z")
	out, errS, code := runCmd(t, []string{"show", id}, "")
	if code != 0 {
		t.Fatalf("show: exit code = %d, stderr = %q", code, errS)
	}
	desc := descriptionSection(t, out)
	if !strings.Contains(desc, "summary line one\nsummary line two\n") {
		t.Errorf("description section = %q, want the summary's newlines preserved", desc)
	}
}
