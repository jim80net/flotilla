package surface

import (
	"errors"
	"testing"
)

func TestCursorRegistered(t *testing.T) {
	d, ok := Get("cursor")
	if !ok || d.Name() != "cursor" {
		t.Errorf(`Get("cursor") = (%v, %v), want the cursor driver`, d, ok)
	}
}

func TestParseCursorState(t *testing.T) {
	// The cursor markers are PLACEHOLDER sentinels (#61, closed-source — live-capture
	// pending). These tests lock the LADDER STRUCTURE using those sentinels; the
	// live-capture replaces the sentinels + these fixtures with observed strings.
	cases := []struct {
		name     string
		captured string
		want     State
	}{
		{
			name:     "approval placeholder present → AwaitingApproval (ladder structure)",
			captured: "some context\n__CURSOR_APPROVAL_PLACEHOLDER_PENDING_LIVE_CAPTURE_61__",
			want:     StateAwaitingApproval,
		},
		{
			name:     "working placeholder present → Working (ladder structure)",
			captured: "some context\n__CURSOR_WORKING_PLACEHOLDER_PENDING_LIVE_CAPTURE_61__",
			want:     StateWorking,
		},
		{
			// THE INERT PROOF: realistic-looking cursor output (no sentinel) → Idle. The
			// skeleton cannot mis-fire AwaitingApproval/Working before live-capture (#61).
			name:     "realistic cursor pane (no sentinel) → Idle (INERT until live-capture)",
			captured: "Cursor Agent\nRun `ls -la`?  (y) to approve / (n) to reject\n> ",
			want:     StateIdle,
		},
		{
			name:     "empty capture → Idle (default)",
			captured: "",
			want:     StateIdle,
		},
		{
			// Bottom-chrome scoping: a sentinel quoted high up (above the tail window) is
			// not matched — same scoping the real markers will rely on.
			name:     "sentinel quoted high above the tail → Idle",
			captured: "__CURSOR_APPROVAL_PLACEHOLDER_PENDING_LIVE_CAPTURE_61__\n" + manyLines(14) + "> ",
			want:     StateIdle,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCursorState(tc.captured); got != tc.want {
				t.Errorf("parseCursorState = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCursorAssess(t *testing.T) {
	boom := errors.New("tmux boom")
	cases := []struct {
		name       string
		cmd        string
		cmdErr     error
		isShell    bool
		captured   string
		captureErr error
		want       State
	}{
		{"panecommand error → unknown", "", boom, false, "", nil, StateUnknown},
		{"isShell → shell (cursor process gone)", "bash", nil, true, "", nil, StateShell},
		{"capture error → unknown", "agent", nil, false, "", boom, StateUnknown},
		{"classifier routes: approval placeholder", "agent", nil, false, "__CURSOR_APPROVAL_PLACEHOLDER_PENDING_LIVE_CAPTURE_61__", nil, StateAwaitingApproval},
		{"classifier routes: inert idle (real-looking input)", "agent", nil, false, "Run `ls`? (y)/(n)\n> ", nil, StateIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cursor{
				paneCommand: func(string) (string, error) { return tc.cmd, tc.cmdErr },
				isShell:     func(string) bool { return tc.isShell },
				capturePane: func(string) (string, error) { return tc.captured, tc.captureErr },
				classify:    parseCursorState,
			}
			if got := c.Assess("0:0.0"); got != tc.want {
				t.Errorf("Assess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCursorSubmitRotateRoute(t *testing.T) {
	var submitted bool
	var injectedCmd string
	c := cursor{
		send:   func(pane, text string) error { submitted = true; return nil },
		inject: func(pane, cmd string) error { injectedCmd = cmd; return nil },
	}
	if err := c.Submit("0:0.0", "hi"); err != nil || !submitted {
		t.Errorf("Submit routed=%v err=%v, want routed to send", submitted, err)
	}
	// The SECOND driver whose reset is not /clear (after grok's /new) — further
	// validates the InjectSlash generalization.
	if err := c.Rotate("0:0.0"); err != nil || injectedCmd != "/new-chat" {
		t.Errorf("Rotate injected %q err=%v, want /new-chat (cursor's reset)", injectedCmd, err)
	}
	if c.RotateStrategy() != SlashCommand {
		t.Errorf("cursor RotateStrategy = %v, want SlashCommand", c.RotateStrategy())
	}
	if newCursor().Name() != "cursor" {
		t.Error("newCursor().Name() != cursor")
	}
}
