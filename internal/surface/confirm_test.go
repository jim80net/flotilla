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
	// THE regression for the false-negative bug (docs/design-confirm-false-negative.md): a heavy
	// pane whose Working spinner NEVER renders inside the window, but whose composer CLEARS on the
	// first poll (the Enter was accepted). Spinner-only confirmation would have returned
	// ErrUnconfirmed and the relay would have raised a FALSE alarm; the composer-cleared signal
	// confirms immediately. ⇒ nil; Submit ×1; NO Enter-only retry.
	enter := 0
	d := &composerStub{
		assessSeq:  []State{StateIdle},       // gate Idle, then Idle forever (spinner lagging)
		pendingSeq: [][2]bool{{false, true}}, // first poll: composer cleared (submitted)
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
