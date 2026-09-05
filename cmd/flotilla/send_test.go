package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

// The mirror precedence matrix: --no-mirror forces off, --mirror forces on, else the
// roster default (mirror_inter_agent, itself default-off) decides.
func TestShouldMirror(t *testing.T) {
	cases := []struct {
		name                              string
		noMirror, doMirror, rosterDefault bool
		want                              bool
	}{
		{"default off — no flags, roster off", false, false, false, false},
		{"roster on — no flags", false, false, true, true},
		{"--mirror forces on over roster off", false, true, false, true},
		{"--mirror with roster on", false, true, true, true},
		{"--no-mirror forces off over roster on", true, false, true, false},
		{"--no-mirror with roster off", true, false, false, false},
	}
	for _, c := range cases {
		if got := shouldMirror(c.noMirror, c.doMirror, c.rosterDefault); got != c.want {
			t.Errorf("%s: shouldMirror(%v,%v,%v) = %v, want %v",
				c.name, c.noMirror, c.doMirror, c.rosterDefault, got, c.want)
		}
	}
}

// --mirror and --no-mirror together is a clear error (caught right after flag parse,
// before any roster load or tmux delivery).
func TestResolveSendLiveDriverGenericNodeUsesOverlay(t *testing.T) {
	root := t.TempDir()
	writeAgentOverlay(t, root, "backend", `{"slot":"fallback-0","surface":"codex"}`)
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "backend", Surface: "grok"}}}
	drv, live, err := resolveSendLiveDriver(cfg, "backend", "%42", "node")
	if err != nil {
		t.Fatal(err)
	}
	if drv.Name() != "codex" || live != "codex" {
		t.Fatalf("send driver=%q live=%q, want overlay codex (roster is grok)", drv.Name(), live)
	}
}

func TestCmdSendRejectsBothMirrorFlags(t *testing.T) {
	err := cmdSend([]string{"--from", "x", "--mirror", "--no-mirror", "agent", "hi"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("cmdSend(--mirror --no-mirror) = %v, want a mutually-exclusive error", err)
	}
}
