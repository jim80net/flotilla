package main

import (
	"strings"
	"testing"
)

func TestAdjutantEvaluationTickBodyThreeStepDuty(t *testing.T) {
	const leader = "alpha-xo"
	const ack = "/state/flotilla-alpha-xo-alive"
	const buf = "/state/flotilla-alpha-xo-buffer.json"
	got := adjutantEvaluationTickBody(leader, ack, buf)
	for _, want := range []string{
		"Evaluation tick",
		leader,
		"timeout signal",
		"not a dead-man ack",
		"1. ACK",
		"touch " + ack,
		"2. EVALUATE",
		"all-quiet",
		"work-found",
		"3. ACT BY TIER",
		buf,
		"idle-holding",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("evaluation tick missing %q\nfull: %s", want, got)
		}
	}
}

func TestAdjutantBufferedNoteBody(t *testing.T) {
	got := adjutantBufferedNoteBody("xo", 2)
	for _, want := range []string{"Buffered 2", "xo", "next seam", "evaluation ticks"} {
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
