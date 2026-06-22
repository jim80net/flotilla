package surface

import (
	"errors"
	"strings"
	"testing"
)

// Fixtures below mirror VERIFIED-LIVE captures (family-office %31, 2026-06-22): the agents panel
// docks at the absolute bottom; a focused panel puts the "❯" cursor on an agent row (◯/●), with the
// composer "❯ " above it. Glyphs: ❯ U+276F, ◯ U+25EF (idle), ● U+25CF (active).

// foFocused is the golden blocked capture: empty composer above, panel docked at the bottom with the
// cursor on the LAST agent row (portfoliosrc-fix).
const foFocused = `  Want me to kick off the edge audit + cost number now, and bring you the graded inventory?
                                   new task? /clear to save 527.5k tokens
  ────────────────────────────────────────────────────────────────────────────
❯
  ────────────────────────────────────────────────────────────────────────────
  jim@rt-dgx-sp001:~/workspace/github.com/General-ML/spark-familyoffice [Opus 4.8] ctx:48%
  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents
  ● main                                            ↑/↓ to select · Enter to view
  ◯ predmkt-build     predmkt-build: You are a build agent ...                    idle
❯ ◯ portfoliosrc-fix  portfoliosrc-fix: You are a focused build agent ...         idle`

func TestParsePanelFocused(t *testing.T) {
	// longMiddle: an 8-subagent panel (the memex case) with the cursor on a MIDDLE row — the case the
	// retired fixed-window rule missed. Rows below the cursor carry no "❯", so the cursor is still the
	// bottom-most "❯".
	longMiddle := "  some conversation line\n  ────\n❯ \n  ────\n  jim@host [Opus 4.8] ctx:33%\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\n" +
		"  ● main                  ↑/↓ to select · Enter to view\n" +
		"  ◯ agent1  ... idle\n" +
		"  ◯ agent2  ... idle\n" +
		"❯ ◯ agent3  ... idle\n" + // cursor here (middle)
		"  ◯ agent4  ... idle\n" +
		"  ◯ agent5  ... idle\n" +
		"  ◯ agent6  ... idle\n" +
		"  ◯ agent7  ... idle\n" +
		"  ◯ agent8  ... idle"

	// displayed: a panel is shown but the COMPOSER is focused (cursor on the composer, no "❯" on any
	// agent row) — a healthy desk running background agents must still receive deliveries.
	displayed := "  conversation\n  ────\n❯ \n  ────\n  jim@host [Opus 4.8]\n  ⏵⏵ auto mode · ← for agents\n" +
		"  ● main      ↑/↓ to select · Enter to view\n" +
		"  ◯ agent1  ... idle\n" +
		"  ◯ agent2  ... idle"

	// echoLone: a lone "❯ ◯ ..." line echoed in scrollback ABOVE a live empty composer (the proven
	// flotilla-dev false positive). The live composer is the bottom-most "❯".
	echoLone := "  ❯ ◯ portfoliosrc-fix  ... idle  (this is a printed capture in scrollback)\n  more conversation\n  ────\n❯ \n  ────\n  jim@host\n  ⏵⏵ auto mode"

	// echoFullPanel: an ENTIRE panel capture (header + rows + cursor) echoed in scrollback above a
	// live empty composer, with NO live panel. The bottom-most "❯" is the live composer.
	echoFullPanel := "  ● main      ↑/↓ to select · Enter to view\n  ◯ agent1 ... idle\n  ❯ ◯ agent2 ... idle\n" +
		"  (^ all the above was a printed capture)\n  more conversation\n  ────\n❯ \n  ────\n  jim@host\n  ⏵⏵ auto mode"

	// composerText: a composer holding a body whose text begins with an agent glyph, no panel header.
	// The header guard must prevent a false block.
	composerText := "  conversation\n  ────\n❯ ◯ this is literally typed text starting with a circle\n  ────\n  jim@host\n  ⏵⏵ auto mode"

	// noPrompt: no "❯" anywhere (e.g. mid-render).
	noPrompt := "  conversation\n  ────\n  jim@host\n  ⏵⏵ auto mode"

	// footerBelowCursor (RESIDUAL): a focused panel but with a "❯"-bearing NON-agent line BELOW the
	// cursor. Per the verified geometry the panel cursor IS the bottom-most "❯"; if a future TUI ever
	// renders a footer with a "❯" below the panel, the bottom-most "❯" is no longer the cursor and
	// detection degrades to NOT-blocked. This documents that intentional residual (design RESIDUAL) —
	// it matches today's geometry (no such footer exists) and degrades to today's behavior, no regression.
	footerBelowCursor := "  conversation\n  ────\n❯ \n  ────\n  jim@host\n  ⏵⏵ auto mode\n" +
		"  ● main      ↑/↓ to select · Enter to view\n" +
		"  ◯ agent1 ... idle\n" +
		"❯ ◯ agent2 ... idle\n" + // panel cursor
		"❯ a hypothetical future footer line" // a NON-agent "❯" BELOW the cursor

	cases := []struct {
		name        string
		capture     string
		wantBlocked bool
		wantOK      bool
	}{
		{"family-office focused (golden)", foFocused, true, true},
		{"long panel, cursor on middle row", longMiddle, true, true},
		{"panel displayed, composer focused", displayed, false, true},
		{"scrollback echo (lone cursor line)", echoLone, false, true},
		{"scrollback echo (full panel, no live panel)", echoFullPanel, false, true},
		{"composer text starting with a glyph, no header", composerText, false, true},
		{"no prompt at all", noPrompt, false, true},
		{"RESIDUAL: ❯ footer below the panel cursor (degrades to not-blocked)", footerBelowCursor, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocked, ok := parsePanelFocused(tc.capture)
			if blocked != tc.wantBlocked || ok != tc.wantOK {
				t.Errorf("parsePanelFocused = (%v, %v), want (%v, %v)", blocked, ok, tc.wantBlocked, tc.wantOK)
			}
		})
	}
}

// TestClaudeInputBlockedCaptureError: a capture failure reads as UNDETERMINED (ok=false) so the
// caller falls back to NOT blocked rather than refusing a delivery off a glitch.
func TestClaudeInputBlockedCaptureError(t *testing.T) {
	c := claudeCode{capturePane: func(string) (string, error) { return "", errors.New("tmux glitch") }}
	blocked, ok := c.InputBlocked("0:0.0")
	if blocked || ok {
		t.Errorf("InputBlocked on capture error = (%v, %v), want (false, false)", blocked, ok)
	}
}

// TestClaudeInputBlockedFocused: the driver wires capture → parse → (true, true) on the golden capture.
func TestClaudeInputBlockedFocused(t *testing.T) {
	c := claudeCode{capturePane: func(string) (string, error) { return foFocused, nil }}
	blocked, ok := c.InputBlocked("0:0.0")
	if !blocked || !ok {
		t.Errorf("InputBlocked on focused panel = (%v, %v), want (true, true)", blocked, ok)
	}
}

// TestParsePanelFocusedCanaryNoHeader: a bottom-most agent-row cursor with NO recognized header is
// NOT blocked (degraded-safe) — the geometry alone isn't trusted without the header corroboration.
func TestParsePanelFocusedCanaryNoHeader(t *testing.T) {
	noHeader := "  conversation\n  ────\n  jim@host\n❯ ◯ agent1 ... idle"
	if blocked, ok := parsePanelFocused(noHeader); blocked || !ok {
		t.Errorf("parsePanelFocused(no header) = (%v, %v), want (false, true)", blocked, ok)
	}
	// Sanity: the golden capture DOES contain the header hint (so the corroboration is real, not vacuous).
	if !strings.Contains(foFocused, panelHeaderHint) {
		t.Fatalf("golden fixture lost the panel header hint %q", panelHeaderHint)
	}
}
