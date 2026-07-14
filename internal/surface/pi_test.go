package surface

import (
	"errors"
	"os"
	"testing"
)

func TestPiRegistered(t *testing.T) {
	d, ok := Get("pi")
	if !ok || d.Name() != "pi" {
		t.Errorf(`Get("pi") = (%v, %v), want the pi driver`, d, ok)
	}
	var _ ComposerStateProbe = pi{}
}

func piFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPiLiveMarkerFixtures(t *testing.T) {
	if got := parsePiState(piFixture(t, "pi-0.73.1-idle.txt")); got != StateIdle {
		t.Fatalf("idle fixture = %v, want Idle", got)
	}
	if got := parsePiState(piFixture(t, "pi-0.73.1-working.txt")); got != StateWorking {
		t.Fatalf("working fixture = %v, want Working", got)
	}
}

func TestClassifyPiComposerLine(t *testing.T) {
	rule := "────────────────────────────────────────"
	cases := []struct {
		name, captured string
		x, y           int
		want           ComposerDisposition
	}{
		{"cleared", "output\n" + rule + "\n\n" + rule + "\n/path\nmodel • high", 0, 2, ComposerCleared},
		{"pending", "output\n" + rule + "\nbeta-probe\n" + rule + "\n/path\nmodel • high", 10, 2, ComposerPending},
		{"displaced cursor", "output\n" + rule + "\n\n" + rule + "\n/path\nmodel • high", 3, 2, ComposerUndetermined},
		{"quoted rules above output", rule + "\n\n" + rule + "\nfiller\noutput\n/path\nmodel • high", 0, 1, ComposerUndetermined},
		{"changed glyph fails closed", "output\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n/path\nmodel • high", 0, 2, ComposerUndetermined},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyPiComposerLine(tc.captured, tc.x, tc.y); got != tc.want {
				t.Fatalf("classifyPiComposerLine = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPiComposerState(t *testing.T) {
	idle := piFixture(t, "pi-0.73.1-idle.txt")
	p := pi{
		cursorSnapshot: func(string) (int, int, bool, bool, error) { return 0, 6, false, false, nil },
		capturePane:    func(string) (string, error) { return idle, nil },
		classify:       parsePiState,
	}
	if got := p.ComposerState("pane"); got != ComposerCleared {
		t.Fatalf("ComposerState = %v, want Cleared", got)
	}
	p.cursorSnapshot = func(string) (int, int, bool, bool, error) { return 0, 0, false, true, nil }
	if got := p.ComposerState("pane"); got != ComposerUndetermined {
		t.Fatalf("copy-mode ComposerState = %v, want Undetermined", got)
	}
}

func TestParsePiState(t *testing.T) {
	// Fixtures use LIVE-CAPTURED markers from pi 0.73.1 canary 2026-07-14
	// (OpenCode Go / kimi-k2.6) via tmux capture-pane -p -J, plus source-verified
	// retry/error phrases from interactive-mode.js.
	//
	// LIVE-CAPTURE CONFIRMED:
	//   - Idle    : banner has "escape interrupt" but NO "Working..." → Idle
	//   - Working : "  ⠹ Working..." (spinner + Working...) above composer border
	//   - Done    : Working... gone; model reply visible → Idle
	// Errored (auth missing) is source-verified; not live-induced this slice.
	cases := []struct {
		name     string
		captured string
		want     State
	}{
		{
			name: "idle banner with static escape interrupt help → Idle (NOT working)",
			// The startup banner always shows "escape interrupt" while idle.
			// Matching that would false-positive every idle frame.
			captured: "" +
				" pi v0.73.1\n" +
				" escape interrupt · ctrl+c/ctrl+d clear/exit · / commands · ! bash · ctrl+o\n" +
				" more\n" +
				" Press ctrl+o to show full startup help and loaded resources.\n" +
				"\n" +
				"────────────────────────────────────────────────────────────────────────────────\n" +
				"\n" +
				"────────────────────────────────────────────────────────────────────────────────\n" +
				"/tmp/pi-canary\n" +
				"0.0%/262k (auto)                                        kimi-k2.6 • thinking off\n",
			want: StateIdle,
		},
		{
			name: "live working spinner → Working",
			captured: "" +
				" pi v0.73.1\n" +
				" escape interrupt · ctrl+c/ctrl+d clear/exit · / commands\n" +
				" Reply with exactly the word pong and nothing else.\n" +
				"\n" +
				" ⠹ Working...\n" +
				"────────────────────────────────────────────────────────────────────────────────\n" +
				"\n" +
				"────────────────────────────────────────────────────────────────────────────────\n" +
				"/tmp/pi-canary\n" +
				"0.0%/262k (auto)                                        kimi-k2.6 • thinking off\n",
			want: StateWorking,
		},
		{
			name: "working with streamed partial thinking still Working",
			captured: "" +
				" Reply with exactly the word pong and nothing else.\n" +
				"\n" +
				" The user wants me to reply with exactly the\n" +
				" ⠼ Working...\n" +
				"────────────────────────────────────────────────────────────────────────────────\n" +
				" kimi-k2.6 • thinking off\n",
			want: StateWorking,
		},
		{
			name: "turn complete (Working... gone) → Idle",
			captured: "" +
				" Reply with exactly the word pong and nothing else.\n" +
				"\n" +
				" The user wants me to reply with exactly the word \"pong\".\n" +
				" pong\n" +
				"────────────────────────────────────────────────────────────────────────────────\n" +
				"\n" +
				"────────────────────────────────────────────────────────────────────────────────\n" +
				"↑597 ↓27 R512 $0.001 0.4%/262k (auto)                   kimi-k2.6 • thinking off\n",
			want: StateIdle,
		},
		{
			name:     "retry countdown → Working (self-healing, not Errored)",
			captured: "litellm.RateLimitError\nRetrying (1/3) in 4s... (escape to cancel)\n",
			want:     StateWorking,
		},
		{
			name:     "no models / auth missing → Errored",
			captured: "No models available. Use /login to log into a provider via OAuth or API key.\n",
			want:     StateErrored,
		},
		{
			name: "stale Working... scrolled out of tail → Idle",
			// 13 non-empty lines after a historical Working... — only last 12 matter.
			captured: "" +
				"old Working...\n" +
				"line1\nline2\nline3\nline4\nline5\nline6\n" +
				"line7\nline8\nline9\nline10\nline11\nline12\n",
			want: StateIdle,
		},
		{
			name: "quoted Working... in model output still in tail → Working (conservative)",
			// Streamed output that quotes the marker while still mid-turn is fine
			// as Working; if it persists after idle, that is a future false-positive
			// risk — tail scoping + live canary showed Working... disappears on idle.
			captured: "I saw the text Working... in the docs.\nstill streaming\n",
			want:     StateWorking,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePiState(tc.captured); got != tc.want {
				t.Fatalf("parsePiState = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPiAssessWiring(t *testing.T) {
	t.Run("shell foreground → StateShell", func(t *testing.T) {
		p := pi{
			paneCommand: func(string) (string, error) { return "bash", nil },
			isShell:     func(string) bool { return true },
			capturePane: func(string) (string, error) { t.Fatal("capture should not run on shell"); return "", nil },
			classify:    parsePiState,
		}
		if got := p.Assess("pane"); got != StateShell {
			t.Fatalf("Assess = %v, want Shell", got)
		}
	})
	t.Run("pane-command error → Unknown", func(t *testing.T) {
		p := pi{
			paneCommand: func(string) (string, error) { return "", errors.New("tmux glitch") },
			isShell:     func(string) bool { return false },
			classify:    parsePiState,
		}
		if got := p.Assess("pane"); got != StateUnknown {
			t.Fatalf("Assess = %v, want Unknown", got)
		}
	})
	t.Run("capture error → Unknown (not Idle)", func(t *testing.T) {
		p := pi{
			paneCommand: func(string) (string, error) { return "node", nil },
			isShell:     func(string) bool { return false },
			capturePane: func(string) (string, error) { return "", errors.New("capture fail") },
			classify:    parsePiState,
		}
		if got := p.Assess("pane"); got != StateUnknown {
			t.Fatalf("Assess = %v, want Unknown", got)
		}
	})
	t.Run("live idle fixture → Idle", func(t *testing.T) {
		p := pi{
			paneCommand: func(string) (string, error) { return "node", nil },
			isShell:     func(string) bool { return false },
			capturePane: func(string) (string, error) {
				return " escape interrupt · ctrl+c\n────────────────────────────────\n\n────────────────────────────────\n", nil
			},
			classify: parsePiState,
		}
		if got := p.Assess("pane"); got != StateIdle {
			t.Fatalf("Assess = %v, want Idle", got)
		}
	})
}

func TestPiRotateAndClose(t *testing.T) {
	var injected string
	p := pi{
		inject: func(_ string, cmd string) error { injected = cmd; return nil },
	}
	if err := p.Rotate("pane"); err != nil {
		t.Fatalf("Rotate = %v", err)
	}
	if injected != "/new" {
		t.Fatalf("Rotate injected %q, want /new", injected)
	}
	if p.RotateStrategy() != SlashCommand {
		t.Fatalf("RotateStrategy = %v, want SlashCommand", p.RotateStrategy())
	}
	if err := p.Close("pane"); !errors.Is(err, ErrNoGracefulClose) {
		t.Fatalf("Close = %v, want ErrNoGracefulClose", err)
	}
}

func TestPiSubmitUsesSend(t *testing.T) {
	var gotPane, gotText string
	p := pi{
		send: func(pane, text string) error {
			gotPane, gotText = pane, text
			return nil
		},
	}
	if err := p.Submit("0:1.0", "hello"); err != nil {
		t.Fatalf("Submit = %v", err)
	}
	if gotPane != "0:1.0" || gotText != "hello" {
		t.Fatalf("Submit wired send(%q, %q), want (0:1.0, hello)", gotPane, gotText)
	}
}
