package workspace

import (
	"fmt"

	"github.com/jim80net/flotilla/internal/launch"
)

// FleetTmuxCheck enforces the cross-recipe "no two share a tmux target" invariant for
// the resolving agent across the flat launch file. `target` is the resolving agent's
// explicit tmux target — empty means no explicit target (resume derives the default
// per-agent session flotilla-<agent>:desk, which never collides across distinct
// agents), so the check is a no-op, matching the flat file's behaviour of only
// guarding explicit targets.
func FleetTmuxCheck(agent, target string, flat *launch.Config) (warnings []string, err error) {
	if target == "" || flat == nil {
		return nil, nil
	}
	for name, r := range flat.Agents {
		if name == agent {
			continue
		}
		if r.Tmux == target {
			return warnings, fmt.Errorf("tmux target %q for %q collides with flat-file agent %q", target, agent, name)
		}
	}
	return nil, nil
}