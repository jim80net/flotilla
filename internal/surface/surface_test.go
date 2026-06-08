package surface

import (
	"errors"
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
		{"panecommand error → shell", "", boom, false, "", nil, StateShell},
		{"isShell → shell", "bash", nil, true, "", nil, StateShell},
		{"capture error → idle (fail-open)", "node", nil, false, "", boom, StateIdle},
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
