package surface

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOpenCodeRegistered(t *testing.T) {
	d, ok := Get("opencode")
	if !ok || d.Name() != "opencode" {
		t.Errorf(`Get("opencode") = (%v, %v), want the opencode driver`, d, ok)
	}
}

func TestOpenCodeRecycleCapabilities(t *testing.T) {
	var _ RecycleBridge = openCode{}
	var _ ComposerStateProbe = openCode{}

	o := newOpenCode()
	path := o.HandoffPath("/home/operator/work/project", "20260713T120000.000000001-abcd1234")
	want := "/home/operator/work/project/.flotilla/handoffs/recycle-20260713T120000.000000001-abcd1234.md"
	if path != want {
		t.Fatalf("HandoffPath = %q, want %q", path, want)
	}
	handoff := o.HandoffTurn(path)
	if !strings.HasPrefix(handoff, PortableMarkdownHandoffTurn(path)) {
		t.Fatal("HandoffTurn must use the shared portable-markdown handoff")
	}
	for _, must := range []string{"use the context already in this session", "Do NOT inspect the worktree", "only tool operation", path} {
		if !strings.Contains(handoff, must) {
			t.Fatalf("HandoffTurn missing OpenCode no-discovery constraint %q", must)
		}
	}
	takeover := o.TakeoverTurn(path)
	if !strings.HasPrefix(takeover, PortableMarkdownTakeoverTurn(path)) {
		t.Fatal("TakeoverTurn must use the shared portable-markdown takeover")
	}
	for _, must := range []string{"exactly the standalone", "Do NOT append `&&`", path} {
		if !strings.Contains(takeover, must) {
			t.Fatalf("TakeoverTurn missing OpenCode exact-cleanup constraint %q", must)
		}
	}
}

func TestClassifyOpenCodeComposerLine(t *testing.T) {
	// Generic normalized fixtures from the LIVE-CAPTURED OpenCode 1.3.15
	// composer marker. The draft text is synthetic.
	cases := []struct {
		name     string
		captured string
		cursorX  int
		cursorY  int
		want     ComposerDisposition
	}{
		{"empty composer", "output\n  ┃\nfooter", 4, 1, ComposerCleared},
		{"fresh placeholder at body start", "output\n  ┃  Ask anything... \"Fix a TODO in the codebase\"\nfooter", 5, 1, ComposerCleared},
		{"randomized placeholder at body start", "output\n  ┃  Ask anything... \"What is the tech stack of this project?\"\nfooter", 5, 1, ComposerCleared},
		{"source-verified third placeholder", "output\n  ┃  Ask anything... \"Fix broken tests\"\nfooter", 5, 1, ComposerCleared},
		{"unknown placeholder wording fails closed", "output\n  ┃  Ask anything... \"Uncharacterized example\"\nfooter", 5, 1, ComposerPending},
		{"same text typed with cursor advanced", "output\n  ┃  Ask anything... \"Fix a TODO in the codebase\"\nfooter", 48, 1, ComposerPending},
		{"pending draft", "output\n  ┃  beta-probe\nfooter", 14, 1, ComposerPending},
		{"wide prefix fails closed", "output\n界  ┃  Ask anything... \"Fix a TODO in the codebase\"\nfooter", 7, 1, ComposerUndetermined},
		{"non-composer row", "output\nAllow once\nfooter", 0, 1, ComposerUndetermined},
		{"cursor above capture", "  ┃", 4, -1, ComposerUndetermined},
		{"cursor below capture", "  ┃", 4, 1, ComposerUndetermined},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyOpenCodeComposerLine(tc.captured, tc.cursorX, tc.cursorY); got != tc.want {
				t.Fatalf("classifyOpenCodeComposerLine = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyHiddenOpenCodeComposer(t *testing.T) {
	placeholder := `Ask anything... "Fix a TODO in the codebase"`
	plain := "output\n  ┃\n  ┃  " + placeholder + "\n  ┃\n  ┃  Build  alpha-model\nfooter"
	styledPlaceholder := "output\n  ┃\n\x1b[38;2;255;255;255m  ┃  \x1b[38;2;49;49;49m" + placeholder + "\n  ┃\n  ┃  Build  alpha-model\nfooter"
	styledDraft := "output\n  ┃\n\x1b[38;2;255;255;255m  ┃  " + placeholder + "\n  ┃\n  ┃  Build  alpha-model\nfooter"

	cases := []struct {
		name, captured, styled string
		want                   ComposerDisposition
	}{
		{"muted placeholder is empty", plain, styledPlaceholder, ComposerCleared},
		{"identical typed text stays pending", plain, styledDraft, ComposerPending},
		{"blank session input is empty", "output\n  ┃\n  ┃\n  ┃\n  ┃  Build  alpha-model\nfooter", "output\n  ┃\n  ┃\n  ┃\n  ┃  Build  alpha-model\nfooter", ComposerCleared},
		{"ordinary draft stays pending", "output\n  ┃\n  ┃  beta-probe\n  ┃\n  ┃  Build  alpha-model\nfooter", "output\n  ┃\n  ┃  beta-probe\n  ┃\n  ┃  Build  alpha-model\nfooter", ComposerPending},
		{"unrecognized layout fails closed", "output\nfooter", "output\nfooter", ComposerUndetermined},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyHiddenOpenCodeComposer(tc.captured, tc.styled); got != tc.want {
				t.Fatalf("classifyHiddenOpenCodeComposer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOpenCodeComposerStateHiddenCursorUsesStyledBlock(t *testing.T) {
	placeholder := `Ask anything... "Fix a TODO in the codebase"`
	plain := "output\n  ┃\n  ┃  " + placeholder + "\n  ┃\n  ┃  Build  alpha-model\nfooter"
	o := openCode{
		cursorSnapshot: func(string) (int, int, bool, bool, error) { return 120, 40, false, false, nil },
		capturePane:    func(string) (string, error) { return plain, nil },
		captureStyled: func(string) (string, error) {
			return "output\n  ┃\n\x1b[38;2;255;255;255m  ┃  \x1b[38;2;49;49;49m" + placeholder + "\n  ┃\n  ┃  Build  alpha-model\nfooter", nil
		},
		classify: parseOpenCodeState,
	}
	if got := o.ComposerState("alpha"); got != ComposerCleared {
		t.Fatalf("ComposerState = %v, want Cleared for hidden cursor + styled empty placeholder", got)
	}
}

func TestDecodeSGRForeground(t *testing.T) {
	cases := []struct {
		name, input, wantText string
		wantFG                []string
	}{
		{"standard", "\x1b[31mred", "red", []string{"31", "31", "31"}},
		{"256 color", "\x1b[38;5;200mx", "x", []string{"38;5;200"}},
		{"true color", "\x1b[38;2;255;128;64mx", "x", []string{"38;2;255;128;64"}},
		{"combined reset and color", "\x1b[0;31mx", "x", []string{"31"}},
		{"bold then color", "\x1b[1;31mx", "x", []string{"31"}},
		{"non SGR ignored", "a\x1b[2Kb", "ab", []string{"default", "default"}},
		{"malformed escape retained", "a\x1b[", "a\x1b[", []string{"default", "default", "default"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, fg := decodeSGRForeground(tc.input)
			if text != tc.wantText || !reflect.DeepEqual(fg, tc.wantFG) {
				t.Fatalf("decodeSGRForeground = (%q, %v), want (%q, %v)", text, fg, tc.wantText, tc.wantFG)
			}
		})
	}
}

func TestOpenCodeComposerStateWiring(t *testing.T) {
	t.Run("idle empty composer is cleared", func(t *testing.T) {
		o := openCode{
			cursorSnapshot: func(string) (int, int, bool, bool, error) { return 4, 1, true, false, nil },
			capturePane:    func(string) (string, error) { return "output\n  ┃\nfooter", nil },
			classify:       parseOpenCodeState,
		}
		if got := o.ComposerState("alpha"); got != ComposerCleared {
			t.Fatalf("ComposerState = %v, want Cleared", got)
		}
	})

	t.Run("approval frame is undetermined", func(t *testing.T) {
		o := openCode{
			cursorSnapshot: func(string) (int, int, bool, bool, error) { return 4, 1, true, false, nil },
			capturePane: func(string) (string, error) {
				return "Permission required\n  ┃\nAllow once", nil
			},
			classify: parseOpenCodeState,
		}
		if got := o.ComposerState("alpha"); got != ComposerUndetermined {
			t.Fatalf("ComposerState = %v, want Undetermined", got)
		}
	})

	t.Run("copy mode and read failures are undetermined", func(t *testing.T) {
		boom := errors.New("tmux boom")
		cases := []openCode{
			{cursorSnapshot: func(string) (int, int, bool, bool, error) { return 0, 0, true, true, nil }},
			{cursorSnapshot: func(string) (int, int, bool, bool, error) { return 0, 0, false, false, boom }},
			{
				cursorSnapshot: func(string) (int, int, bool, bool, error) { return 0, 0, true, false, nil },
				capturePane:    func(string) (string, error) { return "", boom },
			},
		}
		for i, o := range cases {
			if got := o.ComposerState("alpha"); got != ComposerUndetermined {
				t.Fatalf("case %d ComposerState = %v, want Undetermined", i, got)
			}
		}
	})
}

func TestParseOpenCodeState(t *testing.T) {
	// EXHAUSTIVE over the claude-style ladder (Working-positive, Idle-default).
	// Fixtures use OpenCode's real rendered markers (source-verified, sst/opencode):
	// permission.tsx Permission required/Allow once; prompt/index.tsx esc interrupt /
	// [⋯] / [retrying; error-component.tsx A fatal error occurred!. The animated
	// spinner glyph is intentionally NOT a marker.
	//
	// LIVE-CAPTURE (surface-driver-opencode §4, opencode v1.3.15 — the RELEASED build,
	// not the survey's HEAD — against local ollama, $0, via `tmux capture-pane -p`):
	//   - Idle    : composer "Ask anything..." + footer "tab agents  ctrl+p commands"
	//               + status "…:master  1.3.15", NO working marker → Idle (default)   [VALIDATED]
	//   - Working : "⬝⬝⬝■■■■■  esc interrupt" — the `esc interrupt` text PERSISTS the
	//               whole non-idle duration, then vanishes when the turn finishes; this
	//               empirically confirms claude-style polarity (no mid-stream gap)       [VALIDATED]
	//   - running opencode's pane_current_command == "opencode" → IsShell=false          [VALIDATED]
	//   - the spinner (⬝/■) is an animated cycling glyph → correctly NOT a marker        [VALIDATED]
	// AwaitingApproval (Permission required / Allow once / Allow always, permission.tsx:
	// 391,404,407) and Errored (A fatal error occurred!, error-component.tsx:65) are
	// SOURCE-VERIFIED (re-confirmed this session) but were NOT live-elicited: the local
	// 1.5b/7b ollama models did not reliably invoke tools through the framework (they
	// printed tool-call JSON as text), and the capture client's CPR-dependency ended the
	// session before a tool-calling turn reached the permission gate. Follow-up tracked in #54:
	// live-elicit the permission dialog with a reliable tool-calling model +
	// `permission:{edit:"ask"}` to lock these two markers (low risk — they are specific
	// source-cited UI literals; all other states are live-validated).
	cases := []struct {
		name     string
		captured string
		want     State
	}{
		{
			name:     "permission dialog header → AwaitingApproval",
			captured: "Edit calc.py\n  - return a+b\n  + return a + b\nPermission required\nAllow once  Allow always  Reject",
			want:     StateAwaitingApproval,
		},
		{
			name:     "bash permission ($ command) → AwaitingApproval",
			captured: "Shell command\n$ go test ./...\nAllow once  Allow always  Reject\n△ 1 Permission",
			want:     StateAwaitingApproval,
		},
		{
			name:     "approval co-rendered with a working hint → AwaitingApproval (precedence)",
			captured: "Permission required\nAllow once  Reject\nesc interrupt",
			want:     StateAwaitingApproval,
		},
		{
			name:     "esc interrupt hint → Working",
			captured: "Thinking\n⠹\nesc interrupt",
			want:     StateWorking,
		},
		{
			name:     "post-esc 'again to interrupt' → Working",
			captured: "Thinking\nesc again to interrupt",
			want:     StateWorking,
		},
		{
			name:     "animations-disabled [⋯] indicator → Working",
			captured: "Thinking\n[⋯]\nesc interrupt",
			want:     StateWorking,
		},
		{
			name:     "retry backoff line → Working (self-healing)",
			captured: "model error [retrying in 2s attempt #1]\nesc interrupt",
			want:     StateWorking,
		},
		{
			name:     "fatal error boundary → Errored",
			captured: "A fatal error occurred!\nPlease report an issue.\nReset TUI  Exit",
			want:     StateErrored,
		},
		{
			name:     "idle composer (no marker) → Idle (the claude-style default)",
			captured: "Ask anything...\n/status  /help",
			want:     StateIdle,
		},
		{
			name:     "empty capture string → Idle (the classifier default; capture ERRORS are handled in Assess → Unknown)",
			captured: "",
			want:     StateIdle,
		},
		{
			// Torn/partial working frame (systems-review LOW-1): a mid-repaint capture
			// missing the esc-interrupt line reads Idle. Documented residual = the generic
			// scrape-torn-frame risk shared with claude-code (same no-Working→Idle debounce).
			// The esc-interrupt line is a single text node, so capture-pane emits it
			// atomically in practice (confirmed by live capture); a fully torn frame is rare.
			name:     "torn working frame (no marker captured) → Idle (documented residual)",
			captured: "Thinking",
			want:     StateIdle,
		},
		{
			// Tail-scoping: a stale approval far above the visible bottom (pushed past the
			// non-empty tail window) does not trigger AwaitingApproval; the live bottom is idle.
			name:     "stale approval pushed above the tail window → Idle",
			captured: "Permission required\nAllow once\n" + manyLines(22) + "Ask anything...",
			want:     StateIdle,
		},
		{
			// MEDIUM-1 regression: a model response that QUOTES a marker ("Allow once")
			// in the conversation area (above the bottom chrome) must NOT false-trigger
			// AwaitingApproval — the scan is anchored to the last non-empty lines (the
			// footer/composer chrome), not the whole frame.
			name:     "model output quoting 'Allow once' high up + idle composer below → Idle",
			captured: "The permission dialog has an \"Allow once\" button you can click.\n" + manyLines(14) + "Ask anything...\ntab agents  ctrl+p commands",
			want:     StateIdle,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseOpenCodeState(tc.captured); got != tc.want {
				t.Errorf("parseOpenCodeState = %v, want %v", got, tc.want)
			}
		})
	}
}

// manyLines returns n newline-separated filler lines (no markers), to push earlier
// content out of the tail window in tail-scoping tests.
func manyLines(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += "filler conversation line\n"
	}
	return s
}

func TestOpenCodeAssess(t *testing.T) {
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
		{"isShell → shell (opencode process gone)", "bash", nil, true, "", nil, StateShell},
		{"capture error → unknown (NOT a false finished-a-turn; like aider)", "bun", nil, false, "", boom, StateUnknown},
		{"classifier routes: approval", "bun", nil, false, "Permission required\nAllow once", nil, StateAwaitingApproval},
		{"classifier routes: working", "bun", nil, false, "Thinking\nesc interrupt", nil, StateWorking},
		{"classifier routes: errored", "bun", nil, false, "A fatal error occurred!", nil, StateErrored},
		{"classifier routes: idle", "bun", nil, false, "Ask anything...", nil, StateIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := openCode{
				paneCommand: func(string) (string, error) { return tc.cmd, tc.cmdErr },
				isShell:     func(string) bool { return tc.isShell },
				capturePane: func(string) (string, error) { return tc.captured, tc.captureErr },
				classify:    parseOpenCodeState,
			}
			if got := c.Assess("0:0.0"); got != tc.want {
				t.Errorf("Assess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOpenCodeSubmitRotateRoute(t *testing.T) {
	var submitted bool
	var injectedCmd string
	c := openCode{
		send:   func(pane, text string) error { submitted = true; return nil },
		inject: func(pane, cmd string) error { injectedCmd = cmd; return nil },
	}
	if err := c.Submit("0:0.0", "hi"); err != nil || !submitted {
		t.Errorf("Submit routed=%v err=%v, want routed to send", submitted, err)
	}
	if err := c.Rotate("0:0.0"); err != nil || injectedCmd != "/clear" {
		t.Errorf("Rotate injected %q err=%v, want /clear", injectedCmd, err)
	}
	if c.RotateStrategy() != SlashCommand {
		t.Errorf("opencode RotateStrategy = %v, want SlashCommand", c.RotateStrategy())
	}
	if newOpenCode().Name() != "opencode" {
		t.Error("newOpenCode().Name() != opencode")
	}
}
