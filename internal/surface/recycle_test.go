package surface

import (
	"strings"
	"testing"
)

// TestClaudeHandoffPath: the designated path embeds the token under .claude/handoffs/.
func TestClaudeHandoffPath(t *testing.T) {
	c := newClaudeCode()
	got := c.HandoffPath("/home/jim/work/spark", "20260623T141530.000000001-a3f91b2c")
	want := "/home/jim/work/spark/.claude/handoffs/recycle-20260623T141530.000000001-a3f91b2c.md"
	if got != want {
		t.Errorf("HandoffPath = %q, want %q", got, want)
	}
}

// TestClaudeHandoffTurn: the handoff turn names the exact path, force-commits to the
// current branch, and is explicitly non-interactive / remote-driven (NOT the bare skill).
func TestClaudeHandoffTurn(t *testing.T) {
	c := newClaudeCode()
	path := "/repo/.claude/handoffs/recycle-tok.md"
	turn := c.HandoffTurn(path)
	for _, must := range []string{
		path,            // names the exact designated path
		"git add -f",    // force-commit (gitignored handoffs dir)
		"git commit",    // commits to the current branch
		"REMOTE-DRIVEN", // states it is remote-driven
		"stop",          // ends the turn (so Idle ∧ ComposerCleared becomes reachable)
	} {
		if !strings.Contains(turn, must) {
			t.Errorf("HandoffTurn missing %q\n--- turn ---\n%s", must, turn)
		}
	}
	// It must explicitly forbid the interactive skill / a confirmation (not invoke it).
	if !strings.Contains(turn, "Do NOT run the interactive") {
		t.Errorf("HandoffTurn must explicitly forbid the interactive /handoff skill")
	}
}

// TestClaudeTakeoverTurn: the takeover turn names the exact path, says begin-immediately /
// do-not-ask-to-start, and mandates flotilla-message parlay (never an in-pane prompt).
func TestClaudeTakeoverTurn(t *testing.T) {
	c := newClaudeCode()
	path := "/repo/.claude/handoffs/recycle-tok.md"
	turn := c.TakeoverTurn(path)
	for _, must := range []string{
		path,
		"BEGIN WORK IMMEDIATELY",
		"REMOTE-DRIVEN",
		"flotilla", // parlay via a flotilla message
	} {
		if !strings.Contains(turn, must) {
			t.Errorf("TakeoverTurn missing %q\n--- turn ---\n%s", must, turn)
		}
	}
	if !strings.Contains(turn, "shall I start") && !strings.Contains(turn, `"shall I start?"`) {
		t.Errorf("TakeoverTurn should explicitly override the skill's \"shall I start?\" pause")
	}
}

// TestHandoffTurnPathWithSpaces: a path with spaces is embedded verbatim (no truncation).
func TestHandoffTurnPathWithSpaces(t *testing.T) {
	c := newClaudeCode()
	path := "/home/jim/my work/.claude/handoffs/recycle-tok.md"
	if !strings.Contains(c.HandoffTurn(path), path) {
		t.Errorf("HandoffTurn dropped a spaced path")
	}
	if !strings.Contains(c.TakeoverTurn(path), path) {
		t.Errorf("TakeoverTurn dropped a spaced path")
	}
}

// TestRecycleSupport: the claude driver is recycle-capable (implements RecycleBridge); a
// driver without the bridge is not.
func TestRecycleSupport(t *testing.T) {
	if _, ok := RecycleSupport(newClaudeCode()); !ok {
		t.Error("claude-code should implement RecycleBridge")
	}
	if _, ok := RecycleSupport(stubNoBridge{}); ok {
		t.Error("a driver without the bridge must not type-assert as RecycleBridge")
	}
}

// stubNoBridge is a minimal Driver with NO RecycleBridge (and no ComposerStateProbe) — the
// recycle-incapable case the command must refuse.
type stubNoBridge struct{}

func (stubNoBridge) Name() string                { return "no-bridge" }
func (stubNoBridge) Submit(string, string) error { return nil }
func (stubNoBridge) Assess(string) State         { return StateIdle }
func (stubNoBridge) Rotate(string) error         { return nil }
func (stubNoBridge) RotateStrategy() Strategy    { return SlashCommand }
func (stubNoBridge) Close(string) error          { return ErrNoGracefulClose }
