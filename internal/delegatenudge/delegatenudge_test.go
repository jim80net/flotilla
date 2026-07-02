package delegatenudge

import (
	"strings"
	"testing"
)

const claudeSeat = "claude-code"

func TestCheckDelegatesTurn(t *testing.T) {
	text := "Dispatched the fix to @backend via flotilla send — they own the implementation now."
	if r := Check(text, claudeSeat); r.InlineBuild {
		t.Fatalf("delegation turn should not IC-flag; got signal=%q", r.Signal)
	}
}

func TestCheckICImplementTurn(t *testing.T) {
	text := "Implemented the rate-limit probe in internal/surface/ratelimit.go and go test passed green."
	if r := Check(text, claudeSeat); !r.InlineBuild {
		t.Fatal("hands-on implement+test turn should IC-flag")
	}
}

func TestCheckPRMergeTurn(t *testing.T) {
	text := "Opened PR #236 and merged after CI green. Ready for your review."
	if r := Check(text, claudeSeat); !r.InlineBuild {
		t.Fatal("PR ship turn should IC-flag on a coordinator")
	}
}

func TestCheckSynthesisOnlyCarveOut(t *testing.T) {
	text := "Executive brief: fleet status — 3 desks working, 1 idle. Nothing needed from you."
	if r := Check(text, claudeSeat); r.InlineBuild {
		t.Fatal("coordination-only synthesis should not IC-flag")
	}
}

func TestCheckLivenessCarveOut(t *testing.T) {
	text := "[flotilla change-detector] Liveness check — reply with a one-word ack."
	if r := Check(text, claudeSeat); r.InlineBuild {
		t.Fatal("liveness ack should not IC-flag")
	}
}

func TestTrackerThreshold(t *testing.T) {
	tr := NewTracker()
	ic := Result{InlineBuild: true, Signal: "test"}
	if tr.Record("xo", ic) {
		t.Fatal("first strike should not fire nudge")
	}
	if tr.Strikes("xo") != 1 {
		t.Fatalf("strikes = %d, want 1", tr.Strikes("xo"))
	}
	if !tr.Record("xo", ic) {
		t.Fatal("second strike should fire nudge")
	}
	if tr.Strikes("xo") != 0 {
		t.Fatalf("strikes after fire = %d, want 0", tr.Strikes("xo"))
	}
}

func TestTrackerNonICResetsStrikes(t *testing.T) {
	tr := NewTracker()
	ic := Result{InlineBuild: true, Signal: "test"}
	tr.Record("xo", ic)
	tr.Record("xo", Result{})
	if tr.Strikes("xo") != 0 {
		t.Fatalf("non-IC turn should reset strikes; got %d", tr.Strikes("xo"))
	}
}

func TestTrackerICDelegateICNeedsTwoConsecutive(t *testing.T) {
	tr := NewTracker()
	ic := Result{InlineBuild: true, Signal: "test"}
	tr.Record("xo", ic)
	if tr.Strikes("xo") != 1 {
		t.Fatalf("first IC strike = %d, want 1", tr.Strikes("xo"))
	}
	tr.Record("xo", Result{}) // delegation / non-IC resets
	if tr.Strikes("xo") != 0 {
		t.Fatalf("after delegate reset strikes = %d, want 0", tr.Strikes("xo"))
	}
	tr.Record("xo", ic)
	if tr.Strikes("xo") != 1 {
		t.Fatalf("second IC alone = %d, want 1 (not consecutive with first)", tr.Strikes("xo"))
	}
	if !tr.Record("xo", ic) {
		t.Fatal("two consecutive IC turns should fire nudge")
	}
}

func TestNudgePromptNamesAgent(t *testing.T) {
	p := NudgePrompt("alpha-xo")
	if p == "" || !strings.Contains(p, "alpha-xo") || !strings.Contains(p, "flotilla send") {
		t.Fatalf("nudge prompt missing agent or dispatch instruction: %q", p)
	}
	if !strings.Contains(p, "grok") {
		t.Fatalf("nudge prompt should mention grok execution desks: %q", p)
	}
}

func TestCheckSkipsGrokSurface(t *testing.T) {
	text := "Implemented the rate-limit probe and go test passed green."
	if r := Check(text, "grok"); r.InlineBuild {
		t.Fatal("grok workhorse surface should never IC-flag (harness allocation)")
	}
}
