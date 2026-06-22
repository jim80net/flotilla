package surface

import (
	"errors"
	"testing"
	"time"
)

// confirmStub is a Driver whose Assess returns a SCRIPTED sequence (advancing one State per
// call, repeating the last once exhausted) and which records Submit calls. The Enter-only
// retry and escalate are recorded via the Confirm config, so ConfirmSubmit is exercised with
// zero tmux and zero wall-clock.
type confirmStub struct {
	assessSeq   []State
	idx         int
	submitCalls int
	submitErr   error
}

func (s *confirmStub) Name() string                { return "stub" }
func (s *confirmStub) Submit(string, string) error { s.submitCalls++; return s.submitErr }
func (s *confirmStub) Rotate(string) error         { return nil }
func (s *confirmStub) RotateStrategy() Strategy    { return SlashCommand }
func (s *confirmStub) Assess(string) State {
	if s.idx >= len(s.assessSeq) {
		return s.assessSeq[len(s.assessSeq)-1] // repeat the last scripted state
	}
	st := s.assessSeq[s.idx]
	s.idx++
	return st
}

// newConfirm builds a Confirm with recording collaborators; *enter counts Enter-only
// retries, sleep is a no-op (deterministic, instant). Submit is pure mechanism — escalation
// is the caller's policy, so there is nothing to record here.
func newConfirm(enter *int) Confirm {
	return Confirm{
		SendEnter: func(string) error { *enter++; return nil },
		Sleep:     func(time.Duration) {},
	}
}

func TestConfirmSubmitGate(t *testing.T) {
	// The idle-gate (resolution #2): deliver ONLY when idle. Working→ErrBusy; Shell→ErrCrashed;
	// Unknown/Awaiting/Errored→ErrTransient. In every non-idle case NO submit is attempted.
	cases := []struct {
		name    string
		state   State
		wantErr error
	}{
		{"working → ErrBusy, no submit", StateWorking, ErrBusy},
		{"shell → ErrCrashed, no submit", StateShell, ErrCrashed},
		{"unknown → ErrTransient, no submit", StateUnknown, ErrTransient},
		{"awaiting-approval → ErrTransient, no submit", StateAwaitingApproval, ErrTransient},
		{"errored → ErrTransient, no submit", StateErrored, ErrTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enter := 0
			d := &confirmStub{assessSeq: []State{tc.state}}
			err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			if d.submitCalls != 0 {
				t.Errorf("Submit calls = %d, want 0 (never fire into a non-idle composer)", d.submitCalls)
			}
			if enter != 0 {
				t.Errorf("SendEnter calls = %d, want 0 (no submit ⇒ no retry)", enter)
			}
		})
	}
}

func TestConfirmSubmitConfirmsOnWorkingEdge(t *testing.T) {
	// Case a: submit-into-idle succeeds — gate Idle, first poll observes Working. ⇒ nil;
	// Submit ×1; no Enter-only retry.
	enter := 0
	d := &confirmStub{assessSeq: []State{StateIdle, StateWorking}} // gate, then poll-1
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (confirmed)", err)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want 1", d.submitCalls)
	}
	if enter != 0 {
		t.Errorf("SendEnter calls = %d, want 0 (confirmed on first poll, no retry)", enter)
	}
}

func TestConfirmSubmitRetriesDroppedEnterThenConfirms(t *testing.T) {
	// Case b: the Enter was dropped — gate Idle, attempt-1's confirmPolls all Idle, then a
	// single Enter-only retry, then Working. ⇒ nil; Submit EXACTLY once (NO re-paste);
	// SendEnter ×1.
	seq := []State{StateIdle} // gate
	for i := 0; i < confirmPolls; i++ {
		seq = append(seq, StateIdle) // attempt-1 polls all idle (Enter dropped)
	}
	seq = append(seq, StateWorking) // attempt-2 poll-1: the retried Enter landed
	enter := 0
	d := &confirmStub{assessSeq: seq}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (confirmed after one retry)", err)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want EXACTLY 1 — a retry must re-send Enter only, never re-paste the body", d.submitCalls)
	}
	if enter != 1 {
		t.Errorf("SendEnter calls = %d, want 1 (one Enter-only retry)", enter)
	}
}

func TestConfirmSubmitNeverConfirmsBounded(t *testing.T) {
	// Case c: never confirms — gate Idle, then always Idle. ⇒ ErrUnconfirmed; SendEnter
	// EXACTLY maxSubmitAttempts-1 (bounded, no infinite loop). The caller escalates.
	enter := 0
	d := &confirmStub{assessSeq: []State{StateIdle}} // gate idle, then repeats Idle forever
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("err = %v, want ErrUnconfirmed", err)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want 1 (one paste, the rest are Enter-only)", d.submitCalls)
	}
	if enter != maxSubmitAttempts-1 {
		t.Errorf("SendEnter calls = %d, want %d (bounded retries)", enter, maxSubmitAttempts-1)
	}
}

// composerStub is a Driver that ALSO implements ComposerProbe, with scripted Assess and
// ComposerPending sequences (each advancing one entry per call, repeating the last once
// exhausted), for the composer-cleared confirmation path. The probe entries are (pending, ok).
type composerStub struct {
	assessSeq   []State
	aIdx        int
	pendingSeq  [][2]bool // {pending, ok}
	pIdx        int
	submitCalls int
}

func (s *composerStub) Name() string                { return "composer-stub" }
func (s *composerStub) Submit(string, string) error { s.submitCalls++; return nil }
func (s *composerStub) Rotate(string) error         { return nil }
func (s *composerStub) RotateStrategy() Strategy    { return SlashCommand }
func (s *composerStub) Assess(string) State {
	if s.aIdx >= len(s.assessSeq) {
		return s.assessSeq[len(s.assessSeq)-1]
	}
	st := s.assessSeq[s.aIdx]
	s.aIdx++
	return st
}
func (s *composerStub) ComposerPending(string) (bool, bool) {
	var e [2]bool
	if s.pIdx >= len(s.pendingSeq) {
		e = s.pendingSeq[len(s.pendingSeq)-1]
	} else {
		e = s.pendingSeq[s.pIdx]
		s.pIdx++
	}
	return e[0], e[1]
}

func TestConfirmSubmitConfirmsOnComposerClear(t *testing.T) {
	// THE regression for the false-negative bug: a heavy
	// pane whose Working spinner NEVER renders inside the window, but whose composer reads CLEARED
	// and STAYS cleared (the Enter was accepted). Spinner-only confirmation would have returned
	// ErrUnconfirmed and the relay would have raised a FALSE alarm; the stable composer-cleared
	// signal confirms it (within clearedConfirmPolls). ⇒ nil; Submit ×1; NO Enter-only retry.
	enter := 0
	d := &composerStub{
		assessSeq:  []State{StateIdle},       // gate Idle, then Idle forever (spinner lagging)
		pendingSeq: [][2]bool{{false, true}}, // composer cleared, and stays cleared (submitted)
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (confirmed by composer-clear despite no spinner)", err)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want 1", d.submitCalls)
	}
	if enter != 0 {
		t.Errorf("SendEnter calls = %d, want 0 (composer already cleared — no retry)", enter)
	}
}

func TestConfirmSubmitComposerDroppedEnterThenClears(t *testing.T) {
	// A dropped Enter on a composer-probe driver: the composer stays PENDING through attempt 1, an
	// Enter-only retry is sent, then the composer clears. ⇒ nil; Submit EXACTLY 1 (no re-paste);
	// SendEnter ×1. Spinner never appears (heavy pane) — the composer signal carries it.
	pending := make([][2]bool, 0)
	for i := 0; i < confirmPolls; i++ {
		pending = append(pending, [2]bool{true, true}) // attempt-1 polls: body still in composer
	}
	pending = append(pending, [2]bool{false, true}) // attempt-2 poll-1: cleared (retried Enter landed)
	enter := 0
	d := &composerStub{assessSeq: []State{StateIdle}, pendingSeq: pending}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (confirmed after one Enter retry)", err)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want EXACTLY 1 (retry is Enter-only, never re-paste)", d.submitCalls)
	}
	if enter != 1 {
		t.Errorf("SendEnter calls = %d, want 1", enter)
	}
}

func TestConfirmSubmitTransientEmptyThenPendingDoesNotFalseConfirm(t *testing.T) {
	// The paste-ingestion-race guard (clearedConfirmPolls): the FIRST poll reads the composer empty
	// because the bracketed paste has not been ingested YET — not because anything was submitted —
	// and the submitting Enter raced that ingestion and was dropped. A single empty read must NOT
	// confirm; the body renders as PENDING a poll later and resets the streak, so this is recovered
	// (Enter retry) rather than reported as a (false) success. Here the body stays pending after the
	// retries ⇒ ErrUnconfirmed (a genuine stuck message escalates), proving the single leading empty
	// did NOT short-circuit to success.
	seq := [][2]bool{{false, true}} // poll-1: empty (paste not ingested yet) — must not confirm alone
	for i := 0; i < 40; i++ {
		seq = append(seq, [2]bool{true, true}) // thereafter: body rendered + stuck (Enter was dropped)
	}
	enter := 0
	d := &composerStub{assessSeq: []State{StateIdle}, pendingSeq: seq}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("err = %v, want ErrUnconfirmed (a single transient-empty read must not false-confirm)", err)
	}
	if enter != maxSubmitAttempts-1 {
		t.Errorf("SendEnter calls = %d, want %d", enter, maxSubmitAttempts-1)
	}
}

func TestConfirmSubmitTransientEmptyThenStableClearRecovers(t *testing.T) {
	// Same leading transient-empty as above, but after the Enter-only retry the composer reaches a
	// STABLE cleared read (the retried Enter landed) ⇒ confirmed. Proves the guard recovers rather
	// than over-blocking a real submit: a transient empty + later stable clear still succeeds.
	seq := [][2]bool{{false, true}} // poll-1 empty (not ingested)
	for i := 1; i < confirmPolls; i++ {
		seq = append(seq, [2]bool{true, true}) // rest of attempt-1: pending (Enter was dropped)
	}
	seq = append(seq, [2]bool{false, true}, [2]bool{false, true}) // attempt-2: cleared, cleared (stable)
	enter := 0
	d := &composerStub{assessSeq: []State{StateIdle}, pendingSeq: seq}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (stable clear after the retry confirms)", err)
	}
	if d.submitCalls != 1 || enter != 1 {
		t.Errorf("Submit=%d enter=%d, want Submit=1 enter=1", d.submitCalls, enter)
	}
}

func TestConfirmSubmitEscalatesWhenComposerStaysPending(t *testing.T) {
	// The never-silent-drop invariant: a GENUINE non-delivery — the body provably REMAINS in the
	// composer (Enter never accepted) and the spinner never appears — must still escalate. ⇒
	// ErrUnconfirmed; SendEnter bounded at maxSubmitAttempts-1. This is the positive-evidence
	// failure the fix preserves while removing the false negatives.
	enter := 0
	d := &composerStub{
		assessSeq:  []State{StateIdle},      // idle forever (no spinner)
		pendingSeq: [][2]bool{{true, true}}, // composer always pending (body stuck)
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("err = %v, want ErrUnconfirmed (body never left the composer)", err)
	}
	if enter != maxSubmitAttempts-1 {
		t.Errorf("SendEnter calls = %d, want %d (bounded retries)", enter, maxSubmitAttempts-1)
	}
}

func TestConfirmSubmitCrashedMidConfirm(t *testing.T) {
	// The pane was idle at the gate, submit happened, then it dropped to a SHELL mid-confirm — the
	// agent crashed. Confirmation short-circuits to ErrCrashed instead of waiting out the window.
	enter := 0
	d := &composerStub{
		assessSeq:  []State{StateIdle, StateShell}, // gate Idle, then crashed
		pendingSeq: [][2]bool{{true, true}},
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrCrashed) {
		t.Fatalf("err = %v, want ErrCrashed (pane dropped to a shell mid-confirm)", err)
	}
}

func TestConfirmSubmitProbeUndeterminedFallsBackToSpinner(t *testing.T) {
	// When the composer probe cannot read the composer (ok=false — a capture glitch), confirmation
	// must NOT treat it as cleared; it falls back to the spinner. Here Working appears only in the
	// PATIENT grace phase (a genuinely slow start), and the fallback still confirms it. ⇒ nil.
	assess := []State{StateIdle} // gate
	// fast phase: maxSubmitAttempts*confirmPolls Idle polls (probe undetermined throughout)
	for i := 0; i < maxSubmitAttempts*confirmPolls; i++ {
		assess = append(assess, StateIdle)
	}
	assess = append(assess, StateWorking) // grace poll-1: the slow spinner finally renders
	enter := 0
	d := &composerStub{assessSeq: assess, pendingSeq: [][2]bool{{false, false}}} // probe always undetermined
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (spinner confirmed in the patient grace phase)", err)
	}
	if enter != maxSubmitAttempts-1 {
		t.Errorf("SendEnter calls = %d, want %d (fast-phase retries, none in grace)", enter, maxSubmitAttempts-1)
	}
}

func TestConfirmSubmitPasteFailureNoEnterRetry(t *testing.T) {
	// Case g (OCR-H2/L3): the initial Submit returns an error (the body never landed). ⇒
	// the wrapped error; SendEnter NEVER (no Enter-only retry on a paste that didn't land —
	// the idempotency invariant requires Submit==nil before any Enter-only retry).
	boom := errors.New("tmux load-buffer: lock busy")
	enter := 0
	d := &confirmStub{assessSeq: []State{StateIdle}, submitErr: boom}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the wrapped Submit error %v", err, boom)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want 1", d.submitCalls)
	}
	if enter != 0 {
		t.Errorf("SendEnter calls = %d, want 0 (never Enter-retry a paste that didn't land)", enter)
	}
}

// panelStub is a Driver that implements Assess + ComposerProbe + InputBlockProbe with scripted
// sequences (each advancing one entry per call, repeating the last once exhausted), for the
// input-block (#152) gate + pollConfirm-precedence tests. blockedSeq entries are (blocked, ok).
type panelStub struct {
	assessSeq   []State
	aIdx        int
	pendingSeq  [][2]bool // {pending, ok} for ComposerPending
	pIdx        int
	blockedSeq  [][2]bool // {blocked, ok} for InputBlocked
	bIdx        int
	submitCalls int
}

func (s *panelStub) Name() string                { return "panel-stub" }
func (s *panelStub) Submit(string, string) error { s.submitCalls++; return nil }
func (s *panelStub) Rotate(string) error         { return nil }
func (s *panelStub) RotateStrategy() Strategy    { return SlashCommand }
func (s *panelStub) Assess(string) State {
	if s.aIdx >= len(s.assessSeq) {
		return s.assessSeq[len(s.assessSeq)-1]
	}
	st := s.assessSeq[s.aIdx]
	s.aIdx++
	return st
}
func (s *panelStub) ComposerPending(string) (bool, bool) {
	e := s.pendingSeq[min(s.pIdx, len(s.pendingSeq)-1)]
	if s.pIdx < len(s.pendingSeq) {
		s.pIdx++
	}
	return e[0], e[1]
}
func (s *panelStub) InputBlocked(string) (bool, bool) {
	e := s.blockedSeq[min(s.bIdx, len(s.blockedSeq)-1)]
	if s.bIdx < len(s.blockedSeq) {
		s.bIdx++
	}
	return e[0], e[1]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestConfirmSubmitGateRefusesPanelBlocked(t *testing.T) {
	// #152: an Idle pane whose composer is input-blocked behind the agents panel must be REFUSED
	// before any paste — never lost in the panel, never stacked. ⇒ ErrPanelBlocked; Submit ×0.
	enter := 0
	d := &panelStub{
		assessSeq:  []State{StateIdle},      // gate passes the idle check
		blockedSeq: [][2]bool{{true, true}}, // ...but the panel has focus
		pendingSeq: [][2]bool{{false, true}},
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrPanelBlocked) {
		t.Fatalf("err = %v, want ErrPanelBlocked", err)
	}
	if d.submitCalls != 0 {
		t.Errorf("Submit calls = %d, want 0 (refuse before pasting into the panel)", d.submitCalls)
	}
	if enter != 0 {
		t.Errorf("SendEnter calls = %d, want 0 (no submit ⇒ no retry)", enter)
	}
}

func TestConfirmSubmitPanelMidConfirmNotFalseCleared(t *testing.T) {
	// SHIP-BLOCKER (trio A1): a panel that appears AFTER the gate, whose empty composer (above the
	// docked panel) reads CLEARED, must NOT false-confirm. pollConfirm consults InputBlocked BEFORE
	// ComposerPending, so it returns readPanelBlocked → ErrPanelBlocked, never nil.
	enter := 0
	d := &panelStub{
		assessSeq:  []State{StateIdle},                     // gate Idle, then Idle forever (no spinner)
		blockedSeq: [][2]bool{{false, true}, {true, true}}, // gate: not blocked; polls: blocked
		pendingSeq: [][2]bool{{false, true}},               // composer reads CLEARED (the empty one above the panel)
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrPanelBlocked) {
		t.Fatalf("err = %v, want ErrPanelBlocked (panel-before-composer precedence; never false-cleared)", err)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want 1 (the gate passed, one paste, then the panel appeared)", d.submitCalls)
	}
}

func TestConfirmSubmitStartedTurnThenPanelStillConfirms(t *testing.T) {
	// A genuinely started turn (Working) that ALSO spawned subagents (opening the panel) must still
	// CONFIRM: Working precedes the input-block check in pollConfirm, so a started turn is never
	// misclassified as blocked. ⇒ nil.
	enter := 0
	d := &panelStub{
		assessSeq:  []State{StateIdle, StateWorking},       // gate Idle, poll-1 Working (turn started)
		blockedSeq: [][2]bool{{false, true}, {true, true}}, // gate clear; if reached, blocked — but Working wins
		pendingSeq: [][2]bool{{false, true}},
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (Working precedes the panel check — started turn confirms)", err)
	}
}

func TestConfirmSubmitNoInputBlockProbeUnchanged(t *testing.T) {
	// A driver WITHOUT InputBlockProbe (composerStub) must behave exactly as before — the gate and
	// pollConfirm both fall back, no new path taken. A normal composer-cleared confirm still works.
	enter := 0
	d := &composerStub{assessSeq: []State{StateIdle}, pendingSeq: [][2]bool{{false, true}}}
	if err := newConfirm(&enter).Submit(d, "0:0.0", "hi"); err != nil {
		t.Fatalf("err = %v, want nil (no-probe driver confirms by composer-clear as before)", err)
	}
}
