package main

import (
	"os"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestAdjutantEvaluationTickBodyThreeStepDuty(t *testing.T) {
	const leader = "alpha-xo"
	const ack = "/state/flotilla-alpha-xo-alive"
	const buf = "/state/flotilla-alpha-xo-buffer.json"
	const charter = "/state/flotilla-alpha-xo-adjutant-charter.md"
	got := adjutantEvaluationTickBody(leader, ack, buf, charter)
	// #510: evaluation ticks must require leader usage-limit recognition + loud escalate.
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
		"usage-limit",
		"ESCALATE LOUDLY",
		"3. ACT BY TIER",
		buf,
		"idle-holding",
		"Your charter at " + charter,
		"consult it before composing any brief",
		"Fleet interaction intelligence",
		"brainstem",
		"rate-limit exhaustion",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("evaluation tick missing %q\nfull: %s", want, got)
		}
	}
}

func TestAdjutantBufferedNoteBody(t *testing.T) {
	const charter = "/state/flotilla-xo-adjutant-charter.md"
	got := adjutantBufferedNoteBody("xo", 2, charter)
	for _, want := range []string{
		"Buffered 2", "xo", "next seam", "evaluation ticks",
		"Your charter at " + charter,
		"consult it before composing any brief",
		"Fleet interaction intelligence",
	} {
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

func TestAdjutantBufferContract(t *testing.T) {
	got := adjutantBufferContract("alpha-xo")
	for _, want := range []string{
		"Fleet interaction intelligence", "brainstem", "Coalesce", "Disaggregate",
		"Desk stream", "Leader stream", "alpha-xo", "Working/Idle",
		// #524: loop_posture, not pane idle alone
		"loop_posture", "parked", "drifted", "awaiting-authority", "not pane idle alone",
		"may contain the missing decision", "keep system interrupts buffered",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buffer contract missing %q\nfull: %s", want, got)
		}
	}
	if strings.Contains(got, "not while awaiting-authority") {
		t.Fatalf("buffer contract retains stale authority-wait suppression: %s", got)
	}
}

func TestAdjutantCharterPairingBody(t *testing.T) {
	got := adjutantCharterPairingBody("alpha-xo", "alpha-adj", "/state/flotilla-alpha-xo-adjutant-charter.md", "/state/flotilla-alpha-xo-alive")
	for _, want := range []string{
		"First-presentation charter",
		"alpha-xo",
		"flotilla-alpha-xo-adjutant-charter.md",
		"flotilla-alpha-xo-alive",
		"Required minimum",
		"Fleet interaction intelligence",
		"gated until this charter exists",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("charter pairing missing %q\nfull: %s", want, got)
		}
	}
}

func TestLeaderCharterPairingBody(t *testing.T) {
	const ack = "/state/legacy/flotilla-xo-alive"
	got := leaderCharterPairingBody("alpha-xo", "alpha-adj", "/state/flotilla-alpha-xo-adjutant-charter.md", ack)
	for _, want := range []string{"alpha-xo", "alpha-adj", "adjutant-charter.md", "liveness ack", ack} {
		if !strings.Contains(got, want) {
			t.Errorf("leader charter pairing missing %q\nfull: %s", want, got)
		}
	}
}

func TestLayerCharterMissing(t *testing.T) {
	dir := t.TempDir()
	missing := dir + "/flotilla-xo-adjutant-charter.md"
	if !layerCharterMissing(missing) {
		t.Fatal("expected missing charter")
	}
	if err := os.WriteFile(missing, []byte("# charter"), 0o600); err != nil {
		t.Fatal(err)
	}
	if layerCharterMissing(missing) {
		t.Fatal("expected present charter")
	}
}

func TestEnqueueAdjutantCharterPairingSkipsWhenPresent(t *testing.T) {
	dir := t.TempDir()
	charter := roster.LayerCharterPath(dir, "xo")
	if err := os.WriteFile(charter, []byte("# ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	var jobs []watch.Job
	enqueueAdjutantCharterPairing("xo-adj", "xo", dir, "/state/flotilla-xo-alive", func(j watch.Job) {
		jobs = append(jobs, j)
	})
	if len(jobs) != 0 {
		t.Fatalf("expected no jobs when charter present, got %d", len(jobs))
	}
}

func TestEnqueueAdjutantCharterPairingWakesPair(t *testing.T) {
	dir := t.TempDir()
	var jobs []watch.Job
	enqueueAdjutantCharterPairing("xo-adj", "xo", dir, "/state/flotilla-xo-alive", func(j watch.Job) {
		jobs = append(jobs, j)
	})
	if len(jobs) != 2 {
		t.Fatalf("expected adjutant+leader pairing wakes, got %d", len(jobs))
	}
	if jobs[0].Agent != "xo-adj" || jobs[1].Agent != "xo" {
		t.Fatalf("unexpected agents: %+v", jobs)
	}
	for _, j := range jobs {
		if j.Kind != watch.KindDetector {
			t.Fatalf("unexpected kind: %+v", j)
		}
		if !strings.Contains(j.Message, "charter") {
			t.Fatalf("expected charter pairing prompt, got: %s", j.Message)
		}
	}
}
