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
	// Fixtures are LIVE-CAPTURED from the official grok CLI ("Grok Composer 2.5 Fast") on the
	// running grok-research desk (2026-06-16, #58). Working-positive, Idle-default. The Working
	// marker is the live streaming arrow ⇣ (U+21E3, present every frame of a turn); the gerund
	// verb (Thinking…/Waiting…) and the leading spinner glyph vary. Idle/done shows
	// "Turn completed in …" + an empty composer with NO arrow.
	cases := []struct {
		name     string
		captured string
		want     State
	}{
		{
			name:     "live streaming status (spinner + arrow + elapsed) → Working",
			captured: "     ...streamed output...\n\n  ⠙ Waiting… 0.4s ⇣127k [✗]",
			want:     StateWorking,
		},
		{
			// ARROW branch pinned in isolation: the streaming arrow ⇣ with NO braille spinner on
			// the line (so this case fails iff arrow-detection is removed — OCR-M1).
			name:     "arrow only, no spinner frame → Working (arrow branch)",
			captured: "  streaming  3.2s ⇣127k [✗]",
			want:     StateWorking,
		},
		{
			// SPINNER branch pinned in isolation: a braille spinner frame with NO arrow (the brief
			// pre-stream / thinking window) → fails iff spinner-detection is removed.
			name:     "spinner frame, no arrow → Working (spinner branch)",
			captured: "  ⠦ Thinking… 0.1s",
			want:     StateWorking,
		},
		{
			name:     "completed turn (Turn completed in … + ◆ stop, no arrow) → Idle",
			captured: "  ◆ stop  [hooks: 2]\n\n  Turn completed in 3.9s.\n\n  ╭────╮\n  │ ❯  │\n  ╰──── Grok Composer 2.5 Fast ─╯\n  Shift+Tab:mode  │  Ctrl+.:shortcuts",
			want:     StateIdle,
		},
		{
			name:     "fresh empty composer → Idle (the default)",
			captured: "  ╭────╮\n  │ ❯  │\n  ╰──── Grok Composer 2.5 Fast ─╯\n  Shift+Tab:mode  │  Ctrl+.:shortcuts",
			want:     StateIdle,
		},
		{
			name:     "empty capture → Idle",
			captured: "",
			want:     StateIdle,
		},
		{
			// P2 regression: a finished turn whose tail contains an ordinary Capitalized-word +
			// ellipsis ("Note…"/"Done…") must read Idle. The old broad [A-Z][a-z]+… secondary
			// false-matched these; the arrow/spinner anchors (grok chrome, not prose) do not.
			name:     "idle turn with a 'Note…' prose ellipsis in the tail → Idle (not a false Working)",
			captured: "  Note… see the summary above. Done… for now.\n  Turn completed in 2s.\n  │ ❯  │\n  Shift+Tab:mode",
			want:     StateIdle,
		},
		{
			// Bottom-chrome scoping: an old streaming arrow ⇣ scrolled high up in history (above
			// the tail) must NOT keep the desk reading Working after the turn finished.
			captured: "  ⠙ Waiting… 1.2s ⇣99k [✗]\n" + manyLines(14) + "  Turn completed in 5.0s.\n  │ ❯  │\n  Shift+Tab:mode",
			name:     "stale arrow in scrollback + completed below → Idle",
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
		{"classifier routes: working (arrow)", "grok", nil, false, "⠙ Waiting… 0.4s ⇣127k [✗]", nil, StateWorking},
		{"classifier routes: idle (completed)", "grok", nil, false, "Turn completed in 3.9s.\n│ ❯ │", nil, StateIdle},
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
	// Official grok resets with /new ("Start a new session"), confirmed in its slash menu.
	if err := g.Rotate("0:0.0"); err != nil || injectedCmd != "/new" {
		t.Errorf("Rotate injected %q err=%v, want /new", injectedCmd, err)
	}
	if g.RotateStrategy() != SlashCommand {
		t.Errorf("grok RotateStrategy = %v, want SlashCommand", g.RotateStrategy())
	}
	if newGrok().Name() != "grok" {
		t.Error("newGrok().Name() != grok")
	}
}
