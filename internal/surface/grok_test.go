package surface

import (
	"errors"
	"testing"
)

func TestGrokRegistered(t *testing.T) {
	d, ok := Get("grok")
	if !ok || d.Name() != "grok" {
		t.Errorf(`Get("grok") = (%v, %v), want the grok driver`, d, ok)
	}
}

func TestParseGrokState(t *testing.T) {
	// EXHAUSTIVE over the claude-style REDUCED ladder. Fixtures use grok-dev's real
	// rendered markers, SOURCE-VERIFIED at fb97af8 (NOT live-captured — grok-dev is
	// xAI-only/metered): ui/app.tsx Payment required/Paste your xAI API key/Planning
	// next moves/enter queue/esc interrupt; agent/agent.ts STATUS_MESSAGES. The
	// animated spinner ⬒⬔⬓⬕ is intentionally NOT a marker; the Plan-mode generic
	// "Confirm" is intentionally NOT keyed on.
	cases := []struct {
		name     string
		captured string
		want     State
	}{
		{
			name:     "x402 payment panel → AwaitingApproval",
			captured: "Payment required\nPrice: 0.05 USDC on base\nApprove payment  Reject",
			want:     StateAwaitingApproval,
		},
		{
			name:     "API-key-needed prompt (desk blocked) → AwaitingApproval",
			captured: "Paste your xAI API key to unlock chat. You can hide this prompt with esc.",
			want:     StateAwaitingApproval,
		},
		{
			// systems-review: grok renders transient errors INLINE in the conversation
			// (streamContent), not in the bottom chrome — and they linger as history. So
			// the driver does NOT emit Errored; a recovered desk with an old error above
			// an idle composer reads Idle (the turn already fired its Working→Idle wake).
			name:     "transient error in conversation scrollback + idle composer below → Idle (no Errored)",
			captured: "An unexpected error occurred.\n" + manyLines(14) + "Message Grok...\n@ files   shift+enter new line   tab modes",
			want:     StateIdle,
		},
		{
			// An AUTH error pops the api-key modal — caught as AwaitingApproval, the right
			// classification for a desk blocked needing the operator.
			name:     "auth error pops the api-key modal → AwaitingApproval (not a silent error)",
			captured: "Authentication failed.\nPaste your xAI API key to unlock chat. You can hide this prompt with esc.",
			want:     StateAwaitingApproval,
		},
		{
			name:     "pre-stream 'Planning next moves' → Working",
			captured: "Agent\nPlanning next moves\nenter queue   esc interrupt",
			want:     StateWorking,
		},
		{
			name:     "processing status bar 'enter queue' → Working",
			captured: "→ bash\nrunning ls -la\nenter queue   esc interrupt",
			want:     StateWorking,
		},
		{
			// THE reduced-set proof: Grok auto-executes a shell command (a tool is
			// running) with NO approval gate → Working, NEVER AwaitingApproval.
			name:     "auto-executing a shell tool (no approval gate) → Working, NOT AwaitingApproval",
			captured: "→ bash\n$ rm -rf node_modules\nenter queue   esc interrupt",
			want:     StateWorking,
		},
		{
			name:     "idle composer (Message Grok placeholder) → Idle (the default)",
			captured: "Agent\nMessage Grok...\n@ files   shift+enter new line   tab modes",
			want:     StateIdle,
		},
		{
			name:     "empty capture → Idle (classifier default)",
			captured: "",
			want:     StateIdle,
		},
		{
			// Bottom-chrome scoping: a model response quoting "Payment required" high up
			// (above the bottom chrome) must NOT false-trigger AwaitingApproval.
			name:     "model output quoting 'Payment required' high up + idle below → Idle",
			captured: "To pay an x402 invoice you'll see a \"Payment required\" panel.\n" + manyLines(14) + "Message Grok...\n@ files   shift+enter new line   tab modes",
			want:     StateIdle,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseGrokState(tc.captured); got != tc.want {
				t.Errorf("parseGrokState = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGrokAssess(t *testing.T) {
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
		{"isShell → shell (grok process gone)", "bash", nil, true, "", nil, StateShell},
		{"capture error → unknown (NOT a false finished-a-turn)", "node", nil, false, "", boom, StateUnknown},
		{"classifier routes: approval", "node", nil, false, "Payment required\nApprove payment", nil, StateAwaitingApproval},
		{"classifier routes: working", "node", nil, false, "Planning next moves", nil, StateWorking},
		{"classifier routes: idle", "node", nil, false, "Message Grok...", nil, StateIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := grok{
				paneCommand: func(string) (string, error) { return tc.cmd, tc.cmdErr },
				isShell:     func(string) bool { return tc.isShell },
				capturePane: func(string) (string, error) { return tc.captured, tc.captureErr },
				classify:    parseGrokState,
			}
			if got := g.Assess("0:0.0"); got != tc.want {
				t.Errorf("Assess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGrokSubmitRotateRoute(t *testing.T) {
	var submitted bool
	var injectedCmd string
	g := grok{
		send:   func(pane, text string) error { submitted = true; return nil },
		inject: func(pane, cmd string) error { injectedCmd = cmd; return nil },
	}
	if err := g.Submit("0:0.0", "hi"); err != nil || !submitted {
		t.Errorf("Submit routed=%v err=%v, want routed to send", submitted, err)
	}
	// The first driver whose reset is NOT /clear — validates the InjectSlash generalization.
	if err := g.Rotate("0:0.0"); err != nil || injectedCmd != "/new" {
		t.Errorf("Rotate injected %q err=%v, want /new (grok's reset, NOT /clear)", injectedCmd, err)
	}
	if g.RotateStrategy() != SlashCommand {
		t.Errorf("grok RotateStrategy = %v, want SlashCommand", g.RotateStrategy())
	}
	if newGrok().Name() != "grok" {
		t.Error("newGrok().Name() != grok")
	}
}
