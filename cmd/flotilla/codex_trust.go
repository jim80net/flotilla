package main

import (
	"fmt"
	"os"

	"github.com/jim80net/flotilla/internal/codextrust"
	"github.com/jim80net/flotilla/internal/launch"
)

// codexSurfaceName is the registered codex driver name (internal/surface/codex.go).
const codexSurfaceName = "codex"

// recipeInvolvesCodex reports whether a desk's launch recipe can put a codex
// harness in the pane: the roster/overlay surface (which fills the implied
// primary slot's empty Surface) or any explicit chain slot names codex. Trust
// is seeded when ANY slot could launch codex — a later failover to that slot
// respawns into the same cwd, and an already-seeded entry is a no-op.
func recipeInvolvesCodex(rosterSurface string, recipe launch.Recipe) bool {
	if rosterSurface == codexSurfaceName {
		return true
	}
	for _, s := range recipe.Slots() {
		if s.Surface == codexSurfaceName {
			return true
		}
	}
	return false
}

// seedCodexTrust pre-seeds codex directory trust for the desk cwd (worktree-
// aware: the cwd key is what codex's trust lookup checks first — see
// internal/codextrust) so a codex desk launched there does not wedge on the
// interactive first-run trust menu, which a remote coordinator cannot answer.
//
// Best-effort by design: a seeding failure warns loudly on stderr but never
// blocks the launch — a desk that does reach the menu now classifies as
// awaiting-input (detector-escalated, submit-refused) rather than wedging
// silently, so launching anyway is strictly better than refusing to launch.
func seedCodexTrust(cwd string) {
	configPath, err := codextrust.ConfigPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "flotilla: warning — codex trust pre-seed skipped: %v\n", err)
		return
	}
	seeded, err := codextrust.Seed(configPath, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flotilla: warning — codex trust pre-seed for %q failed: %v (an untrusted dir shows codex's first-run trust menu; the desk reads awaiting-input until it is answered at the pane)\n", cwd, err)
		return
	}
	if seeded {
		fmt.Fprintf(os.Stderr, "flotilla: seeded codex directory trust for %q in %s\n", cwd, configPath)
	}
}
