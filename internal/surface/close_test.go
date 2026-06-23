package surface

import (
	"errors"
	"testing"
)

// TestClaudeCloseIssuesExit asserts the claude driver's Close injects exactly "/exit"
// through its slash-keys seam (a wrong command would be caught here, before the live
// 6.3 keystroke characterization).
func TestClaudeCloseIssuesExit(t *testing.T) {
	var gotPane, gotCmd string
	calls := 0
	c := claudeCode{slashKeys: func(pane, cmd string) error {
		calls++
		gotPane, gotCmd = pane, cmd
		return nil
	}}
	if err := c.Close("flotilla:0.1"); err != nil {
		t.Fatalf("claude Close: %v", err)
	}
	if calls != 1 {
		t.Errorf("slashKeys called %d times, want 1", calls)
	}
	if gotPane != "flotilla:0.1" {
		t.Errorf("pane = %q, want flotilla:0.1", gotPane)
	}
	if gotCmd != "/exit" {
		t.Errorf("cmd = %q, want /exit", gotCmd)
	}
}

// TestClaudeClosePropagatesError: a slash-keys failure surfaces (never swallowed).
func TestClaudeClosePropagatesError(t *testing.T) {
	boom := errors.New("tmux send-keys failed")
	c := claudeCode{slashKeys: func(string, string) error { return boom }}
	if err := c.Close("p"); !errors.Is(err, boom) {
		t.Errorf("claude Close err = %v, want %v", err, boom)
	}
}

// TestAiderCloseIssuesExit: aider's Close injects "/exit" via its inject seam.
func TestAiderCloseIssuesExit(t *testing.T) {
	var gotCmd string
	a := aider{inject: func(_, cmd string) error { gotCmd = cmd; return nil }}
	if err := a.Close("p"); err != nil {
		t.Fatalf("aider Close: %v", err)
	}
	if gotCmd != "/exit" {
		t.Errorf("aider Close cmd = %q, want /exit", gotCmd)
	}
}

// TestNoGracefulCloseDrivers: grok and opencode have no live-verified clean exit, so
// Close returns ErrNoGracefulClose (the honest refusal) and injects NOTHING — the caller
// uses the handoff-gated kill fallback.
func TestNoGracefulCloseDrivers(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    Driver
	}{
		{"grok", newGrok()},
		{"opencode", newOpenCode()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.d.Close("p"); !errors.Is(err, ErrNoGracefulClose) {
				t.Errorf("%s Close err = %v, want ErrNoGracefulClose", tc.name, err)
			}
		})
	}
}
