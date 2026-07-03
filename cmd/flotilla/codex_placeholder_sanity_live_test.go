//go:build live

package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/surface"
)

// Live sanity: post-auth ghost placeholder must read Pending (not Cleared) at recycle
// idle-gate, and Confirm.Submit must not false-confirm on fast-turn delivery.
// Run: CODEX_SANITY_PANE=codex-sanity:0.0 go test -tags live ./cmd/flotilla -run TestLiveCodexPlaceholderSanity -v
func TestLiveCodexPlaceholderSanity(t *testing.T) {
	pane := os.Getenv("CODEX_SANITY_PANE")
	if pane == "" {
		pane = "codex-sanity:0.0"
	}
	d, ok := surface.Get("codex")
	if !ok {
		t.Fatal("codex driver not registered")
	}
	sp, ok := d.(surface.ComposerStateProbe)
	if !ok {
		t.Fatal("codex driver lacks ComposerStateProbe")
	}

	assess := d.Assess(pane)
	composer := sp.ComposerState(pane)
	cy, inMode, cyErr := deliver.CursorState(pane)
	captured, capErr := deliver.CapturePane(pane)

	t.Logf("CAPTURE assess=%v composer=%v cursor_y=%d inMode=%v cyErr=%v capErr=%v", assess, composer, cy, inMode, cyErr, capErr)
	if capErr == nil {
		lines := splitCaptureLines(captured)
		if cy >= 0 && cy < len(lines) {
			t.Logf("CAPTURE cursor_line=%q", lines[cy])
		}
		t.Logf("CAPTURE pane_tail:\n%s", tailNonEmpty(captured, 8))
	}

	if assess != surface.StateIdle {
		t.Fatalf("want idle desk for sanity capture, got %v", assess)
	}
	if composer == surface.ComposerCleared {
		t.Fatal("FAIL: ghost placeholder reads Cleared — recycle idle-gate would false-pass")
	}
	if composer != surface.ComposerPending {
		t.Fatalf("unexpected composer disposition %v (want Pending for ghost text)", composer)
	}
	t.Log("PASS idle-gate: placeholder reads Pending, recycle_idleCleared=false")

	c := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	err := c.Submit(d, pane, "Reply with exactly SANITY_OK and nothing else.")
	t.Logf("CONFIRM err=%v post_assess=%v post_composer=%v", err, d.Assess(pane), sp.ComposerState(pane))
	if err != nil {
		t.Fatalf("confirm submit failed (want fast-turn confirm, not false-block): %v", err)
	}
	t.Log("PASS confirm: submit confirmed (Working or cleared — not false-confirm on placeholder)")
}

func splitCaptureLines(s string) []string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func tailNonEmpty(captured string, n int) string {
	var lines []string
	for _, ln := range splitCaptureLines(captured) {
		if ln != "" && ln != " " {
			lines = append(lines, ln)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := ""
	for _, ln := range lines {
		out += fmt.Sprintf("  %s\n", ln)
	}
	return out
}
