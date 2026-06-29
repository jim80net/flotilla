package surface

import (
	"errors"
	"strings"
	"testing"
)

// Detection is CURSOR-based: classifyComposerLine classifies the line at the terminal cursor (the
// focused input). These fixtures use the REAL bytes verified live on a running deployment (2026-06-22):
//
//	prompt = U+276F "❯"
//	nbsp   = U+00A0 NON-BREAKING space — Claude Code renders this (NOT ASCII 0x20) between the prompt
//	         and the body. An ASCII-space fixture passed while the live pane returned the opposite —
//	         the synthetic-fixture trap this corpus closes. We assert the const IS U+00A0 below.
//	idle/active glyph = U+25EF "◯" / U+25CF "●".
const (
	prompt      = "❯"
	nbsp        = "\u00a0"
	idleGlyph   = "◯"
	activeGlyph = "●"
)

// makePane builds a capture whose line at index cursorAt is focusLine (realistic cursor_y was 64-69).
func makePane(focusLine string, cursorAt int) string {
	lines := make([]string, 0, cursorAt+3)
	for i := 0; i < cursorAt; i++ {
		lines = append(lines, "  prior conversation / chrome")
	}
	lines = append(lines, focusLine, "  --------", "  operator@host [Opus 4.8] ctx:48%")
	return strings.Join(lines, "\n")
}

func TestClassifyComposerLine(t *testing.T) {
	// Guard against the synthetic-fixture trap: the nbsp const MUST be the non-breaking space.
	if nbsp != "\u00a0" {
		t.Fatalf("nbsp const is %q, want U+00A0 — fixtures must use the real byte the live pane renders", nbsp)
	}
	cases := []struct {
		name  string
		focus string
		want  ComposerDisposition
	}{
		// The three live states the geometry rule got wrong.
		{"empty main composer → Cleared", prompt + nbsp, ComposerCleared},
		{"per-agent message sub-composer → SubAgent", prompt + nbsp + "Message @reviewer…", ComposerSubAgent},
		{"queued behind a modal → Queued", prompt + nbsp + "Press up to edit queued messages", ComposerQueued},
		// List-nav: cursor literally on an agent row.
		{"cursor on an idle agent row → ListNav", prompt + nbsp + idleGlyph + " portfoliosrc-fix  idle", ComposerListNav},
		{"cursor on an active agent row → ListNav", prompt + nbsp + activeGlyph + " reviewer  idle", ComposerListNav},
		// Main composer with the user's own draft → Pending (a body remains).
		{"main composer with user text → Pending", prompt + nbsp + "operator: are you there?", ComposerPending},
		// A draft that merely MENTIONS an agent mid-line is the main composer, not a sub-composer.
		{"main composer mentioning @agent mid-line → Pending", prompt + nbsp + "ping @reviewer re the PR", ComposerPending},
		// Belt-and-suspenders: an ASCII-space sub-composer classifies the same (no regress if the TUI
		// swaps the NBSP for a normal space).
		{"ascii-space sub-composer → SubAgent", prompt + " Message @x", ComposerSubAgent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyComposerLine(makePane(tc.focus, 64), 64)
			if got != tc.want {
				t.Errorf("classifyComposerLine(%q) = %v, want %v", tc.focus, got, tc.want)
			}
		})
	}
}

func TestClassifyComposerLineEdges(t *testing.T) {
	cap := makePane(prompt+nbsp, 5)
	if got := classifyComposerLine(cap, 9999); got != ComposerUndetermined {
		t.Errorf("out-of-range cursorY = %v, want Undetermined", got)
	}
	if got := classifyComposerLine(cap, -1); got != ComposerUndetermined {
		t.Errorf("negative cursorY = %v, want Undetermined", got)
	}
	// cursor on a NON-prompt line (no prompt glyph) → Undetermined (fall back to the spinner).
	if got := classifyComposerLine("  plain conversation\n  more", 0); got != ComposerUndetermined {
		t.Errorf("cursor on non-prompt line = %v, want Undetermined", got)
	}
}

// TestClaudeComposerStateWiring: the driver threads cursorY + capturePane → classifyComposerLine,
// using the real NBSP renders, including a sub-composer rendered ABOVE a docked panel (the
// miss the bottom-of-pane window could not see).
func TestClaudeComposerStateWiring(t *testing.T) {
	// data-desk-like: cursor (y=2) on the sub-composer rendered above the panel rows below → SubAgent.
	dataCap := "  conversation\n  -- @reviewer --\n" + prompt + nbsp + "Message @reviewer…\n  ----\n  operator@host\n  " +
		idleGlyph + " main\n  " + idleGlyph + " hookbug  idle"
	c := claudeCode{
		cursorState: func(string) (int, bool, error) { return 2, false, nil },
		capturePane: func(string) (string, error) { return dataCap, nil },
	}
	if got := c.ComposerState("0:0.0"); got != ComposerSubAgent {
		t.Errorf("data-desk sub-composer ComposerState = %v, want SubAgent", got)
	}

	// desk-like: cursor (y=2) on the empty MAIN composer, with a panel selection marker
	// (prompt+◯) rendered BELOW it → Cleared (reachable-by-position; the post-submit state is the
	// authority — this just isn't the sub-composer carve-out).
	deskCap := "  conversation\n  ----\n" + prompt + nbsp + "\n  ----\n  operator@host\n" + prompt + nbsp + idleGlyph + " portfoliosrc-fix  idle"
	c2 := claudeCode{
		cursorState: func(string) (int, bool, error) { return 2, false, nil },
		capturePane: func(string) (string, error) { return deskCap, nil },
	}
	if got := c2.ComposerState("0:0.0"); got != ComposerCleared {
		t.Errorf("desk main composer ComposerState = %v, want Cleared (the ◯ marker below is not focus)", got)
	}
}

func TestClaudeComposerStateUndetermined(t *testing.T) {
	// A cursor read error → Undetermined; the caller falls back to the spinner.
	c := claudeCode{cursorState: func(string) (int, bool, error) { return 0, false, errors.New("no server") }}
	if got := c.ComposerState("0:0.0"); got != ComposerUndetermined {
		t.Errorf("cursor read error = %v, want Undetermined", got)
	}
	// A capture error (after a good cursor read) → Undetermined.
	c2 := claudeCode{
		cursorState: func(string) (int, bool, error) { return 2, false, nil },
		capturePane: func(string) (string, error) { return "", errors.New("glitch") },
	}
	if got := c2.ComposerState("0:0.0"); got != ComposerUndetermined {
		t.Errorf("capture error = %v, want Undetermined", got)
	}
	// COPY-MODE (inMode=true): the cursor + capture coordinate spaces diverge, so even with a clean
	// capture the result MUST be Undetermined (fail-safe to the spinner) — never a cursor-indexed
	// mis-classification. The guard must short-circuit BEFORE capturePane (which here would return a
	// "cleared" composer that must NOT be trusted).
	c3 := claudeCode{
		cursorState: func(string) (int, bool, error) { return 2, true, nil }, // pane in copy/view-mode
		capturePane: func(string) (string, error) { return makePane(prompt+nbsp, 2), nil },
	}
	if got := c3.ComposerState("0:0.0"); got != ComposerUndetermined {
		t.Errorf("copy-mode = %v, want Undetermined (cursor/capture coords diverge — fail-safe)", got)
	}
}
