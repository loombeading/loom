// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import "testing"

func TestResolveActor(t *testing.T) {
	cases := []struct {
		name       string
		flag       string
		env        map[string]string
		osUsername string
		want       string
	}{
		{"flag wins over everything", "alice", map[string]string{"LM_ACTOR": "bob"}, "carol", "alice"},
		{"env wins over OS username", "", map[string]string{"LM_ACTOR": "bob"}, "carol", "bob"},
		{"OS username is the fallback", "", map[string]string{}, "carol", "carol"},
		{"empty env value falls through", "", map[string]string{"LM_ACTOR": ""}, "carol", "carol"},
		{"LM_ACTOR empty, CLAUDE_CODE_SESSION_ID wins over OS username", "", map[string]string{"LM_ACTOR": "", "CLAUDE_CODE_SESSION_ID": "sess-uuid"}, "carol", "sess-uuid"},
		{"LM_ACTOR wins over CLAUDE_CODE_SESSION_ID", "", map[string]string{"LM_ACTOR": "bob", "CLAUDE_CODE_SESSION_ID": "sess-uuid"}, "carol", "bob"},
		{"both LM_ACTOR and CLAUDE_CODE_SESSION_ID empty falls through to OS username", "", map[string]string{"LM_ACTOR": "", "CLAUDE_CODE_SESSION_ID": ""}, "carol", "carol"},
		{"empty CLAUDE_CODE_SESSION_ID value falls through", "", map[string]string{"CLAUDE_CODE_SESSION_ID": ""}, "carol", "carol"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveActor(c.flag, envFunc(c.env), c.osUsername)
			if got != c.want {
				t.Errorf("ResolveActor(%q, ..., %q) = %q, want %q", c.flag, c.osUsername, got, c.want)
			}
		})
	}
}
