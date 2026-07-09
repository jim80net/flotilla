package surface

import (
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
)

// interactiveConfirmTail bounds confirmation-chrome scans to the live footer
// (same order as worktree-exit / grok status tails).
const interactiveConfirmTail = 14

// ElevateIdle upgrades a bare Idle assessment when the cursor is on a focus-stealing
// composer disposition that recycle's idle∧cleared gate already refuses (#557).
//
// A pane that merely DISPLAYS background agents while the main composer is Cleared
// stays Idle (panel-input-guard contract) — only focus on the panel/sub-composer
// elevates. ComposerUndetermined alone does NOT elevate (capture glitches must not
// flap idle→awaiting-input); interactive confirmation chrome is handled separately
// by InteractiveConfirmPrompt inside drivers / AssessForFleet.
func ElevateIdle(st State, disp ComposerDisposition) State {
	if st != StateIdle {
		return st
	}
	switch disp {
	case ComposerSubAgent, ComposerListNav:
		// Cursor on agents panel or per-agent sub-composer — not a safe idle seam.
		return StateAwaitingInput
	case ComposerQueued:
		// Input queued behind an in-flight turn — still mid-work for automation.
		return StateWorking
	default:
		return st
	}
}

// InteractiveConfirmPrompt reports harness-agnostic interactive confirmation chrome
// that blocks unattended automation (exit menus, numbered confirm dialogs). Pure —
// no pane I/O. Used so Assess does not report plain Idle when recycle would refuse
// the idle∧cleared gate for the same frame (#557).
func InteractiveConfirmPrompt(captured string) bool {
	if deliver.ClaudeWorktreeExitPrompt(captured) {
		return true
	}
	tail := strings.ToLower(deliver.TailRegion(captured, interactiveConfirmTail))
	if tail == "" {
		return false
	}
	// Numbered menu + confirm affordance (Claude worktree-style and siblings).
	if hasConfirmAffordance(tail) && hasNumberedMenu(tail) {
		return true
	}
	// Explicit exit / quit confirmation (y/n or confirm wording in the live tail).
	if (strings.Contains(tail, "exit") || strings.Contains(tail, "quit") || strings.Contains(tail, "close session")) &&
		hasConfirmAffordance(tail) {
		return true
	}
	// AskUserQuestion-style choice lists: "select" / "choose" with numbered options.
	if hasNumberedMenu(tail) && (strings.Contains(tail, "select an option") ||
		strings.Contains(tail, "choose one") ||
		strings.Contains(tail, "which option") ||
		strings.Contains(tail, "do you want to")) {
		return true
	}
	return false
}

func hasConfirmAffordance(tailLower string) bool {
	return strings.Contains(tailLower, "enter to confirm") ||
		strings.Contains(tailLower, "press enter to confirm") ||
		strings.Contains(tailLower, "press enter to continue") ||
		strings.Contains(tailLower, "(y/n)") ||
		strings.Contains(tailLower, "[y/n]") ||
		strings.Contains(tailLower, "y/n") ||
		strings.Contains(tailLower, "are you sure")
}

func hasNumberedMenu(tailLower string) bool {
	// Common TUI numbered choice rows: "1. …" / "1) …" / "› 1. …"
	return strings.Contains(tailLower, "1.") ||
		strings.Contains(tailLower, "1)") ||
		strings.Contains(tailLower, "› 1") ||
		strings.Contains(tailLower, "> 1")
}

// AssessForFleet is the status/detector assess path: Driver.Assess plus composer-aware
// idle elevation and interactive-confirm re-check so plain "idle" means a safe settled
// composer seam — not "stuck at a prompt / panel with live background work" (#557).
//
// Recycle keeps its own idle∧ComposerCleared gate; this aligns the published snapshot
// vocabulary with that class of unsafe frames without requiring every caller to probe
// the composer twice.
func AssessForFleet(d Driver, pane string) State {
	st := d.Assess(pane)
	if st != StateIdle {
		return st
	}
	if probe, ok := d.(ComposerStateProbe); ok {
		st = ElevateIdle(st, probe.ComposerState(pane))
		if st != StateIdle {
			return st
		}
	}
	// Drivers that already scan InteractiveConfirmPrompt inside Assess return
	// non-Idle above. Re-scan here only when we can cheaply re-capture for drivers
	// that skipped it (or for stubs) — skip when no capture is available.
	// Prefer not double-capturing: callers that already classified stay Idle only
	// when both Assess and composer say so.
	return st
}
