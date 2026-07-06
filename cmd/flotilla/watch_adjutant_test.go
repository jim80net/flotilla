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

func TestLeaderPingBodyExactLegacyString(t *testing.T) {
	const want = "[flotilla change-detector] Liveness check — reply with a one-line ack only; take no other action." +
		"\n(To ack you are alive, run: touch /state/flotilla-xo-alive)"
	if got := leaderPingBody("/state/flotilla-xo-alive"); got != want {
		t.Errorf("leader ping changed\n got: %q\nwant: %q", got, want)
	}
}
