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

func TestAdjutantBufferedNoteBody(t *testing.T) {
	got := adjutantBufferedNoteBody("xo", 2)
	for _, want := range []string{"Buffered 2", "xo", "next seam"} {
		if !strings.Contains(got, want) {
			t.Errorf("buffered note missing %q\nfull: %s", want, got)
		}
	}
}

func TestLeaderPingBodyUnchangedShape(t *testing.T) {
	got := leaderPingBody("/state/flotilla-xo-alive")
	if !strings.Contains(got, "Liveness check") || !strings.Contains(got, "flotilla-xo-alive") {
		t.Errorf("leader ping shape changed: %s", got)
	}
}
