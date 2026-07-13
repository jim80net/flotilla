package main

import (
	"fmt"
	"os"
	"path/filepath"

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

// seedCodexTrust pre-seeds codex launch configuration: it disables the
// interactive startup update prompt for this centrally managed fleet, then
// seeds directory trust for the desk cwd (worktree-aware: the cwd key is what
// codex's trust lookup checks first — see internal/codextrust).
//
// BOTH path forms are seeded when they differ: the given (logical) cwd and its
// symlink-resolved realpath. The launched codex derives its lookup key from
// getcwd (symlink-free), so a recipe cwd that traverses a symlink needs the
// realpath entry; the logical form covers a codex normalization that keeps the
// path as given. Seeding is idempotent, so the extra entry costs nothing.
//
// Best-effort by design: a seeding failure warns loudly on stderr but never
// blocks the launch — a desk that does reach the menu now classifies as
// awaiting-input (the detector escalates the observed transition; a send into
// the menu refuses loudly) rather than wedging silently, so launching anyway is
// strictly better than refusing to launch. Known bound: codextrust.ConfigPath
// reads THIS process's CODEX_HOME — a launch recipe that exports a different
// CODEX_HOME inline is not parsed (the classifier layer catches that desk).
func seedCodexTrust(cwd string) {
	configPath, err := codextrust.ConfigPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "flotilla: warning — codex trust pre-seed skipped: %v\n", err)
		return
	}
	updatesSuppressed, err := codextrust.SuppressStartupUpdateCheck(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flotilla: warning — codex startup update suppression failed: %v (a release may leave the desk awaiting input at the update menu)\n", err)
	} else if updatesSuppressed {
		fmt.Fprintf(os.Stderr, "flotilla: disabled codex startup update prompts in %s (version upgrades remain fleet-ops-owned)\n", configPath)
	}
	forms := []string{cwd}
	if real, rerr := filepath.EvalSymlinks(cwd); rerr == nil && real != cwd {
		forms = append(forms, real)
	}
	for _, form := range forms {
		seeded, err := codextrust.Seed(configPath, form)
		if err != nil {
			fmt.Fprintf(os.Stderr, "flotilla: warning — codex trust pre-seed for %q failed: %v (an untrusted dir shows codex's first-run trust menu; the desk reads awaiting-input until it is answered at the pane)\n", form, err)
			continue
		}
		if seeded {
			fmt.Fprintf(os.Stderr, "flotilla: seeded codex directory trust for %q in %s\n", form, configPath)
		}
	}
}
