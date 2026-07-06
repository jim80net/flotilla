package main

import (
	"strings"
	"testing"
)

func TestAdjutantPingBodyTouchesLeaderAck(t *testing.T) {
	const leader = "alpha-xo"
	const ack = "/state/flotilla-alpha-xo-alive"
	got := adjutantPingBody(leader, ack)
	for _, want := range []string{leader, ack, "touch " + ack} {
		if !strings.Contains(got, want) {
			t.Errorf("adjutant ping missing %q\nfull: %s", want, got)
		}
	}
}

func TestAdjutantMaterialBodyNamesLeaderAndTriage(t *testing.T) {
	got := adjutantMaterialBody("xo", []string{"backend Working→Idle"})
	for _, want := range []string{"xo", "backend Working→Idle", "Triage", "buffer judgment"} {
		if !strings.Contains(got, want) {
			t.Errorf("adjutant material missing %q\nfull: %s", want, got)
		}
	}
}

func TestLeaderPingBodyUnchangedShape(t *testing.T) {
	got := leaderPingBody("/state/flotilla-xo-alive")
	if !strings.Contains(got, "Liveness check") || !strings.Contains(got, "flotilla-xo-alive") {
		t.Errorf("leader ping shape changed: %s", got)
	}
}