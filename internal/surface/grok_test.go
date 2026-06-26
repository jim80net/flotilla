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

// The grok driver must satisfy the optional ResultReader capability (compile-time), and — since #158 —
// the ComposerStateProbe + RecycleBridge capabilities (so a grok desk is recycle-capable).
var (
	_ ResultReader       = grok{}
	_ ComposerStateProbe = grok{}
	_ RecycleBridge      = grok{}
	_ ReplyReader        = grok{}       // #175 c2-hotline reply correlation seam
	_ ReplyReader        = claudeCode{} // claude implements it too (the reference)
)

// TestParseGrokStateApproval: the tool-approval modal classifies AwaitingApproval, NOT Working, even
// though the ⇣ streamed-token arrow is co-present on the modal's "◆ Run …" line (the live #58/#158 gap).
// Fixtures LIVE-CAPTURED 2026-06-23 from a throwaway grok session (design.md §10.3) — not recalled.
func TestParseGrokStateApproval(t *testing.T) {
	// The exact captured modal tail: a ◆ Run line carrying ⇣19.0k, the ┃ Allow block, and the
	// "1/4:select │ Ctrl+o:yolo │ Ctrl+c:cancel" status line.
	modal := "    ◆ Run Edit `/tmp/grok-char/hello.txt` 10s                       11s ⇣19.0k [✗]\n" +
		"\n  ┃\n  ┃  Allow Edit `/tmp/grok-char/hello.txt`?\n  ┃\n" +
		"  ┃  1 (●) Yes, and don't ask again for anything (always-approve mode)\n" +
		"  ┃  2 (○) Yes, allow all edits during this session\n  ┃  3 (○) Yes\n" +
		"  ┃  4 (○) No, reject (type to add feedback)\n  ┃\n\n" +
		"  1/4:select  │  Ctrl+o:yolo  │  Ctrl+c:cancel"
	if got := parseGrokState(modal); got != StateAwaitingApproval {
		t.Errorf("parseGrokState(approval modal) = %v, want AwaitingApproval (must precede the Working/⇣ check)", got)
	}
	// A genuine streaming turn (arrow, no modal chrome) still reads Working — the modal anchors must
	// not over-fire on ordinary output.
	if got := parseGrokState("  ⠙ Waiting… 0.4s ⇣127k [✗]"); got != StateWorking {
		t.Errorf("parseGrokState(streaming) = %v, want Working", got)
	}
	// An idle finished turn still reads Idle.
	if got := parseGrokState("  Turn completed in 3.9s.\n  │ ❯  │\n  Shift+Tab:mode"); got != StateIdle {
		t.Errorf("parseGrokState(idle) = %v, want Idle", got)
	}
	// Tail-scoping symmetry (mirrors the stale-arrow scrollback case): a modal's status line scrolled
	// ABOVE the grokTail window, with an idle composer below, must NOT keep the desk reading
	// AwaitingApproval — only the bottom chrome decides.
	staleModal := "  1/4:select  │  Ctrl+o:yolo  │  Ctrl+c:cancel\n" + manyLines(14) +
		"  Turn completed in 5.0s.\n  │ ❯  │\n  Shift+Tab:mode"
	if got := parseGrokState(staleModal); got != StateIdle {
		t.Errorf("parseGrokState(stale modal in scrollback + idle below) = %v, want Idle", got)
	}
}

// TestClassifyGrokComposerLine: the cursor-indexed composer classifier over the §10 live captures.
func TestClassifyGrokComposerLine(t *testing.T) {
	// Realistic grok composer-box renders (LIVE-CAPTURED 2026-06-23, design.md §10.1/§10.2). The
	// classifier reads the line at cursorY; the box left/right borders (│) must be stripped.
	const cleared = "  ╭────────╮\n  │ ❯                                              │\n  ╰──── Composer 2.5 Fast ─╯"
	const pending = "  ╭────────╮\n  │ ❯ characterization pending body do not submit  │\n  ╰──── Composer 2.5 Fast ─╯"
	// A multi-line pending body: the cursor on the THIRD (continuation) row, which has no ❯.
	const multiline = "  ╭────────╮\n  │ ❯ line ONE                                     │\n  │   line TWO                                     │\n  │   line THREE                                   │\n  ╰──── Composer 2.5 Fast ─╯"
	// The approval modal: the cursor sits on the ◆ Run line (no ❯), with the ┃ block below.
	const modalCap = "    ◆ Run Edit `/x/hello.txt` 10s          11s ⇣19.0k [✗]\n\n  ┃\n  ┃  Allow Edit `/x/hello.txt`?\n  ┃\n  ┃  1 (●) Yes\n\n  1/4:select  │  Ctrl+o:yolo  │  Ctrl+c:cancel"

	cases := []struct {
		name     string
		captured string
		cursorY  int
		want     ComposerDisposition
	}{
		{"empty composer at cursor → Cleared", cleared, 1, ComposerCleared},
		{"composer with a pending body → Pending", pending, 1, ComposerPending},
		// A lone user-typed box-drawing │ must NOT false-read Cleared (the recycle gate would discard
		// the draft) — only the trailing RIGHT border is stripped, not a typed │ in the body.
		{"body is a lone typed │ → Pending (not a false Cleared)", "  │ ❯ │                       │", 0, ComposerPending},
		{"multi-line continuation row (no ❯) → Undetermined (non-Cleared, fail-closed)", multiline, 3, ComposerUndetermined},
		{"approval modal: cursor on ◆ Run line (no ❯) → Undetermined (NOT Cleared — gate-safety)", modalCap, 0, ComposerUndetermined},
		{"cursor past the captured range → Undetermined", cleared, 9999, ComposerUndetermined},
		{"negative cursor → Undetermined", cleared, -1, ComposerUndetermined},
		{"cursor on a plain (non-prompt) line → Undetermined", "  plain conversation line\n  more", 0, ComposerUndetermined},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyGrokComposerLine(tc.captured, tc.cursorY); got != tc.want {
				t.Errorf("classifyGrokComposerLine = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGrokComposerStateWiring: ComposerState threads cursorState + capturePane → classifyGrokComposerLine,
// and fails safe (Undetermined) on a cursor read error or a tmux copy/view mode.
func TestGrokComposerStateWiring(t *testing.T) {
	const cleared = "  │ ❯                          │"
	t.Run("idle cleared composer → Cleared", func(t *testing.T) {
		g := grok{
			cursorState: func(string) (int, bool, error) { return 0, false, nil },
			capturePane: func(string) (string, error) { return cleared, nil },
		}
		if got := g.ComposerState("0:0.0"); got != ComposerCleared {
			t.Errorf("ComposerState = %v, want Cleared", got)
		}
	})
	t.Run("cursor read error → Undetermined", func(t *testing.T) {
		g := grok{cursorState: func(string) (int, bool, error) { return 0, false, errBoom }}
		if got := g.ComposerState("0:0.0"); got != ComposerUndetermined {
			t.Errorf("ComposerState = %v, want Undetermined on cursor error", got)
		}
	})
	t.Run("tmux copy/view mode → Undetermined", func(t *testing.T) {
		g := grok{
			cursorState: func(string) (int, bool, error) { return 0, true, nil }, // in mode
			capturePane: func(string) (string, error) { return cleared, nil },
		}
		if got := g.ComposerState("0:0.0"); got != ComposerUndetermined {
			t.Errorf("ComposerState = %v, want Undetermined in copy-mode", got)
		}
	})
	t.Run("capture error → Undetermined", func(t *testing.T) {
		g := grok{
			cursorState: func(string) (int, bool, error) { return 0, false, nil },
			capturePane: func(string) (string, error) { return "", errBoom },
		}
		if got := g.ComposerState("0:0.0"); got != ComposerUndetermined {
			t.Errorf("ComposerState = %v, want Undetermined on capture error", got)
		}
	})
}

func TestGrokLatestResult(t *testing.T) {
	t.Run("resolves cwd then reads the store", func(t *testing.T) {
		g := grok{
			paneCWD:  func(pane string) (string, error) { return "/srv/fleet/research", nil },
			grokHome: "/home/you/.grok",
			latestResult: func(home, cwd string) (string, error) {
				if home != "/home/you/.grok" || cwd != "/srv/fleet/research" {
					t.Errorf("latestResult got (home=%q, cwd=%q), want the resolved pair", home, cwd)
				}
				return "the full latest grok result", nil
			},
		}
		got, err := g.LatestResult("flotilla:5.0")
		if err != nil || got != "the full latest grok result" {
			t.Errorf("LatestResult = (%q, %v), want the store result", got, err)
		}
	})
	t.Run("pane cwd resolution error propagates", func(t *testing.T) {
		g := grok{paneCWD: func(string) (string, error) { return "", errBoom }, grokHome: "/x"}
		if _, err := g.LatestResult("p"); err != errBoom {
			t.Errorf("err = %v, want the cwd-resolution error", err)
		}
	})
	t.Run("empty grokHome → clear error, store not consulted", func(t *testing.T) {
		called := false
		g := grok{
			paneCWD:      func(string) (string, error) { return "/cwd", nil },
			grokHome:     "",
			latestResult: func(string, string) (string, error) { called = true; return "", nil },
		}
		if _, err := g.LatestResult("p"); err == nil {
			t.Error("want an error when grokHome is unresolved")
		}
		if called {
			t.Error("store must not be consulted when grokHome is empty")
		}
	})
}

var errBoom = errors.New("boom")
