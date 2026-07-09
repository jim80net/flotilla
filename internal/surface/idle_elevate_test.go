package surface

import "testing"

// #557 live gap: desk at an unanswered interactive exit-confirmation prompt (with
// live background subagents) assessed plain Idle while recycle's idle∧cleared gate
// correctly refused. Pin the status-facing classification so it is NOT plain idle.

func TestInteractiveConfirmPrompt_ExitConfirm557(t *testing.T) {
	// Sole-supervisor-as-member analog for prompts: numbered exit menu + Enter to confirm
	// (no working spinner) — the frame recycle aborts on and status must not call idle.
	exitConfirm := "" +
		"  background agents still running (6)\n" +
		"  review-task  ● running\n" +
		"  migration    ● running\n" +
		"\n" +
		"  Exit session?\n" +
		"  1. Save and exit\n" +
		"  2. Cancel\n" +
		"  Enter to confirm\n"
	if !InteractiveConfirmPrompt(exitConfirm) {
		t.Fatal("exit-confirmation + numbered menu must detect InteractiveConfirmPrompt")
	}
	if got := parseGrokState(exitConfirm); got != StateAwaitingInput {
		t.Errorf("parseGrokState(exit confirm) = %v, want awaiting-input (not idle)", got)
	}
}

func TestInteractiveConfirmPrompt_WorktreeStillDetected(t *testing.T) {
	wt := "Exiting worktree session\n  1. Keep worktree\n  2. Remove worktree\nEnter to confirm"
	if !InteractiveConfirmPrompt(wt) {
		t.Fatal("Claude worktree-exit must remain InteractiveConfirmPrompt")
	}
}

func TestInteractiveConfirmPrompt_GenuineIdleNotBlocked(t *testing.T) {
	// Healthy idle composer with scrollback that merely mentions "confirm" higher up.
	idle := "earlier: please confirm the approach with the XO\n\n❯ \n  ⏵ auto mode on\n"
	if InteractiveConfirmPrompt(idle) {
		t.Fatal("genuine idle composer must not trip InteractiveConfirmPrompt")
	}
	if got := parseGrokState(idle); got != StateIdle {
		t.Errorf("parseGrokState(idle) = %v, want idle", got)
	}
}

func TestElevateIdle_SubAgentAndListNav557(t *testing.T) {
	if got := ElevateIdle(StateIdle, ComposerSubAgent); got != StateAwaitingInput {
		t.Errorf("SubAgent elevate = %v, want awaiting-input", got)
	}
	if got := ElevateIdle(StateIdle, ComposerListNav); got != StateAwaitingInput {
		t.Errorf("ListNav elevate = %v, want awaiting-input", got)
	}
	if got := ElevateIdle(StateIdle, ComposerQueued); got != StateWorking {
		t.Errorf("Queued elevate = %v, want working", got)
	}
	// Cleared / Undetermined / Pending leave Idle (panel-display-only stays idle;
	// Undetermined is capture-glitch fail-open for elevation — confirm chrome is separate).
	for _, d := range []ComposerDisposition{ComposerCleared, ComposerUndetermined, ComposerPending} {
		if got := ElevateIdle(StateIdle, d); got != StateIdle {
			t.Errorf("ElevateIdle(Idle, %v) = %v, want idle", d, got)
		}
	}
	// Non-idle states are unchanged.
	if got := ElevateIdle(StateWorking, ComposerSubAgent); got != StateWorking {
		t.Errorf("Working+SubAgent must stay Working, got %v", got)
	}
}

// elevateStub exercises AssessForFleet: bare Assess returns Idle, ComposerState is scripted.
type elevateStub struct {
	assess State
	disp   ComposerDisposition
}

func (elevateStub) Name() string                               { return "elevate-stub" }
func (s elevateStub) Submit(string, string) error              { return nil }
func (s elevateStub) Assess(string) State                      { return s.assess }
func (s elevateStub) Rotate(string) error                      { return nil }
func (elevateStub) RotateStrategy() Strategy                   { return SlashCommand }
func (elevateStub) Close(string) error                         { return ErrNoGracefulClose }
func (s elevateStub) ComposerState(string) ComposerDisposition { return s.disp }

func TestAssessForFleet_ElevatesPromptBlockedComposer557(t *testing.T) {
	// Same structural class as recycle abort: Assess Idle but cursor on subagent panel.
	d := elevateStub{assess: StateIdle, disp: ComposerSubAgent}
	if got := AssessForFleet(d, "0:0.0"); got != StateAwaitingInput {
		t.Errorf("AssessForFleet(Idle+SubAgent) = %v, want awaiting-input", got)
	}
	// Genuine idle∧cleared stays idle.
	d2 := elevateStub{assess: StateIdle, disp: ComposerCleared}
	if got := AssessForFleet(d2, "0:0.0"); got != StateIdle {
		t.Errorf("AssessForFleet(Idle+Cleared) = %v, want idle", got)
	}
}

func TestStatusLabel_AwaitingInputNotIdle557(t *testing.T) {
	// deskStateLabel is in package main — pin the State.String contract status uses.
	if StateAwaitingInput.String() != "awaiting-input" {
		t.Fatalf("StateAwaitingInput.String = %q", StateAwaitingInput.String())
	}
	if StateIdle.String() == StateAwaitingInput.String() {
		t.Fatal("idle and awaiting-input must remain distinct labels")
	}
}
