package surface

import (
	"errors"
	"os"
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

func TestRegisteredEmptyAndUnknown(t *testing.T) {
	if Registered("") {
		t.Error(`Registered("") = true, want false (empty is not a driver name)`)
	}
	if Registered("not-a-driver") {
		t.Error(`Registered("not-a-driver") = true, want false`)
	}
	if !Registered("grok") || !Registered("codex") {
		t.Fatal("Registered(grok/codex) = false, want true")
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
		"codex":       "codex",
		"pi":          "pi",
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
	for _, name := range []string{"claude-code", "aider", "opencode", "grok", "codex", "pi"} {
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
func (d *recordingDriver) Close(string) error          { return nil }

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
		{"busy spinner → working", "node", nil, false, "✻ Frosting… (3s · ↓ 25 tokens)\n❯ ", nil, StateWorking},
		{"legacy esc-to-interrupt prose → idle", "node", nil, false, "doing\nesc to interrupt\n❯ ", nil, StateIdle},
		{"idle composer → idle", "node", nil, false, "❯ \n  ⏵⏵ auto mode on", nil, StateIdle},
		{"model-limit banner with idle composer → errored", "node", nil, false, "You've reached your model limit for this session.\nYou're out of usage credits.\n❯ ", nil, StateErrored},
		{"model-limit choice chrome → awaiting-input", "node", nil, false, "You've reached your model limit\n  1. Switch model\n  2. Exit\nEnter to confirm", nil, StateAwaitingInput},
		{"worktree-exit prompt → awaiting-input", "node", nil, false, "Exiting worktree session\n  1. Keep worktree\n  2. Remove worktree\nEnter to confirm", nil, StateAwaitingInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := claudeCode{
				paneCommand: func(string) (string, error) { return tc.cmd, tc.cmdErr },
				isShell:     func(string) bool { return tc.isShell },
				capturePane: func(string) (string, error) { return tc.captured, tc.captureErr },
				parseBusyAt: deliver.ParseBusyAt,
				cursorState: func(string) (int, bool, error) {
					lines := strings.Split(strings.TrimRight(tc.captured, "\n"), "\n")
					return len(lines) - 1, false, nil
				},
			}
			if got := c.Assess("0:0.0"); got != tc.want {
				t.Errorf("Assess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClaudeAssessHistoricalSpinnerPromptPairIsNotWorking(t *testing.T) {
	captured := "✻ Historical… (3s · ↓ 25 tokens)\n❯ old prompt\n● current response"
	c := claudeCode{
		paneCommand: func(string) (string, error) { return "node", nil },
		isShell:     func(string) bool { return false },
		capturePane: func(string) (string, error) { return captured, nil },
		parseBusyAt: deliver.ParseBusyAt,
		cursorState: func(string) (int, bool, error) { return 2, false, nil },
	}
	if got := c.Assess("0:0.0"); got != StateIdle {
		t.Fatalf("historical spinner+prompt pair Assess = %v, want idle", got)
	}
	c.cursorState = func(string) (int, bool, error) { return 0, false, errors.New("cursor unavailable") }
	if got := c.Assess("0:0.0"); got != StateIdle {
		t.Fatalf("cursor-error degraded arm Assess = %v, want idle", got)
	}
	c.cursorState = func(string) (int, bool, error) { return 1, true, nil }
	if got := c.Assess("0:0.0"); got != StateIdle {
		t.Fatalf("copy-mode degraded arm Assess = %v, want idle", got)
	}
}

func TestClaudeAssessStructuralInterruptStatusExactStates(t *testing.T) {
	working := "✻ Deliberating… (3m 14s · thinking)\none\ntwo\nthree\nfour\nfive\nsix\nseven\n" +
		"──────────────────────── cos ──\n❯ \n──────────────────────────────\n" +
		"  ⏵⏵ auto mode on (shift+tab to cycle) · esc to interrupt · ctrl+t to hide tasks · ← for agents"
	c := claudeCode{
		paneCommand: func(string) (string, error) { return "node", nil },
		isShell:     func(string) bool { return false },
		capturePane: func(string) (string, error) { return working, nil },
		parseBusyAt: deliver.ParseBusyAt,
		cursorState: func(string) (int, bool, error) { return 9, false, nil },
	}
	if got := c.Assess("0:0.0"); got != StateWorking {
		t.Fatalf("structural interrupt status Assess = %v, want Working", got)
	}

	quoted := "● The report quotes ⏵⏵ auto mode on (shift+tab to cycle) · esc to interrupt · ctrl+t to hide tasks · ← for agents\n❯ "
	c.capturePane = func(string) (string, error) { return quoted, nil }
	c.cursorState = func(string) (int, bool, error) { return 1, false, nil }
	if got := c.Assess("0:0.0"); got != StateIdle {
		t.Fatalf("quoted interrupt-status prose Assess = %v, want Idle", got)
	}
}

func TestClaudeAssessGenuineNBSPComposerFixture(t *testing.T) {
	fixture, err := os.ReadFile("../deliver/testdata/working-cos.txt")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(fixture), "\n"), "\n")
	if got, want := len(lines), 63; got != want {
		t.Fatalf("working-cos.txt rows = %d, want %d", got, want)
	}
	if got, want := lines[51], "✢ Diagnosing the fleet delivery blockage… (18m 1s · ↓ 18.6k tokens)"; got != want {
		t.Fatalf("spinner row = %q, want %q", got, want)
	}
	if got, want := lines[60], "❯\u00a0"; got != want {
		t.Fatalf("composer row bytes = % x, want % x", []byte(got), []byte(want))
	}
	if got, want := lines[62], "  ⏵⏵ auto mode on (shift+tab to cycle) · esc to interrupt · ctrl+t to hide tasks · ← for agents"; got != want {
		t.Fatalf("status row = %q, want %q", got, want)
	}
	const cursorY = 60
	if got := deliver.ParseBusyAt(string(fixture), cursorY); !got {
		t.Fatalf("ParseBusyAt(genuine fixture, %d) = %v, want true", cursorY, got)
	}

	c := claudeCode{
		paneCommand: func(string) (string, error) { return "node", nil },
		isShell:     func(string) bool { return false },
		capturePane: func(string) (string, error) { return string(fixture), nil },
		parseBusyAt: deliver.ParseBusyAt,
		cursorState: func(string) (int, bool, error) { return cursorY, false, nil },
	}
	if got := c.Assess("0:0.0"); got != StateWorking {
		t.Fatalf("Assess(genuine fixture) = %v, want exact state %v", got, StateWorking)
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
