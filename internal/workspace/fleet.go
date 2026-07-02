package workspace

import (
	"fmt"
	"path/filepath"

	"github.com/jim80net/flotilla/internal/launch"
)

// FleetTmuxCheck enforces the cross-recipe "no two share a tmux target" invariant for
// the resolving agent across BOTH sources: sibling workspaces
// (~/.flotilla/*/launch.json) and flat-file recipes for agents WITHOUT a workspace
// (so the invariant spans both during migration). `target` is the resolving agent's
// explicit tmux target — empty means no explicit target (resume derives the default
// per-agent session flotilla-<agent>:desk, which never collides across distinct
// agents), so the check is a no-op, matching the flat file's behaviour of only
// guarding explicit targets.
//
// Unlike the flat file's fail-closed load, a malformed/unreadable OTHER workspace is
// SKIPPED (surfaced in warnings), NOT fail-closed — a broken unrelated workspace MUST
// NOT block recovering a healthy desk. An actual collision is the only error.
func FleetTmuxCheck(agent, target string, flat *launch.Config) (warnings []string, err error) {
	if target == "" {
		return nil, nil
	}
	root, err := Root()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{} // agents accounted for via a workspace (don't double-count from flat)
	// A glob error (e.g. a metacharacter in the resolved root) must NOT silently
	// turn the cross-workspace collision guard into a no-op — surface it.
	matches, gerr := filepath.Glob(filepath.Join(root, "*", LaunchFileName))
	if gerr != nil {
		return warnings, fmt.Errorf("scan workspaces under %q: %w", root, gerr)
	}
	for _, p := range matches {
		other := filepath.Base(filepath.Dir(p))
		seen[other] = true
		if other == agent {
			continue
		}
		r, ok, lerr := LoadRecipe(other)
		if lerr != nil {
			warnings = append(warnings, fmt.Sprintf("skipped malformed workspace %q: %v (its tmux-collision check was bypassed for %q — fix it to restore the guard)", other, lerr, agent))
			continue
		}
		if ok && r.Tmux == target {
			return warnings, fmt.Errorf("tmux target %q for %q collides with workspace agent %q", target, agent, other)
		}
	}
	if flat != nil {
		for name, r := range flat.Agents {
			if name == agent || seen[name] {
				continue
			}
			if r.Tmux == target {
				return warnings, fmt.Errorf("tmux target %q for %q collides with flat-file agent %q", target, agent, name)
			}
		}
	}
	return warnings, nil
}
