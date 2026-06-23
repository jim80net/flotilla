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

// TestRecycleSupport: the claude AND grok drivers are recycle-capable (implement RecycleBridge); a
// driver without the bridge is not (the refuse fixture stays — KEEP stubNoBridge per #158).
func TestRecycleSupport(t *testing.T) {
	if _, ok := RecycleSupport(newClaudeCode()); !ok {
		t.Error("claude-code should implement RecycleBridge")
	}
	if _, ok := RecycleSupport(newGrok()); !ok {
		t.Error("grok should implement RecycleBridge (#158 — cross-harness recycle-capable)")
	}
	if _, ok := RecycleSupport(stubNoBridge{}); ok {
		t.Error("a driver without the bridge must not type-assert as RecycleBridge")
	}
}

// TestGrokHandoffPath: grok's designated path embeds the token under the HARNESS-AGNOSTIC
// .flotilla/handoffs/ (NOT the claude-branded .claude/handoffs/) — #158.
func TestGrokHandoffPath(t *testing.T) {
	g := newGrok()
	got := g.HandoffPath("/home/jim/work/spark", "20260623T141530.000000001-a3f91b2c")
	want := "/home/jim/work/spark/.flotilla/handoffs/recycle-20260623T141530.000000001-a3f91b2c.md"
	if got != want {
		t.Errorf("grok HandoffPath = %q, want %q", got, want)
	}
	if strings.Contains(got, ".claude/handoffs") {
		t.Errorf("grok HandoffPath must NOT use the claude-branded .claude/handoffs/: %q", got)
	}
}

// TestGrokHandoffTurn: the grok handoff turn names the exact path, force-commits, is non-interactive /
// remote-driven, and references NO claude/memex skill (grok has no /handoff skill).
func TestGrokHandoffTurn(t *testing.T) {
	g := newGrok()
	path := "/repo/.flotilla/handoffs/recycle-tok.md"
	turn := g.HandoffTurn(path)
	for _, must := range []string{path, "git add -f", "git commit", "REMOTE-DRIVEN", "stop"} {
		if !strings.Contains(turn, must) {
			t.Errorf("grok HandoffTurn missing %q\n--- turn ---\n%s", must, turn)
		}
	}
	// grok has no /handoff,/takeover SKILL — the turn must not invoke one. (We check "skill", not the
	// bare "/handoff", because the designated path legitimately contains ".flotilla/handoffs/".)
	if strings.Contains(turn, "skill") {
		t.Errorf("grok HandoffTurn must not reference a skill (grok has no /handoff skill)\n%s", turn)
	}
}

// TestGrokTakeoverTurn: the grok takeover turn names the exact path, says begin-immediately / not
// "shall I start?", mandates flotilla-message parlay, and references no /takeover skill.
func TestGrokTakeoverTurn(t *testing.T) {
	g := newGrok()
	path := "/repo/.flotilla/handoffs/recycle-tok.md"
	turn := g.TakeoverTurn(path)
	for _, must := range []string{path, "BEGIN WORK IMMEDIATELY", "REMOTE-DRIVEN", "flotilla", "shall I start"} {
		if !strings.Contains(turn, must) {
			t.Errorf("grok TakeoverTurn missing %q\n--- turn ---\n%s", must, turn)
		}
	}
	if strings.Contains(turn, "skill") {
		t.Errorf("grok TakeoverTurn must not reference a skill (grok has no /takeover skill)\n%s", turn)
	}
}

// TestGrokTurnsPathWithSpaces: a path with spaces is embedded verbatim in both turns.
func TestGrokTurnsPathWithSpaces(t *testing.T) {
	g := newGrok()
	path := "/home/jim/my work/.flotilla/handoffs/recycle-tok.md"
	if !strings.Contains(g.HandoffTurn(path), path) {
		t.Errorf("grok HandoffTurn dropped a spaced path")
	}
	if !strings.Contains(g.TakeoverTurn(path), path) {
		t.Errorf("grok TakeoverTurn dropped a spaced path")
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
