// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import "testing"

func TestValidateNamespace(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"lm", false},
		{"web2", false},
		{"a", false},
		{"", true},
		{"-lm", true},
		{"bd-web", true},
		{"2bd", true},
		{"Bd", true},
		{"bd_web", true},
	}
	for _, c := range cases {
		err := ValidateNamespace(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateNamespace(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
		}
	}
}

func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveNamespaceForCreate(t *testing.T) {
	cases := []struct {
		name    string
		flag    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{"flag wins", "web", map[string]string{"LM_NAMESPACE": "env"}, "web", false},
		{"env wins over default", "", map[string]string{"LM_NAMESPACE": "env"}, "env", false},
		{"default when neither set", "", nil, DefaultNamespace, false},
		{"invalid flag", "Bad", nil, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveNamespaceForCreate(c.flag, envFunc(c.env), DefaultNamespace)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err == nil && got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestResolveNamespaceForFilter(t *testing.T) {
	cases := []struct {
		name    string
		flag    string
		flagSet bool
		env     map[string]string
		def     string
		want    string
		wantErr bool
	}{
		{"flag wins", "web", true, map[string]string{"LM_NAMESPACE": "env"}, "cfg", "web", false},
		{"env wins over default", "", false, map[string]string{"LM_NAMESPACE": "env"}, "cfg", "env", false},
		{"config default when neither flag nor env set", "", false, nil, "cfg", "cfg", false},
		{"no filter when nothing set", "", false, nil, "", "", false},
		{"explicit empty flag resets env and default", "", true, map[string]string{"LM_NAMESPACE": "env"}, "cfg", "", false},
		{"invalid flag", "Bad", true, nil, "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveNamespaceForFilter(c.flag, c.flagSet, envFunc(c.env), c.def)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err == nil && got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
