package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestLeaderExhaustionBodies(t *testing.T) {
	alert := leaderExhaustionAlertBody("xo", surface.RateLimitAccountSide)
	for _, want := range []string{"LEADER EXHAUSTION", "xo", "account-side", "auto-switch"} {
		if !strings.Contains(alert, want) {
			t.Errorf("alert missing %q: %s", want, alert)
		}
	}
	adj := leaderExhaustionAdjutantBody("xo", surface.RateLimitServerSide, "/state/charter.md")
	for _, want := range []string{"URGENT", "xo", "server-side", "ESCALATE LOUDLY", "charter"} {
		if !strings.Contains(adj, want) {
			t.Errorf("adjutant body missing %q: %s", want, adj)
		}
	}
	note := coordinatorResuscitationNotifyBody("xo", "grok")
	for _, want := range []string{"xo", "grok", "resuscitated"} {
		if !strings.Contains(note, want) {
			t.Errorf("notify missing %q: %s", want, note)
		}
	}
}
