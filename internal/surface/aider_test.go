package surface

import (
	"errors"
	"testing"
)

func TestAiderRegistered(t *testing.T) {
	d, ok := Get("aider")
	if !ok || d.Name() != "aider" {
		t.Errorf(`Get("aider") = (%v, %v), want the aider driver`, d, ok)
	}
}

func TestParseAiderState(t *testing.T) {
	// EXHAUSTIVE over the idle-positive ladder, including the polarity-fix and
	// tail-scoping regressions. Fixtures use aider's real rendered markers
	// (verified against source: io.py:832 approval, io.py:545-550 prompt,
	// base_coder.py:1486 retry, exceptions.py / report.py error phrases).
	//
	// LIVE-CAPTURE CONFIRMED (surface-driver-aider §4, aider 0.86.2 against a local
	// ollama model, $0, via `tmux capture-pane -p` — the exact mechanism Assess uses):
	//   - Idle     : last line "> "                          → matches aiderPromptLine
	//   - Working  : "░█  Waiting for ollama_chat/<model>"   → last line not a prompt → default Working
	//   - Approval : "...? (Y)es/(N)o [Yes]: " on last line  → Create-file / Add-file both route here
	//   - Rotate   : literal /clear + Enter → "All chat history cleared." → back to "> "
	//   - a running aider's pane_current_command == "python3" → IsShell=false (NOT a false StateShell)
	//   - multi-line bracketed-paste Submit (incl. a "}" line and a "{"-leading line) lands literal, one turn
	// Errored (auth/uncaught) and crash-to-shell were NOT live-induced (they need a
	// deliberately broken model/key, or a process exit that closes an exec'd pane — the
	// crash path is the claude-identical ResolvePane-failure route); their markers are
	// source-verified (exceptions.py / report.py) and Errored is best-effort by design.
	cases := []struct {
		name     string
		captured string
		want     State
	}{
		{
			name:     "open approval prompt (last line) → AwaitingApproval",
			captured: "diff --git a/x\nmake clean\nRun shell command? (Y)es/(N)o/(A)ll/(S)kip all [Yes]: ",
			want:     StateAwaitingApproval,
		},
		{
			name:     "add-file approval → AwaitingApproval",
			captured: "main.py\nAdd file to the chat? (Y)es/(N)o/(A)ll/(S)kip all [Yes]: ",
			want:     StateAwaitingApproval,
		},
		{
			name:     "returned bare prompt → Idle (positive)",
			captured: "All chat history cleared.\n\n> ",
			want:     StateIdle,
		},
		{
			name:     "edit-format prompt ask> → Idle",
			captured: "Some answer text.\nask> ",
			want:     StateIdle,
		},
		{
			name:     "architect prompt → Idle",
			captured: "architect> ",
			want:     StateIdle,
		},
		{
			name:     "multiline prompt multi> → Idle",
			captured: "multi> ",
			want:     StateIdle,
		},
		{
			name:     "edit-format + multiline 'ask multi>' → Idle",
			captured: "ask multi> ",
			want:     StateIdle,
		},
		{
			name:     "stale approval in scrollback + prompt last line → Idle (tail-anchored, not misled)",
			captured: "Run shell command? (Y)es/(N)o [Yes]: y\nRan 1 shell command\nDone.\n> ",
			want:     StateIdle,
		},
		{
			name:     "mid-stream (no prompt, no marker) → Working (the polarity fix)",
			captured: "Here is the plan. First I will refactor the\nclassifier to be idle-positive and then",
			want:     StateWorking,
		},
		{
			name:     "auto-retry countdown → Working (self-healing, not Errored)",
			captured: "litellm.RateLimitError ...\nThe API provider has rate limited you. Try again later or check your quotas.\nRetrying in 4.0 seconds...",
			want:     StateWorking,
		},
		{
			name:     "Waiting-for spinner line → Working",
			captured: "> add a feature\nWaiting for ollama_chat/qwen2.5-coder",
			want:     StateWorking,
		},
		{
			name:     "live auth error, no prompt below → Errored",
			captured: "litellm.AuthenticationError\nThe API provider is not able to authenticate you. Check your API key.\n",
			want:     StateErrored,
		},
		{
			name:     "uncaught exception banner → Errored",
			captured: "An uncaught exception occurred:\n\n```\nTraceback (most recent call last):\n",
			want:     StateErrored,
		},
		{
			name:     "error phrase WITH prompt returned below → Idle (recovered, idle wins over error)",
			captured: "The API provider is not able to authenticate you. Check your API key.\n> ",
			want:     StateIdle,
		},
		{
			name:     "empty capture → Working (default; not a false idle)",
			captured: "",
			want:     StateWorking,
		},
		{
			name:     "blockquote line is not the prompt → Working (not a false idle)",
			captured: "Consider this note:\n> quoted advice from the model",
			want:     StateWorking,
		},
		{
			name:     "stale prompt mid-scrollback, streaming below → Working (only last line is the idle anchor)",
			captured: "> earlier turn\nGenerating the next chunk of the response now",
			want:     StateWorking,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAiderState(tc.captured); got != tc.want {
				t.Errorf("parseAiderState = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAiderAssess(t *testing.T) {
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
		{"panecommand error → unknown (transient glitch)", "", boom, false, "", nil, StateUnknown},
		{"isShell → shell (aider process gone)", "bash", nil, true, "", nil, StateShell},
		{"capture error → unknown (NOT a false finish — idle-positive)", "python", nil, false, "", boom, StateUnknown},
		{"classifier routes: approval", "python", nil, false, "Run shell command? (Y)es/(N)o [Yes]: ", nil, StateAwaitingApproval},
		{"classifier routes: idle prompt", "python", nil, false, "done\n> ", nil, StateIdle},
		{"classifier routes: working default", "python", nil, false, "streaming the response", nil, StateWorking},
		{"classifier routes: errored", "python", nil, false, "Check your API key\n", nil, StateErrored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := aider{
				paneCommand: func(string) (string, error) { return tc.cmd, tc.cmdErr },
				isShell:     func(string) bool { return tc.isShell },
				capturePane: func(string) (string, error) { return tc.captured, tc.captureErr },
				classify:    parseAiderState,
			}
			if got := a.Assess("0:0.0"); got != tc.want {
				t.Errorf("Assess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAiderSubmitRotateRoute(t *testing.T) {
	var submitted bool
	var injectedCmd string
	a := aider{
		send:   func(pane, text string) error { submitted = true; return nil },
		inject: func(pane, cmd string) error { injectedCmd = cmd; return nil },
	}
	if err := a.Submit("0:0.0", "hi"); err != nil || !submitted {
		t.Errorf("Submit routed=%v err=%v, want routed to send", submitted, err)
	}
	if err := a.Rotate("0:0.0"); err != nil || injectedCmd != "/clear" {
		t.Errorf("Rotate injected %q err=%v, want /clear", injectedCmd, err)
	}
	if a.RotateStrategy() != SlashCommand {
		t.Errorf("aider RotateStrategy = %v, want SlashCommand", a.RotateStrategy())
	}
	if newAider().Name() != "aider" {
		t.Error("newAider().Name() != aider")
	}
}
