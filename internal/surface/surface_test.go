package surface

import (
	"errors"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/deliver"
)

func TestRegistryDefaultAndUnknown(t *testing.T) {
	// Empty name and the explicit name both resolve to the claude-code driver;
	// an unknown name is not-ok (callers turn this into a clear startup error).
	if d, ok := Get(""); !ok || d.Name() != "claude-code" {
		t.Errorf(`Get("") = (%v, %v), want the claude-code driver`, d, ok)
	}
	if d, ok := Get("claude-code"); !ok || d.Name() != "claude-code" {
		t.Errorf(`Get("claude-code") = (%v, %v), want the claude-code driver`, d, ok)
	}
	if _, ok := Get("nope"); ok {
		t.Error(`Get("nope") = ok, want not-ok (unknown surface)`)
	}
}

func TestMixedHarnessFleetRoutesPerDriver(t *testing.T) {
	// The inter-harness guarantee (pillar B): a roster mixing harnesses resolves EACH
	// agent's surface to ITS OWN driver, so send/inject/Assess route per-driver across a
	// mixed fleet. (send/watch resolve via surface.Get(agent.Surface) — main.go:235,
	// watch.go:122,216 — proven live this session with an aider+opencode fleet + opencode
	// XO; this locks the registry side of that guarantee.) cursor joins when it ships.
	want := map[string]string{
		"":            "claude-code", // empty surface → default
		"claude-code": "claude-code",
		"aider":       "aider",
		"opencode":    "opencode",
		"grok":        "grok",
	}
	seen := map[string]bool{}
	for surface, wantName := range want {
		d, ok := Get(surface)
		if !ok {
			t.Errorf("Get(%q) not ok — a mixed roster would fail to drive this desk", surface)
			continue
		}
		if d.Name() != wantName {
			t.Errorf("Get(%q).Name() = %q, want %q (mis-routed driver)", surface, d.Name(), wantName)
		}
		seen[d.Name()] = true
	}
	// Distinct harnesses resolve to distinct drivers (no collapse to a single driver).
	for _, name := range []string{"claude-code", "aider", "opencode", "grok"} {
		if !seen[name] {
			t.Errorf("driver %q was never resolved — the mixed fleet is missing a harness", name)
		}
	}
}

// recordingDriver is a stub used to prove the rotate guard never injects into a
// RestartProcess surface.
type recordingDriver struct {
	strategy    Strategy
	submitCalls int
	rotateCalls int
}

func (d *recordingDriver) Name() string                { return "recording" }
func (d *recordingDriver) Submit(string, string) error { d.submitCalls++; return nil }
func (d *recordingDriver) Assess(string) State         { return StateIdle }
func (d *recordingDriver) Rotate(string) error         { d.rotateCalls++; return nil }
func (d *recordingDriver) RotateStrategy() Strategy    { return d.strategy }

func TestRotateContextNeverInjectsIntoRestartSurface(t *testing.T) {
	// THE GUARD (XO ruling): a RestartProcess surface must NEVER be injected into
	// — RotateContext returns ErrRestartRequired and the driver's Rotate/Submit
	// are never called (a /clear into e.g. cursor-agent would be literal text).
	d := &recordingDriver{strategy: RestartProcess}
	err := RotateContext(d, "0:0.0")
	if !errors.Is(err, ErrRestartRequired) {
		t.Errorf("RestartProcess RotateContext err = %v, want ErrRestartRequired", err)
	}
	if d.rotateCalls != 0 || d.submitCalls != 0 {
		t.Errorf("RestartProcess surface was injected into: rotate=%d submit=%d, want 0/0", d.rotateCalls, d.submitCalls)
	}
}

func TestRotateContextSlashSurfaceInjects(t *testing.T) {
	// A SlashCommand surface IS rotated via its Rotate (which injects the reset).
	d := &recordingDriver{strategy: SlashCommand}
	if err := RotateContext(d, "0:0.0"); err != nil {
		t.Fatalf("SlashCommand RotateContext err = %v, want nil", err)
	}
	if d.rotateCalls != 1 {
		t.Errorf("SlashCommand surface rotate calls = %d, want 1", d.rotateCalls)
	}
}

func TestClaudeAssessParity(t *testing.T) {
	// EXHAUSTIVE parity with the prior watch-gate logic — EVERY branch. Uses the
	// REAL deliver.ParseBusy so the working/idle classification is honest.
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
		{"panecommand error → unknown (transient glitch, not a crash)", "", boom, false, "", nil, StateUnknown},
		{"isShell → shell", "bash", nil, true, "", nil, StateShell},
		{"capture error → unknown (#55: non-material, not a false finish)", "node", nil, false, "", boom, StateUnknown},
		{"busy spinner → working", "node", nil, false, "✻ Frosting… (3s · ↓ 25 tokens)", nil, StateWorking},
		{"esc-to-interrupt → working", "node", nil, false, "doing\nesc to interrupt", nil, StateWorking},
		{"idle composer → idle", "node", nil, false, "❯ \n  ⏵⏵ auto mode on", nil, StateIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := claudeCode{
				paneCommand: func(string) (string, error) { return tc.cmd, tc.cmdErr },
				isShell:     func(string) bool { return tc.isShell },
				capturePane: func(string) (string, error) { return tc.captured, tc.captureErr },
				parseBusy:   deliver.ParseBusy,
			}
			if got := c.Assess("0:0.0"); got != tc.want {
				t.Errorf("Assess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseComposerPending(t *testing.T) {
	// The composer-cleared signal: classify Claude Code's composer line from real captured render.
	// PENDING = a body the Enter has not taken; CLEARED = empty composer (submitted); UNDETERMINED
	// = no prompt line found (fall back to the spinner).
	const rule = "────────────────────────────────────────"
	// The real hydra-ops capture (2026-06-17): a CLEARED composer with the post-turn survey modal
	// rendered ABOVE it — the case that must not confuse the composer-line finder.
	hydraCleared := strings.Join([]string{
		"  Now — back to your original questions: the floor is yours.",
		"",
		"✻ Churned for 3m 34s",
		"",
		"● How is Claude doing this session? (optional)",
		"  1: Bad    2: Fine   3: Good   0: Dismiss",
		"",
		rule,
		"❯ ",
		rule,
		"  jim@rt-dgx-sp001:~ [Opus 4.8] ctx:35%                    /rc active",
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents",
	}, "\n")
	cases := []struct {
		name        string
		captured    string
		wantPending bool
		wantOK      bool
	}{
		{"empty composer → cleared", rule + "\n❯ \n" + rule + "\n  footer", false, true},
		{"single-line body → pending", rule + "\n❯ operator: are you there?\n" + rule, true, true},
		{"multi-line paste placeholder → pending", rule + "\n❯ [Pasted text +12 lines]\n" + rule, true, true},
		{"working render, composer empty → cleared", "· Ideating… (6m · ↓ 23k tokens)\n" + rule + "\n❯ \n" + rule, false, true},
		{"indented empty composer → cleared", "  ❯ \n  footer", false, true},
		{"no composer prompt in tail → undetermined", "jim@host:~$ \n  some shell output", false, false},
		{"real hydra-ops capture with survey modal → cleared", hydraCleared, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pending, ok := parseComposerPending(tc.captured)
			if pending != tc.wantPending || ok != tc.wantOK {
				t.Errorf("parseComposerPending = (pending=%v, ok=%v), want (pending=%v, ok=%v)", pending, ok, tc.wantPending, tc.wantOK)
			}
		})
	}
}

func TestClaudeComposerPendingRoutesCaptureError(t *testing.T) {
	// A capture error reads as UNDETERMINED (ok=false) so confirmation falls back to the spinner
	// rather than misreading a glitch as "cleared".
	boom := errors.New("tmux capture boom")
	c := claudeCode{capturePane: func(string) (string, error) { return "", boom }}
	if pending, ok := c.ComposerPending("0:0.0"); pending || ok {
		t.Errorf("ComposerPending on capture error = (%v, %v), want (false, false)", pending, ok)
	}
	// And it routes a clean capture through the parser.
	c.capturePane = func(string) (string, error) { return "──\n❯ \n──", nil }
	if pending, ok := c.ComposerPending("0:0.0"); pending || !ok {
		t.Errorf("ComposerPending on empty composer = (%v, %v), want (false, true)", pending, ok)
	}
}

func TestClaudeSubmitAndRotateRoute(t *testing.T) {
	var submitted, rotated bool
	c := claudeCode{
		send:  func(pane, text string) error { submitted = true; return nil },
		clear: func(pane string) error { rotated = true; return nil },
	}
	if err := c.Submit("0:0.0", "hi"); err != nil || !submitted {
		t.Errorf("Submit routed=%v err=%v, want routed to send", submitted, err)
	}
	if err := c.Rotate("0:0.0"); err != nil || !rotated {
		t.Errorf("Rotate routed=%v err=%v, want routed to clear (/clear)", rotated, err)
	}
	if c.RotateStrategy() != SlashCommand {
		t.Errorf("claude RotateStrategy = %v, want SlashCommand", c.RotateStrategy())
	}
	if newClaudeCode().Name() != "claude-code" {
		t.Error("newClaudeCode().Name() != claude-code")
	}
}
