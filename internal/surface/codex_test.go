package surface

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCodexRegistered(t *testing.T) {
	d, ok := Get("codex")
	if !ok || d.Name() != "codex" {
		t.Errorf(`Get("codex") = (%v, %v), want the codex driver`, d, ok)
	}
}

func TestParseCodexState(t *testing.T) {
	// Login fixture LIVE-CAPTURED 2026-07-02 from codex-cli 0.142.5 (unauthenticated welcome menu).
	loginCapture := "  Welcome to Codex, OpenAI's command-line coding agent\n\n" +
		"  Sign in with ChatGPT to use Codex as part of your paid plan\n\n" +
		"> 1. Sign in with ChatGPT\n" +
		"     Usage included with Plus, Pro, Business, and Enterprise plans\n\n" +
		"  2. Sign in with Device Code\n" +
		"  3. Provide your own API key\n\n" +
		"  Press enter to continue"

	cases := []struct {
		name     string
		captured string
		want     State
	}{
		{
			name:     "live login welcome menu → AwaitingInput",
			captured: loginCapture,
			want:     StateAwaitingInput,
		},
		{
			name:     "hooks trust gate (binary) → AwaitingInput",
			captured: "  Hooks need review\n  Press enter to continue\n  Trust all and continue",
			want:     StateAwaitingInput,
		},
		{
			name: "hooks trust gate LIVE 2026-07-03 post-auth → AwaitingInput",
			captured: "  Hooks need review\n  4 hooks are new or changed.\n" +
				"  Press enter to confirm or esc to go back\n  Trust all and continue",
			want: StateAwaitingInput,
		},
		{
			name:     "approval modal Action Required (binary) → AwaitingApproval",
			captured: "  [ ! ] Action Required\n  Approve for me\n  Decline this request",
			want:     StateAwaitingApproval,
		},
		{
			name: "on-request shell approval LIVE 2026-07-03 → AwaitingApproval",
			captured: "  ◦ Running printf '%s\n" +
				"  Would you like to run the following command?\n" +
				"  › 1. Yes, proceed (y)\n  Press enter to confirm or esc to cancel",
			want: StateAwaitingApproval,
		},
		{
			name:     "main needs approval status → AwaitingApproval",
			captured: "  review started: main needs input\n  Approve for me\n  /status",
			want:     StateAwaitingApproval,
		},
		{
			name:     "footer interrupt hint (binary) → Working",
			captured: "  streaming output above\n  esc to interrupt\n  /status",
			want:     StateWorking,
		},
		{
			name: "working spinner LIVE 2026-07-03 → Working",
			captured: "  › Reply with exactly PONG and nothing else.\n" +
				"  ◦ Working (0s • esc to interrupt)\n  › Find and fix a bug in @filename",
			want: StateWorking,
		},
		{
			name:     "task in progress guard → Working",
			captured: "  Ctrl+L is disabled while a task is in progress.\n  │ composer │",
			want:     StateWorking,
		},
		{
			name:     "waiting for background terminal → Working",
			captured: "  Waiting for background terminal\n  job still running",
			want:     StateWorking,
		},
		{
			name:     "idle empty composer → Idle (default)",
			captured: "  Turn done.\n  › \n  / for commands",
			want:     StateIdle,
		},
		{
			name:     "passive limit banner above ready composer → Idle",
			captured: codexLimitBannerCapture,
			want:     StateIdle,
		},
		{
			name:     "model selector overlay → AwaitingInput",
			captured: codexModelSelectorCapture,
			want:     StateAwaitingInput,
		},
		{
			name:     "rate-limit selector overlay → AwaitingInput",
			captured: codexRateLimitOverlayCapture,
			want:     StateAwaitingInput,
		},
		{
			name: "post-turn idle LIVE 2026-07-03 → Idle",
			captured: "  • PONG\n  › Find and fix a bug in @filename\n" +
				"  gpt-5.5 default · ~/workspace/…/example-repo",
			want: StateIdle,
		},
		{
			name:     "empty capture → Idle",
			captured: "",
			want:     StateIdle,
		},
		{
			name:     "stale working marker in scrollback + idle below → Idle",
			captured: "  esc to interrupt\n" + manyLines(14) + "  › \n  / for commands",
			want:     StateIdle,
		},
		{
			name:     "login markers outside 12-line tail but inside startup window → AwaitingInput",
			captured: "  Welcome to Codex\n  Sign in with ChatGPT\n" + manyLines(15),
			want:     StateAwaitingInput,
		},
		{
			name:     "stale login scrollback + idle composer at bottom → Idle",
			captured: loginCapture + manyLines(50) + "  › \n  / for commands",
			want:     StateIdle,
		},
		{
			name:     "first-run directory-trust menu (source snapshot 0.144.1) → AwaitingInput",
			captured: codexTrustMenuCapture,
			want:     StateAwaitingInput,
		},
		{
			name:     "first-run update menu (source snapshot 0.144.1) → AwaitingInput",
			captured: codexUpdateMenuCapture,
			want:     StateAwaitingInput,
		},
		{
			name:     "stale trust-menu scrollback + idle composer at bottom → Idle",
			captured: codexTrustMenuCapture + "\n" + manyLines(50) + "  › \n  / for commands",
			want:     StateIdle,
		},
		{
			name:     "trust question alone (conversation echo, no option row) → Idle",
			captured: "  the menu asks Do you trust the contents of this directory\n  › \n  / for commands",
			want:     StateIdle,
		},
		{
			// A working desk DISPLAYING this driver's own marker strings (source
			// code, diffs, fixtures — this repo dogfoods codex desks on itself)
			// must not misread as a first-run menu: quoted/assigned forms are not
			// whole rendered option rows, so the line-anchored row match rejects
			// them and the working footer wins.
			name: "working desk displaying marker source → Working (not menu)",
			captured: "  codexTrustQuestion = \"Do you trust the contents\"\n" +
				"  codexTrustYesRow   = \"1. Yes, continue\"\n" +
				"  codexUpdateBanner  = \"Update available!\"\n" +
				"  codexUpdateSkipRow = \"3. Skip until next version\"\n" +
				"  ◦ Working (3s • esc to interrupt)",
			want: StateWorking,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCodexState(tc.captured); got != tc.want {
				t.Errorf("parseCodexState = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCodexAssess(t *testing.T) {
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
		{"isShell → shell", "bash", nil, true, "", nil, StateShell},
		{"capture error → unknown", "codex", nil, false, "", boom, StateUnknown},
		{"classifier routes: login", "codex", nil, false, "Welcome to Codex\nSign in with ChatGPT", nil, StateAwaitingInput},
		{"classifier routes: working", "codex", nil, false, "esc to interrupt", nil, StateWorking},
		{"classifier routes: idle", "codex", nil, false, "› \n/ for commands", nil, StateIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := codex{
				paneCommand: func(string) (string, error) { return tc.cmd, tc.cmdErr },
				isShell:     func(string) bool { return tc.isShell },
				capturePane: func(string) (string, error) { return tc.captured, tc.captureErr },
				classify:    parseCodexState,
			}
			if got := c.Assess("0:0.0"); got != tc.want {
				t.Errorf("Assess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCodexSubmitRotateRoute(t *testing.T) {
	var submitted bool
	var injectedCmd string
	c := codex{
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
		t.Errorf("codex RotateStrategy = %v, want SlashCommand", c.RotateStrategy())
	}
	if newCodex().Name() != "codex" {
		t.Error("newCodex().Name() != codex")
	}
}

var (
	_ ResultReader       = codex{}
	_ ReplyReader        = codex{}
	_ RecycleBridge      = codex{}
	_ ComposerStateProbe = codex{}
)

func TestClassifyCodexComposerLine(t *testing.T) {
	cases := []struct {
		name     string
		captured string
		cursorX  int
		cursorY  int
		want     ComposerDisposition
	}{
		{"empty › prompt → Cleared", "  Turn done.\n  › \n  / for commands", 4, 1, ComposerCleared},
		{"pending body after › → Pending", "  › draft in composer\n  / for commands", 22, 0, ComposerPending},
		// LIVE-CAPTURED 2026-07-14 (#709): a human-idle desk renders the randomized
		// placeholder hint after "› " at column 0 with the cursor parked at the
		// body-start cell — an empty textarea, so idle∧cleared must hold.
		{"placeholder hint, cursor at body start → Cleared", "› Explain this codebase\ngpt-5.6-sol medium · ~/workspace", 2, 0, ComposerCleared},
		{"side placeholder, cursor at body start → Cleared", "› Will this algorithm scale well?\ngpt-5.6-sol medium · ~/workspace", 2, 0, ComposerCleared},
		{"indented placeholder LIVE 2026-07-03 render → Cleared", "  › Find and fix a bug in @filename\n  gpt-5.5 default", 4, 0, ComposerCleared},
		{"placeholder wording but cursor after text → Pending (typed draft)", "› Explain this codebase\ngpt-5.6-sol medium · ~/workspace", 23, 0, ComposerPending},
		{"unknown hint wording fails closed → Pending", "› Uncharacterized example\ngpt-5.6-sol medium · ~/workspace", 2, 0, ComposerPending},
		// A tab passes trimSpace's prompt cut but defeats the byte==cell equivalence
		// the placeholder arithmetic needs, so the hint check fails closed.
		{"tab-decorated prefix before › breaks cell math → Pending", "\t› Explain this codebase\ngpt-5.6-sol medium · ~/workspace", 3, 0, ComposerPending},
		{"model selector highlighted row → Undetermined, never Pending", codexModelSelectorCapture, 0, 2, ComposerUndetermined},
		{"rate-limit selector highlighted row → Undetermined, never Pending", codexRateLimitOverlayCapture, 0, 3, ComposerUndetermined},
		{"approval row without › → Undetermined", "  [ ! ] Action Required\n  Approve for me", 0, 0, ComposerUndetermined},
		{"cursor out of range → Undetermined", "  › \n", 0, 99, ComposerUndetermined},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyCodexComposerLine(tc.captured, tc.cursorX, tc.cursorY); got != tc.want {
				t.Errorf("classifyCodexComposerLine = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCodexComposerStateWiring(t *testing.T) {
	const cleared = "  › \n  / for commands"
	t.Run("idle cleared composer → Cleared", func(t *testing.T) {
		c := codex{
			cursorPosition: func(string) (int, int, bool, error) { return 4, 0, false, nil },
			capturePane:    func(string) (string, error) { return cleared, nil },
		}
		if got := c.ComposerState("0:0.0"); got != ComposerCleared {
			t.Errorf("ComposerState = %v, want Cleared", got)
		}
	})
	t.Run("passive limit banner preserves cleared composer", func(t *testing.T) {
		c := codex{
			cursorPosition: func(string) (int, int, bool, error) { return 4, 2, false, nil },
			capturePane:    func(string) (string, error) { return codexLimitBannerCapture, nil },
		}
		if got := c.ComposerState("0:0.0"); got != ComposerCleared {
			t.Errorf("ComposerState behind passive banner = %v, want Cleared", got)
		}
	})
	t.Run("cursor read error → Undetermined", func(t *testing.T) {
		c := codex{cursorPosition: func(string) (int, int, bool, error) { return 0, 0, false, errors.New("no server") }}
		if got := c.ComposerState("0:0.0"); got != ComposerUndetermined {
			t.Errorf("ComposerState = %v, want Undetermined", got)
		}
	})
	// The first-run menus render their highlighted row as "› 1. …" — the same
	// glyph as the composer prompt. A cursor on that row must NOT read Pending
	// (a Pending read drives the confirm loop's Enter-only retry, which would
	// SELECT the menu option); the whole screen is Undetermined.
	t.Run("cursor on trust-menu option row → Undetermined, not Pending", func(t *testing.T) {
		c := codex{
			cursorPosition: func(string) (int, int, bool, error) { return 0, 7, false, nil }, // "› 1. Yes, continue"
			capturePane:    func(string) (string, error) { return codexTrustMenuCapture, nil },
		}
		if got := c.ComposerState("0:0.0"); got != ComposerUndetermined {
			t.Errorf("ComposerState on trust menu = %v, want Undetermined", got)
		}
	})
	t.Run("cursor on update-menu option row → Undetermined, not Pending", func(t *testing.T) {
		c := codex{
			cursorPosition: func(string) (int, int, bool, error) { return 0, 4, false, nil }, // "› 1. Update now (…)"
			capturePane:    func(string) (string, error) { return codexUpdateMenuCapture, nil },
		}
		if got := c.ComposerState("0:0.0"); got != ComposerUndetermined {
			t.Errorf("ComposerState on update menu = %v, want Undetermined", got)
		}
	})
	t.Run("cursor on generic selector row → Undetermined, not ListNav", func(t *testing.T) {
		c := codex{
			cursorPosition: func(string) (int, int, bool, error) { return 0, 2, false, nil },
			capturePane:    func(string) (string, error) { return codexModelSelectorCapture, nil },
		}
		if got := c.ComposerState("0:0.0"); got != ComposerUndetermined {
			t.Errorf("ComposerState on selector = %v, want Undetermined", got)
		}
	})
}

func TestCodexRateLimitOverlayDiagnosticNamePendingLiveCapture690(t *testing.T) {
	if got := codexOverlayName(codexRateLimitOverlayCapture); got != "rate-limit overlay" {
		t.Fatalf("codexOverlayName = %q, want rate-limit overlay", got)
	}
	if got := codexOverlayName(codexModelSelectorCapture); got != "" {
		t.Fatalf("ordinary model selector diagnostic = %q, want unnamed structural overlay", got)
	}
}

// TestConfirmSubmitRefusesCodexFirstRunMenu proves the incident's false-confirm
// chain is closed END-TO-END, not just layer-by-layer: confirmed delivery
// against a codex pane showing the trust menu must refuse at the idle gate
// (ErrTransient) with ZERO keystrokes delivered — previously the paste+Enter
// navigated the menu, the menu dismissed to an empty composer, and the empty
// composer read as a confirmed submit.
func TestConfirmSubmitRefusesCodexSelectorMenus(t *testing.T) {
	for name, tc := range map[string]struct {
		capture string
		wantErr error
		wantMsg string
	}{
		"trust menu":         {codexTrustMenuCapture, ErrTransient, ""},
		"update menu":        {codexUpdateMenuCapture, ErrTransient, ""},
		"model selector":     {codexModelSelectorCapture, ErrTransient, ""},
		"rate-limit overlay": {codexRateLimitOverlayCapture, ErrPanelBlocked, "rate-limit overlay"},
	} {
		t.Run(name, func(t *testing.T) {
			pasted := false
			d := codex{
				paneCommand:    func(string) (string, error) { return "codex", nil },
				isShell:        func(string) bool { return false },
				capturePane:    func(string) (string, error) { return tc.capture, nil },
				classify:       parseCodexState,
				send:           func(string, string) error { pasted = true; return nil },
				cursorPosition: func(string) (int, int, bool, error) { return 0, 7, false, nil },
			}
			enters := 0
			c := Confirm{
				SendEnter: func(string) error { enters++; return nil },
				Sleep:     func(time.Duration) {},
			}
			err := c.Submit(d, "0:0.0", "operator message")
			if !errors.Is(err, tc.wantErr) || (tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg)) {
				t.Fatalf("Submit on %s = %v, want %v containing %q", name, err, tc.wantErr, tc.wantMsg)
			}
			if pasted || enters != 0 {
				t.Errorf("keystrokes reached the menu: pasted=%v enters=%d, want none", pasted, enters)
			}
		})
	}
}

// Synthetic frames use the same selector rows as the /model rendering captured
// during the restore sweep. Rate-limit prose remains pending live capture (#690);
// the structural selector guard does not depend on those inferred strings.
const codexModelSelectorCapture = "  5. gpt-5.4  Strong model for broad knowledge and reasoning\n" +
	"  Choose a model\n" +
	"› 6. gpt-5.4-mini (current)  Small, fast, and efficient\n" +
	"  Enter to select · Esc to cancel"

const codexRateLimitOverlayCapture = "  Approaching rate limits\n" +
	"  Switch to gpt-5.4-mini to keep working\n" +
	"  1. Keep current model\n" +
	"› 2. Switch to gpt-5.4-mini\n" +
	"  Enter to select · Esc to cancel"

const codexLimitBannerCapture = "  ⚠ Heads up, you have less than 10% of your weekly limit left.\n" +
	"  Run /status for a breakdown\n" +
	"  › \n" +
	"  / for commands"

// First-run menu fixtures, VERBATIM from the openai/codex rust-v0.144.1 rendered
// snapshot tests (tui/src/onboarding/snapshots/…trust_directory…renders_snapshot_
// for_git_repo.snap and tui/src/snapshots/…update_prompt…update_prompt_modal.snap).
const codexTrustMenuCapture = "> You are in /workspace/project\n" +
	"\n" +
	"  Do you trust the contents of this directory? Working with untrusted\n" +
	"  contents comes with higher risk of prompt injection. Trusting the\n" +
	"  directory allows project-local config, hooks, and exec policies to\n" +
	"  load.\n" +
	"\n" +
	"› 1. Yes, continue\n" +
	"  2. No, quit\n" +
	"\n" +
	"  Press enter to continue"

const codexUpdateMenuCapture = "  ✨ Update available! 0.0.0 -> 9.9.9\n" +
	"\n" +
	"  Release notes: https://github.com/openai/codex/releases/latest\n" +
	"\n" +
	"› 1. Update now (runs `npm install -g @openai/codex@latest`)\n" +
	"  2. Skip\n" +
	"  3. Skip until next version\n" +
	"\n" +
	"  Press enter to continue"

func TestCodexLatestResult(t *testing.T) {
	t.Run("resolves cwd then reads the store", func(t *testing.T) {
		c := codex{
			paneCWD:   func(string) (string, error) { return "/srv/fleet/backend", nil },
			codexHome: "/home/you/.codex",
			latestResult: func(home, cwd string) (string, error) {
				if home != "/home/you/.codex" || cwd != "/srv/fleet/backend" {
					t.Errorf("latestResult got (home=%q, cwd=%q)", home, cwd)
				}
				return "the full latest codex result", nil
			},
		}
		got, err := c.LatestResult("flotilla:5.0")
		if err != nil || got != "the full latest codex result" {
			t.Errorf("LatestResult = (%q, %v)", got, err)
		}
	})
	t.Run("empty codexHome → clear error", func(t *testing.T) {
		called := false
		c := codex{
			paneCWD:      func(string) (string, error) { return "/cwd", nil },
			codexHome:    "",
			latestResult: func(string, string) (string, error) { called = true; return "", nil },
		}
		if _, err := c.LatestResult("p"); err == nil {
			t.Error("want error when codexHome is empty")
		}
		if called {
			t.Error("store must not be consulted when codexHome is empty")
		}
	})
}
