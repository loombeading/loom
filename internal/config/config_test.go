// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, found, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatalf("found = true, want false for a missing file")
	}
	if cfg.RetentionDays != 0 || cfg.ListLimit != 0 || cfg.NamespaceDefault != "" || cfg.BusyTimeoutMS != 0 {
		t.Fatalf("cfg = %+v, want all zero fields", cfg)
	}
}

func TestLoadEmptyFile(t *testing.T) {
	path := writeConfig(t, "")
	cfg, found, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true for an existing (empty) file")
	}
	if cfg.RetentionDays != 0 || cfg.ListLimit != 0 || cfg.NamespaceDefault != "" || cfg.BusyTimeoutMS != 0 {
		t.Fatalf("cfg = %+v, want all zero fields", cfg)
	}
}

func TestLoadAllFourKeys(t *testing.T) {
	path := writeConfig(t, `{
  "note": "top-level note is ignored",
  "compaction": {"retention_days": 45},
  "list": {"limit": 100, "note": "section note is ignored"},
  "namespace": {"default": "web"},
  "db": {"busy_timeout_ms": 9000}
}
`)
	cfg, found, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if cfg.RetentionDays != 45 {
		t.Fatalf("RetentionDays = %v, want 45", cfg.RetentionDays)
	}
	if cfg.ListLimit != 100 {
		t.Fatalf("ListLimit = %v, want 100", cfg.ListLimit)
	}
	if cfg.NamespaceDefault != "web" {
		t.Fatalf("NamespaceDefault = %v, want web", cfg.NamespaceDefault)
	}
	if cfg.BusyTimeoutMS != 9000 {
		t.Fatalf("BusyTimeoutMS = %v, want 9000", cfg.BusyTimeoutMS)
	}
}

func TestLoadSLOKeys(t *testing.T) {
	path := writeConfig(t, `{"slo":{"warn_minutes":20,"incident_minutes":60,"target_percent":95}}`)
	cfg, found, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if cfg.SLOWarnMinutes != 20 {
		t.Fatalf("SLOWarnMinutes = %v, want 20", cfg.SLOWarnMinutes)
	}
	if cfg.SLOIncidentMinutes != 60 {
		t.Fatalf("SLOIncidentMinutes = %v, want 60", cfg.SLOIncidentMinutes)
	}
	if cfg.SLOTargetPercent != 95 {
		t.Fatalf("SLOTargetPercent = %v, want 95", cfg.SLOTargetPercent)
	}
}

func TestLoadCreateRequireMilestone(t *testing.T) {
	var nilCfg *Config
	if nilCfg.RequireMilestone() {
		t.Fatal("RequireMilestone(nil cfg) = true, want false")
	}
	path := writeConfig(t, `{"create":{"note":"x","require_milestone":true}}`)
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.RequireMilestone() {
		t.Fatal("RequireMilestone = false, want true")
	}
}

func TestLoadUnknownSectionErrors(t *testing.T) {
	path := writeConfig(t, `{"bogus":{"foo":1}}`)
	if _, _, err := Load(path); err == nil {
		t.Fatal("Load: want error for unknown section, got nil")
	}
}

func TestLoadUnknownKeyErrors(t *testing.T) {
	path := writeConfig(t, `{"list":{"bogus":1}}`)
	if _, _, err := Load(path); err == nil {
		t.Fatal("Load: want error for unknown key, got nil")
	}
}

func TestLoadTypeMismatchErrors(t *testing.T) {
	cases := []string{
		`{"list":{"limit":"fifty"}}`,
		`{"namespace":{"default":5}}`,
		`{"note":1}`,
	}
	for _, c := range cases {
		path := writeConfig(t, c)
		if _, _, err := Load(path); err == nil {
			t.Fatalf("Load(%q): want error for type mismatch, got nil", c)
		}
	}
}

func TestLoadSyntaxErrorErrors(t *testing.T) {
	cases := []string{
		`{"list":{"limit":1}`,
		`{"list":{"limit"}}`,
		`{"list":{"limit":}}`,
		`{"list":{"limit":1}} {}`,
		"[list]\nlimit = 1\n",
	}
	for _, c := range cases {
		path := writeConfig(t, c)
		if _, _, err := Load(path); err == nil {
			t.Fatalf("Load(%q): want syntax error, got nil", c)
		}
	}
}

func TestLoadUnreadablePathErrors(t *testing.T) {
	if _, _, err := Load(t.TempDir()); err == nil {
		t.Fatal("Load(directory): want read error, got nil")
	}
}

func TestOrHelpersHandleNilConfig(t *testing.T) {
	var cfg *Config
	if got := cfg.ListLimitOr(50); got != 50 {
		t.Fatalf("ListLimitOr(nil cfg) = %d, want 50", got)
	}
	if got := cfg.NamespaceDefaultOr("lm"); got != "lm" {
		t.Fatalf("NamespaceDefaultOr(nil cfg) = %q, want lm", got)
	}
	if got := cfg.BusyTimeoutMSOr(5000); got != 5000 {
		t.Fatalf("BusyTimeoutMSOr(nil cfg) = %d, want 5000", got)
	}
}

func TestOrHelpersPreferSetValue(t *testing.T) {
	cfg := &Config{ListLimit: 100, NamespaceDefault: "web", BusyTimeoutMS: 9000}
	if got := cfg.ListLimitOr(50); got != 100 {
		t.Fatalf("ListLimitOr = %d, want 100", got)
	}
	if got := cfg.NamespaceDefaultOr("lm"); got != "web" {
		t.Fatalf("NamespaceDefaultOr = %q, want web", got)
	}
	if got := cfg.BusyTimeoutMSOr(5000); got != 9000 {
		t.Fatalf("BusyTimeoutMSOr = %d, want 9000", got)
	}
}

func TestShortLenOr(t *testing.T) {
	var nilCfg *Config
	if got := nilCfg.ShortLenOr(4); got != 4 {
		t.Fatalf("nil cfg = %d, want 4", got)
	}
	path := writeConfig(t, `{"id":{"note":"x","short_len":6}}`)
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ShortLenOr(4); got != 6 {
		t.Fatalf("configured = %d, want 6", got)
	}
	if got := (&Config{IDShortLen: 1}).ShortLenOr(4); got != 3 {
		t.Fatalf("below minimum = %d, want 3", got)
	}
	if got := (&Config{}).ShortLenOr(4); got != 4 {
		t.Fatalf("unset = %d, want 4", got)
	}
}
