// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package xdg

import "testing"

func envMap(m map[string]string) LookupFunc {
	return func(k string) string { return m[k] }
}

func TestDataHomeXDGOverride(t *testing.T) {
	got, via, err := DataHome(envMap(map[string]string{"XDG_DATA_HOME": "/xdg/data"}), "loom")
	if err != nil {
		t.Fatalf("DataHome: %v", err)
	}
	if got != "/xdg/data/loom" {
		t.Fatalf("dir = %q, want /xdg/data/loom", got)
	}
	if via != "XDG_DATA_HOME" {
		t.Fatalf("via = %q, want XDG_DATA_HOME", via)
	}
}

func TestDataHomeIgnoresRelativeOverride(t *testing.T) {
	got, via, err := DataHome(envMap(map[string]string{"XDG_DATA_HOME": "relative", "HOME": "/home/u"}), "loom")
	if err != nil {
		t.Fatalf("DataHome: %v", err)
	}
	if got != "/home/u/.local/share/loom" {
		t.Fatalf("dir = %q, want /home/u/.local/share/loom", got)
	}
	if via != "default" {
		t.Fatalf("via = %q, want default", via)
	}
}

func TestDataHomeDefault(t *testing.T) {
	got, via, err := DataHome(envMap(map[string]string{"HOME": "/home/u"}), "loom")
	if err != nil {
		t.Fatalf("DataHome: %v", err)
	}
	if got != "/home/u/.local/share/loom" {
		t.Fatalf("dir = %q, want /home/u/.local/share/loom", got)
	}
	if via != "default" {
		t.Fatalf("via = %q, want default", via)
	}
}

func TestDataHomeNoHomeIsError(t *testing.T) {
	_, _, err := DataHome(envMap(nil), "loom")
	if err == nil {
		t.Fatal("expected an error when $HOME is unset")
	}
}

func TestConfigHomeXDGOverride(t *testing.T) {
	got, via, err := ConfigHome(envMap(map[string]string{"XDG_CONFIG_HOME": "/xdg/config"}), "loom")
	if err != nil {
		t.Fatalf("ConfigHome: %v", err)
	}
	if got != "/xdg/config/loom" {
		t.Fatalf("dir = %q, want /xdg/config/loom", got)
	}
	if via != "XDG_CONFIG_HOME" {
		t.Fatalf("via = %q, want XDG_CONFIG_HOME", via)
	}
}

func TestConfigHomeDefault(t *testing.T) {
	got, via, err := ConfigHome(envMap(map[string]string{"HOME": "/home/u"}), "loom")
	if err != nil {
		t.Fatalf("ConfigHome: %v", err)
	}
	if got != "/home/u/.config/loom" {
		t.Fatalf("dir = %q, want /home/u/.config/loom", got)
	}
	if via != "default" {
		t.Fatalf("via = %q, want default", via)
	}
}

func TestConfigHomeNoHomeIsError(t *testing.T) {
	_, _, err := ConfigHome(envMap(nil), "loom")
	if err == nil {
		t.Fatal("expected an error when $HOME is unset")
	}
}
