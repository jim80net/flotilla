package main

import "testing"

func TestParseRegisterArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		paneDflt  string
		wantAgent string
		wantPane  string
		wantErr   bool
	}{
		{"agent then flag (the migration form)", []string{"desk-b", "--pane", "0:0.2"}, "", "desk-b", "0:0.2", false},
		{"flag then agent", []string{"--pane", "0:0.2", "desk-b"}, "", "desk-b", "0:0.2", false},
		{"agent only, pane from default ($TMUX_PANE)", []string{"desk-b"}, "%7", "desk-b", "%7", false},
		{"agent with =flag form", []string{"desk-b", "--pane=%9"}, "", "desk-b", "%9", false},
		{"no agent", []string{"--pane", "0:0.2"}, "", "", "", true},
		{"empty", []string{}, "", "", "", true},
		{"extra positional", []string{"a", "b"}, "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent, pane, _, err := parseRegisterArgs(tc.args, tc.paneDflt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got agent=%q pane=%q", agent, pane)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if agent != tc.wantAgent || pane != tc.wantPane {
				t.Errorf("parseRegisterArgs(%v) = (agent %q, pane %q), want (%q, %q)", tc.args, agent, pane, tc.wantAgent, tc.wantPane)
			}
		})
	}
}
