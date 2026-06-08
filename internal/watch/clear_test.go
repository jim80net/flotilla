package watch

import (
	"errors"
	"testing"
	"time"
)

// clearStub builds a ClearController with controllable collaborators and records
// what happened. AssertWindow defaults to 0 (exactly one health sample) unless a
// test overrides it.
type clearStub struct {
	awaiting         bool
	resolveErr       error
	pane             string
	captures         []string // returned in order per Capture call; last value sticks
	captureErr       error
	beforeCaptureErr error // errors ONLY the first (pre-clear) Capture call
	isShell          bool
	clearErr         error
	clearCalled      int
	alerts           []string
}

func (s *clearStub) controller() *ClearController {
	capIdx := 0
	capCalls := 0
	return &ClearController{
		AwaitingExists: func() bool { return s.awaiting },
		Resolve:        func() (string, error) { return s.pane, s.resolveErr },
		Capture: func(string) (string, error) {
			capCalls++
			if capCalls == 1 && s.beforeCaptureErr != nil {
				return "", s.beforeCaptureErr
			}
			if s.captureErr != nil {
				return "", s.captureErr
			}
			if len(s.captures) == 0 {
				return "", nil
			}
			v := s.captures[capIdx]
			if capIdx < len(s.captures)-1 {
				capIdx++
			}
			return v, nil
		},
		PaneIsShell: func(string) bool { return s.isShell },
		Clear:       func(string) error { s.clearCalled++; return s.clearErr },
		Alert:       func(msg string) { s.alerts = append(s.alerts, msg) },
		// AssertWindow 0 ⇒ one sample; tests needing the poll set it explicitly.
	}
}

func TestClearVetoSkipsClear(t *testing.T) {
	s := &clearStub{awaiting: true, pane: "0:0.0"}
	if got := s.controller().Decide("xo"); got != ProceedNoClear {
		t.Errorf("veto present: Decide = %v, want ProceedNoClear", got)
	}
	if s.clearCalled != 0 {
		t.Errorf("veto present: /clear was injected %d times, want 0 (outstanding question must not be wiped)", s.clearCalled)
	}
}

func TestClearResolveFailureNoClearNoAlert(t *testing.T) {
	s := &clearStub{resolveErr: errors.New("no pane"), pane: ""}
	if got := s.controller().Decide("xo"); got != ProceedNoClear {
		t.Errorf("resolve fail: Decide = %v, want ProceedNoClear", got)
	}
	if s.clearCalled != 0 {
		t.Error("resolve fail: must not inject /clear")
	}
	if len(s.alerts) != 0 {
		t.Errorf("resolve fail: must not alert (gate/watchdog owns unresolvable panes); got %v", s.alerts)
	}
}

func TestClearRCSurvivesIsCleared(t *testing.T) {
	// RC active before AND after the clear → healthy → ProceedCleared.
	s := &clearStub{pane: "0:0.0", captures: []string{"… Remote Control active …"}}
	if got := s.controller().Decide("xo"); got != ProceedCleared {
		t.Errorf("RC survived: Decide = %v, want ProceedCleared", got)
	}
	if s.clearCalled != 1 {
		t.Errorf("RC survived: /clear injected %d times, want 1", s.clearCalled)
	}
	if len(s.alerts) != 0 {
		t.Errorf("RC survived: must not alert; got %v", s.alerts)
	}
}

func TestClearRCDroppedAlertsAndSkips(t *testing.T) {
	// RC active before, ABSENT after (single sample, window 0) → SkipPrompt + alert.
	s := &clearStub{pane: "0:0.0", captures: []string{"Remote Control active", "no rc here"}}
	if got := s.controller().Decide("xo"); got != SkipPrompt {
		t.Errorf("RC dropped: Decide = %v, want SkipPrompt (never drive a broken XO)", got)
	}
	if len(s.alerts) == 0 {
		t.Error("RC dropped: expected a loud alert")
	}
}

func TestClearNoRCDeploymentSkipsRCCheck(t *testing.T) {
	// RC was NOT active before (deployment doesn't use RC) → RC sub-check skipped;
	// a live (non-shell) pane after the clear ⇒ ProceedCleared, no alert.
	s := &clearStub{pane: "0:0.0", captures: []string{"no remote control anywhere"}}
	if got := s.controller().Decide("xo"); got != ProceedCleared {
		t.Errorf("no-RC deployment: Decide = %v, want ProceedCleared", got)
	}
	if len(s.alerts) != 0 {
		t.Errorf("no-RC deployment: must not alert on a missing RC string it never had; got %v", s.alerts)
	}
}

func TestClearPaneDroppedToShellAlertsAndSkips(t *testing.T) {
	// Even without RC, a pane that fell back to a shell after the clear is broken.
	s := &clearStub{pane: "0:0.0", captures: []string{"no rc"}, isShell: true}
	if got := s.controller().Decide("xo"); got != SkipPrompt {
		t.Errorf("pane→shell: Decide = %v, want SkipPrompt", got)
	}
	if len(s.alerts) == 0 {
		t.Error("pane→shell: expected a loud alert")
	}
}

func TestClearInjectionErrorFallsBackToNoClear(t *testing.T) {
	// /clear failed to inject → don't claim cleared, don't skip the prompt.
	s := &clearStub{pane: "0:0.0", clearErr: errors.New("tmux boom"), captures: []string{"Remote Control active"}}
	if got := s.controller().Decide("xo"); got != ProceedNoClear {
		t.Errorf("clear inject error: Decide = %v, want ProceedNoClear", got)
	}
	if len(s.alerts) != 0 {
		t.Errorf("clear inject error: must not alert; got %v", s.alerts)
	}
}

func TestClearBeforeCaptureErrorSkipsClear(t *testing.T) {
	// If the pre-clear capture fails we cannot honestly assert post-clear health
	// (a real RC drop would masquerade as "RC was never active"), so we must NOT
	// clear this tick — deliver the prompt in the existing context, no alert.
	s := &clearStub{pane: "0:0.0", beforeCaptureErr: errors.New("capture-pane boom"), captures: []string{"Remote Control active"}}
	if got := s.controller().Decide("xo"); got != ProceedNoClear {
		t.Errorf("pre-clear capture error: Decide = %v, want ProceedNoClear (cannot assert health → don't clear)", got)
	}
	if s.clearCalled != 0 {
		t.Errorf("pre-clear capture error: /clear injected %d times, want 0", s.clearCalled)
	}
	if len(s.alerts) != 0 {
		t.Errorf("pre-clear capture error: must not alert (nothing was cleared); got %v", s.alerts)
	}
}

func TestClearAssertionPollsThroughRepaint(t *testing.T) {
	// The post-clear capture must POLL, not snapshot: RC absent on the first sample
	// (TUI mid-repaint), present on the second → healthy, no false alert.
	s := &clearStub{pane: "0:0.0", captures: []string{"Remote Control active", "repainting…", "Remote Control active"}}
	cc := s.controller()
	cc.AssertWindow = 200 * time.Millisecond
	cc.AssertPoll = 0 // no real delay between polls
	if got := cc.Decide("xo"); got != ProceedCleared {
		t.Errorf("poll-through-repaint: Decide = %v, want ProceedCleared (a single snapshot would false-fail)", got)
	}
	if len(s.alerts) != 0 {
		t.Errorf("poll-through-repaint: must not alert once RC repaints within the window; got %v", s.alerts)
	}
}
