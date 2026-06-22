package surface

import (
	"errors"
	"testing"
	"time"
)

// confirmStub is a Driver whose Assess returns a SCRIPTED sequence (advancing one State per
// call, repeating the last once exhausted) and which records Submit calls. It implements NO
// composer probe, so it exercises the spinner-only path. The Enter-only retry and escalate are
// recorded via the Confirm config, so ConfirmSubmit is exercised with zero tmux and zero clock.
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
	// The idle-gate: deliver ONLY when idle. Working→ErrBusy; Shell→ErrCrashed;
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
	// submit-into-idle succeeds — gate Idle, first poll observes Working. ⇒ nil; Submit ×1; no retry.
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
	// The Enter was dropped — gate Idle, attempt-1's confirmPolls all Idle, then a single Enter-only
	// retry, then Working. ⇒ nil; Submit EXACTLY once (NO re-paste); SendEnter ×1.
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

func TestConfirmSubmitNoProbeNeverConfirmsBounded(t *testing.T) {
	// A no-probe driver (confirmStub) that never shows the spinner — gate Idle, then always Idle. ⇒
	// ErrUnconfirmed (ambiguous — no composer authority); SendEnter EXACTLY maxSubmitAttempts-1.
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

// stateStub is a Driver implementing ComposerStateProbe, scripting Assess + ComposerState sequences
// (each advancing one entry per call, repeating the last once exhausted). The FIRST ComposerState
// entry is consumed by the pre-paste gate; subsequent entries are poll reads.
type stateStub struct {
	assessSeq   []State
	aIdx        int
	stateSeq    []ComposerDisposition
	sIdx        int
	submitCalls int
}

func (s *stateStub) Name() string                { return "state-stub" }
func (s *stateStub) Submit(string, string) error { s.submitCalls++; return nil }
func (s *stateStub) Rotate(string) error         { return nil }
func (s *stateStub) RotateStrategy() Strategy    { return SlashCommand }
func (s *stateStub) Assess(string) State {
	if s.aIdx >= len(s.assessSeq) {
		return s.assessSeq[len(s.assessSeq)-1]
	}
	st := s.assessSeq[s.aIdx]
	s.aIdx++
	return st
}
func (s *stateStub) ComposerState(string) ComposerDisposition {
	if len(s.stateSeq) == 0 {
		return ComposerUndetermined
	}
	if s.sIdx >= len(s.stateSeq) {
		return s.stateSeq[len(s.stateSeq)-1]
	}
	d := s.stateSeq[s.sIdx]
	s.sIdx++
	return d
}

// dispSeq builds a ComposerState sequence: a gate entry (cleared, so the gate proceeds) followed by
// n poll entries of disp.
func dispSeq(gate ComposerDisposition, poll ComposerDisposition, n int) []ComposerDisposition {
	out := []ComposerDisposition{gate}
	for i := 0; i < n; i++ {
		out = append(out, poll)
	}
	return out
}

func TestConfirmSubmitConfirmsOnComposerClear(t *testing.T) {
	// THE false-negative regression: a heavy pane whose Working spinner NEVER renders, but whose
	// composer reads CLEARED and STAYS cleared (the Enter was accepted). ⇒ nil; Submit ×1; no retry.
	enter := 0
	d := &stateStub{
		assessSeq: []State{StateIdle},                                      // gate Idle, then Idle forever
		stateSeq:  []ComposerDisposition{ComposerCleared, ComposerCleared}, // gate cleared; polls cleared
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (confirmed by composer-clear despite no spinner)", err)
	}
	if d.submitCalls != 1 || enter != 0 {
		t.Errorf("Submit=%d enter=%d, want Submit=1 enter=0", d.submitCalls, enter)
	}
}

func TestConfirmSubmitComposerDroppedEnterThenClears(t *testing.T) {
	// A dropped Enter: the composer stays PENDING through attempt 1, an Enter-only retry, then clears.
	// ⇒ nil; Submit EXACTLY 1 (no re-paste); SendEnter ×1.
	seq := dispSeq(ComposerCleared, ComposerPending, confirmPolls) // gate cleared; attempt-1 polls pending
	seq = append(seq, ComposerCleared)                             // attempt-2 poll-1: cleared (retry landed)
	enter := 0
	d := &stateStub{assessSeq: []State{StateIdle}, stateSeq: seq}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (confirmed after one Enter retry)", err)
	}
	if d.submitCalls != 1 || enter != 1 {
		t.Errorf("Submit=%d enter=%d, want Submit=1 enter=1 (retry is Enter-only)", d.submitCalls, enter)
	}
}

func TestConfirmSubmitTransientEmptyThenPendingIsBlocked(t *testing.T) {
	// The paste-ingestion-race guard (clearedConfirmPolls): a single leading empty read must NOT
	// confirm; the body renders PENDING a poll later and resets the streak. Here it stays pending
	// after the retries ⇒ BLOCKED (the body provably remained — the authority), proving the single
	// transient-empty did not short-circuit to success.
	seq := []ComposerDisposition{ComposerCleared, ComposerCleared} // gate cleared; poll-1 empty (not ingested)
	for i := 0; i < 40; i++ {
		seq = append(seq, ComposerPending) // thereafter: body rendered + stuck (Enter dropped)
	}
	enter := 0
	d := &stateStub{assessSeq: []State{StateIdle}, stateSeq: seq}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrPanelBlocked) {
		t.Fatalf("err = %v, want ErrPanelBlocked (a single transient-empty must not false-confirm; stays pending → blocked)", err)
	}
	if enter != maxSubmitAttempts-1 {
		t.Errorf("SendEnter calls = %d, want %d", enter, maxSubmitAttempts-1)
	}
}

func TestConfirmSubmitTransientEmptyThenStableClearRecovers(t *testing.T) {
	// Leading transient-empty, but after the Enter-only retry the composer reaches a STABLE cleared
	// read ⇒ confirmed. Proves the guard recovers a real submit rather than over-blocking.
	seq := []ComposerDisposition{ComposerCleared, ComposerCleared} // gate cleared; poll-1 empty (not ingested)
	for i := 1; i < confirmPolls; i++ {
		seq = append(seq, ComposerPending) // rest of attempt-1: pending (Enter dropped)
	}
	seq = append(seq, ComposerCleared, ComposerCleared) // attempt-2: cleared, cleared (stable)
	enter := 0
	d := &stateStub{assessSeq: []State{StateIdle}, stateSeq: seq}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (stable clear after the retry confirms)", err)
	}
	if d.submitCalls != 1 || enter != 1 {
		t.Errorf("Submit=%d enter=%d, want Submit=1 enter=1", d.submitCalls, enter)
	}
}

func TestConfirmSubmitPendingAfterRetriesIsBlocked(t *testing.T) {
	// THE authority (the family-office case): the body provably REMAINS in the composer through the
	// retries + grace (the submit never landed) ⇒ BLOCKED, regardless of cursor/geometry.
	enter := 0
	d := &stateStub{
		assessSeq: []State{StateIdle},                                      // idle forever (no spinner)
		stateSeq:  []ComposerDisposition{ComposerCleared, ComposerPending}, // gate cleared; polls pending forever
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrPanelBlocked) {
		t.Fatalf("err = %v, want ErrPanelBlocked (composer stayed pending — the submit never landed)", err)
	}
	if enter != maxSubmitAttempts-1 {
		t.Errorf("SendEnter calls = %d, want %d (bounded retries)", enter, maxSubmitAttempts-1)
	}
}

func TestConfirmSubmitQueuedIsSoftSuccess(t *testing.T) {
	// The hydra-ops case: after submitting, the composer enters the QUEUED state ("Press up to edit
	// queued messages") — the message is queued behind a modal/turn and will deliver. ⇒ nil (a
	// soft-success), NOT a failure or an alarm.
	enter := 0
	d := &stateStub{
		assessSeq: []State{StateIdle},
		stateSeq:  []ComposerDisposition{ComposerCleared, ComposerQueued}, // gate cleared; poll-1 queued
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (queued is a soft-success — the message will deliver)", err)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want 1", d.submitCalls)
	}
}

func TestConfirmSubmitCrashedMidConfirm(t *testing.T) {
	// Idle at the gate, submit, then the pane dropped to a SHELL mid-confirm — the agent crashed. ⇒
	// ErrCrashed (short-circuits, no waiting out the window).
	enter := 0
	d := &stateStub{
		assessSeq: []State{StateIdle, StateShell},                          // gate Idle, then crashed
		stateSeq:  []ComposerDisposition{ComposerCleared, ComposerPending}, // gate cleared (proceed)
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrCrashed) {
		t.Fatalf("err = %v, want ErrCrashed (pane dropped to a shell mid-confirm)", err)
	}
}

func TestConfirmSubmitUndeterminedFallsBackToSpinner(t *testing.T) {
	// When ComposerState is Undetermined (capture/cursor glitch), confirmation must NOT treat it as
	// cleared; it falls back to the spinner. Here Working appears only in the PATIENT grace phase, and
	// the fallback still confirms it. ⇒ nil.
	assess := []State{StateIdle} // gate
	for i := 0; i < maxSubmitAttempts*confirmPolls; i++ {
		assess = append(assess, StateIdle) // fast phase: idle (probe undetermined throughout)
	}
	assess = append(assess, StateWorking) // grace poll-1: the slow spinner finally renders
	enter := 0
	// gate Cleared (proceed), then Undetermined forever.
	d := &stateStub{assessSeq: assess, stateSeq: []ComposerDisposition{ComposerCleared, ComposerUndetermined}}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (spinner confirmed in the patient grace phase)", err)
	}
	if enter != maxSubmitAttempts-1 {
		t.Errorf("SendEnter calls = %d, want %d (fast-phase retries, none in grace)", enter, maxSubmitAttempts-1)
	}
}

func TestConfirmSubmitPasteFailureNoEnterRetry(t *testing.T) {
	// The initial Submit returns an error (the body never landed). ⇒ the wrapped error; SendEnter
	// NEVER (no Enter-only retry on a paste that didn't land — the idempotency invariant).
	boom := errors.New("tmux load-buffer: lock busy")
	enter := 0
	d := &confirmStub{assessSeq: []State{StateIdle}, submitErr: boom}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the wrapped Submit error %v", err, boom)
	}
	if d.submitCalls != 1 || enter != 0 {
		t.Errorf("Submit=%d enter=%d, want Submit=1 enter=0", d.submitCalls, enter)
	}
}

func TestConfirmSubmitGateRefusesSubComposer(t *testing.T) {
	// #152 carve-out: an Idle pane whose CURSOR is on a per-agent message sub-composer must be REFUSED
	// before any paste — a paste there mis-delivers to the wrong recipient. ⇒ ErrPanelBlocked; Submit ×0.
	enter := 0
	d := &stateStub{
		assessSeq: []State{StateIdle},                      // gate passes the idle check
		stateSeq:  []ComposerDisposition{ComposerSubAgent}, // ...but the cursor is on the sub-composer
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrPanelBlocked) {
		t.Fatalf("err = %v, want ErrPanelBlocked", err)
	}
	if d.submitCalls != 0 || enter != 0 {
		t.Errorf("Submit=%d enter=%d, want Submit=0 enter=0 (refuse before pasting — no mis-deliver)", d.submitCalls, enter)
	}
}

func TestConfirmSubmitGateRefusesListNav(t *testing.T) {
	// The other carve-out: the cursor on an agent-list row is refused pre-paste. ⇒ ErrPanelBlocked; Submit ×0.
	enter := 0
	d := &stateStub{assessSeq: []State{StateIdle}, stateSeq: []ComposerDisposition{ComposerListNav}}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrPanelBlocked) || d.submitCalls != 0 {
		t.Errorf("err=%v Submit=%d, want ErrPanelBlocked + Submit=0", err, d.submitCalls)
	}
}

func TestConfirmSubmitSubComposerMidConfirmIsBlocked(t *testing.T) {
	// A sub-composer/list-nav that appears AFTER the gate (the gate read it cleared, so we pasted) →
	// readPanelBlocked → ErrPanelBlocked, never a false-confirm. Submit ×1 (the gate passed).
	enter := 0
	d := &stateStub{
		assessSeq: []State{StateIdle},                                       // gate Idle, then Idle forever
		stateSeq:  []ComposerDisposition{ComposerCleared, ComposerSubAgent}, // gate cleared; poll-1 sub-composer
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if !errors.Is(err, ErrPanelBlocked) {
		t.Fatalf("err = %v, want ErrPanelBlocked (sub-composer appeared mid-confirm)", err)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want 1 (gate passed, one paste, then the overlay appeared)", d.submitCalls)
	}
}

func TestConfirmSubmitListNavMidConfirmIsBlocked(t *testing.T) {
	// Symmetric to the sub-composer case: a list-nav overlay appearing mid-confirm → readPanelBlocked
	// → ErrPanelBlocked (never a false-confirm). Submit ×1 (the gate read it cleared).
	enter := 0
	d := &stateStub{
		assessSeq: []State{StateIdle},
		stateSeq:  []ComposerDisposition{ComposerCleared, ComposerListNav}, // gate cleared; poll-1 list-nav
	}
	if err := newConfirm(&enter).Submit(d, "0:0.0", "hi"); !errors.Is(err, ErrPanelBlocked) {
		t.Fatalf("err = %v, want ErrPanelBlocked (list-nav appeared mid-confirm)", err)
	}
	if d.submitCalls != 1 {
		t.Errorf("Submit calls = %d, want 1", d.submitCalls)
	}
}

func TestConfirmSubmitStartedTurnThenOverlayStillConfirms(t *testing.T) {
	// A genuinely started turn (Working) that ALSO opened an overlay must still CONFIRM: Working
	// precedes the ComposerState check in pollConfirm, so a started turn is never misclassified. ⇒ nil.
	enter := 0
	d := &stateStub{
		assessSeq: []State{StateIdle, StateWorking},                         // gate Idle, poll-1 Working
		stateSeq:  []ComposerDisposition{ComposerCleared, ComposerSubAgent}, // gate clear; if reached, sub — but Working wins
	}
	err := newConfirm(&enter).Submit(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (Working precedes the ComposerState check)", err)
	}
}

func TestConfirmSubmitNoStateProbeRestsOnSpinner(t *testing.T) {
	// A driver WITHOUT ComposerStateProbe (confirmStub) rests entirely on the spinner: gate proceeds
	// (no carve-out), poll-1 Working ⇒ confirmed.
	enter := 0
	d := &confirmStub{assessSeq: []State{StateIdle, StateWorking}}
	if err := newConfirm(&enter).Submit(d, "0:0.0", "hi"); err != nil {
		t.Fatalf("err = %v, want nil (no-probe driver confirms by the spinner as before)", err)
	}
}

// healStub (#156 self-heal): a driver whose ComposerState is an overlay until `recoverAt` Ctrl-C
// presses have been sent (tracked via the shared *ctrlc counter the Confirm.SendCtrlC closure
// increments), then Cleared. Assess follows assessSeq (default Idle). Records Submit calls.
type healStub struct {
	assessSeq []State
	aIdx      int
	overlay   ComposerDisposition   // SubAgent or ListNav (used when stateSeq is empty)
	recoverAt int                   // ComposerState → Cleared once *ctrlc >= recoverAt (stateSeq empty)
	stateSeq  []ComposerDisposition // optional: the disposition indexed by the Ctrl-C count (a CHANGING stack)
	ctrlc     *int
	submits   int
}

func (s *healStub) Name() string                { return "heal-stub" }
func (s *healStub) Submit(string, string) error { s.submits++; return nil }
func (s *healStub) Rotate(string) error         { return nil }
func (s *healStub) RotateStrategy() Strategy    { return SlashCommand }
func (s *healStub) Assess(string) State {
	if len(s.assessSeq) == 0 {
		return StateIdle
	}
	if s.aIdx >= len(s.assessSeq) {
		return s.assessSeq[len(s.assessSeq)-1]
	}
	st := s.assessSeq[s.aIdx]
	s.aIdx++
	return st
}
func (s *healStub) ComposerState(string) ComposerDisposition {
	if len(s.stateSeq) > 0 { // sequence mode: the disposition indexed by how many Ctrl-C have been sent
		i := *s.ctrlc
		if i >= len(s.stateSeq) {
			i = len(s.stateSeq) - 1
		}
		return s.stateSeq[i]
	}
	if *s.ctrlc >= s.recoverAt {
		return ComposerCleared
	}
	return s.overlay
}

// healConfirm builds a Confirm with a SendCtrlC recorder (*ctrlc) — self-heal ENABLED.
func healConfirm(enter, ctrlc *int) Confirm {
	c := newConfirm(enter)
	c.SendCtrlC = func(string) error { *ctrlc++; return nil }
	return c
}

func TestSubmitWithSelfHealDisabledIsPlainSubmit(t *testing.T) {
	// SendCtrlC nil (the default-off kill-switch) → SubmitWithSelfHeal == Submit: zero Ctrl-C even on
	// an overlay; falls through to the normal blocked path (ErrPanelBlocked).
	enter, ctrlc := 0, 0
	c := newConfirm(&enter) // SendCtrlC stays nil
	d := &healStub{overlay: ComposerSubAgent, recoverAt: 99, ctrlc: &ctrlc}
	err := c.SubmitWithSelfHeal(d, "0:0.0", "hi")
	if ctrlc != 0 {
		t.Errorf("Ctrl-C sent = %d, want 0 (self-heal disabled)", ctrlc)
	}
	if !errors.Is(err, ErrPanelBlocked) {
		t.Errorf("err = %v, want ErrPanelBlocked (no heal → gate refuses the overlay)", err)
	}
}

func TestSubmitWithSelfHealRecoversThenSubmitsOnce(t *testing.T) {
	// A pre-paste overlay on an idle pane: self-heal sends Ctrl-C until recovered, then Submit is
	// called EXACTLY ONCE into the clean composer → delivered. recoverAt=1 → one Ctrl-C.
	enter, ctrlc := 0, 0
	d := &healStub{overlay: ComposerSubAgent, recoverAt: 1, ctrlc: &ctrlc}
	err := healConfirm(&enter, &ctrlc).SubmitWithSelfHeal(d, "0:0.0", "hi")
	if err != nil {
		t.Fatalf("err = %v, want nil (healed → submitted)", err)
	}
	if ctrlc != 1 {
		t.Errorf("Ctrl-C sent = %d, want 1 (recovered after one press, then STOP — no Ctrl-C into the recovered composer)", ctrlc)
	}
	if d.submits != 1 {
		t.Errorf("Submit calls = %d, want EXACTLY 1 (no re-attempt — double-deliver impossible)", d.submits)
	}
}

func TestSubmitWithSelfHealNonOverlayDoesNotHeal(t *testing.T) {
	// A reachable (Cleared) composer → no self-heal, Submit once. (The exit guard: never Ctrl-C a
	// non-overlay composer.)
	enter, ctrlc := 0, 0
	d := &healStub{overlay: ComposerCleared, recoverAt: 0, ctrlc: &ctrlc} // already Cleared
	if err := healConfirm(&enter, &ctrlc).SubmitWithSelfHeal(d, "0:0.0", "hi"); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ctrlc != 0 {
		t.Errorf("Ctrl-C sent = %d, want 0 (composer reachable — never Ctrl-C it)", ctrlc)
	}
}

func TestSelfHealIdleGateNeverCtrlCsAWorkingPane(t *testing.T) {
	// C2: a pane that flips to Working mid-heal must NOT be Ctrl-C'd again (a Ctrl-C into a running
	// turn interrupts it). assessSeq Idle (press once) → Working (abort).
	enter, ctrlc := 0, 0
	c := healConfirm(&enter, &ctrlc)
	d := &healStub{assessSeq: []State{StateIdle, StateWorking}, overlay: ComposerSubAgent, recoverAt: 99, ctrlc: &ctrlc}
	c.selfHeal(d, "0:0.0", d)
	if ctrlc > 1 {
		t.Errorf("Ctrl-C sent = %d, want ≤1 (Working aborts the loop — never interrupt a turn)", ctrlc)
	}
}

func TestSelfHealStopsOnNoProgress(t *testing.T) {
	// H1: a Ctrl-C that does not change the overlay state stops the loop (an ignored Ctrl-C must not
	// march toward the exit). recoverAt=99 so ComposerState never changes → exactly one press.
	enter, ctrlc := 0, 0
	c := healConfirm(&enter, &ctrlc)
	d := &healStub{overlay: ComposerListNav, recoverAt: 99, ctrlc: &ctrlc}
	c.selfHeal(d, "0:0.0", d)
	if ctrlc != 1 {
		t.Errorf("Ctrl-C sent = %d, want 1 (state unchanged after the first press → stop)", ctrlc)
	}
}

func TestSelfHealTwoLayerStackRecovers(t *testing.T) {
	// A 2-layer overlay (sub-composer → panel → composer) that CHANGES disposition per press:
	// SubAgent → ListNav → Cleared. selfHeal presses twice (each a new state, so no-progress never
	// trips), then STOPS at Cleared. Exactly 2 Ctrl-C, never a 3rd into the recovered composer.
	enter, ctrlc := 0, 0
	c := healConfirm(&enter, &ctrlc)
	d := &healStub{stateSeq: []ComposerDisposition{ComposerSubAgent, ComposerListNav, ComposerCleared}, ctrlc: &ctrlc}
	c.selfHeal(d, "0:0.0", d)
	if ctrlc != 2 {
		t.Errorf("Ctrl-C sent = %d, want 2 (two-layer stack recovers in two presses, then STOP)", ctrlc)
	}
}

func TestSelfHealCapExhausted(t *testing.T) {
	// An overlay that keeps CHANGING but never reaches Cleared → the loop presses exactly
	// maxSelfHealCtrlC times and hits the cap (then the caller's single Submit re-detects → alert).
	enter, ctrlc := 0, 0
	c := healConfirm(&enter, &ctrlc)
	// alternates SubAgent/ListNav forever (always a state change, so no-progress never trips).
	seq := []ComposerDisposition{}
	for i := 0; i < maxSelfHealCtrlC+3; i++ {
		if i%2 == 0 {
			seq = append(seq, ComposerSubAgent)
		} else {
			seq = append(seq, ComposerListNav)
		}
	}
	d := &healStub{stateSeq: seq, ctrlc: &ctrlc}
	c.selfHeal(d, "0:0.0", d)
	if ctrlc != maxSelfHealCtrlC {
		t.Errorf("Ctrl-C sent = %d, want %d (cap-bounded; never unbounded toward the exit)", ctrlc, maxSelfHealCtrlC)
	}
}

func TestSelfHealShellAborts(t *testing.T) {
	// A pane that reads Shell during the loop aborts (no Ctrl-C into a gone session) — the exit
	// detector path.
	enter, ctrlc := 0, 0
	c := healConfirm(&enter, &ctrlc)
	d := &healStub{assessSeq: []State{StateShell}, overlay: ComposerSubAgent, recoverAt: 99, ctrlc: &ctrlc}
	c.selfHeal(d, "0:0.0", d)
	if ctrlc != 0 {
		t.Errorf("Ctrl-C sent = %d, want 0 (Shell → abort)", ctrlc)
	}
}
