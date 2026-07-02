package launch

import "strings"

// Tmux topology conventions for cold-resume and workspace init.
//
// v2 (per-agent session, seat-flip ready): each desk gets a detached tmux session
// named flotilla-<agent> with a single window desk (recipe flotilla-<agent>:desk).
// Cross-session list-panes -a resolves any desk; cold-create uses new-session.
//
// v1 (legacy shared session): one flotilla session, one window per agent
// (recipe flotilla:<agent>). Preserved for existing launch.json files; cold-create
// adds a window when the shared session already exists.
const (
	SharedFleetSession    = "flotilla"
	PerAgentSessionPrefix = "flotilla-"
	DefaultDeskWindow     = "desk"
)

// DefaultPerAgentTmux returns the canonical v2 recipe tmux target for an agent.
func DefaultPerAgentTmux(agent string) string {
	return PerAgentSessionPrefix + agent + ":" + DefaultDeskWindow
}

// ResumeTarget derives the (session, window) pair for resume cold-create.
// An empty recipe.Tmux defaults to the per-agent session topology.
func ResumeTarget(r Recipe, agentName string) (session, window string) {
	if r.Tmux == "" {
		return PerAgentSessionPrefix + agentName, DefaultDeskWindow
	}
	session, window, _ = strings.Cut(r.Tmux, ":")
	return session, window
}

// IsPerAgentSession reports whether session uses the v2 per-agent topology
// (session name flotilla-<agent>, not the legacy shared flotilla session).
func IsPerAgentSession(session string) bool {
	return strings.HasPrefix(session, PerAgentSessionPrefix)
}
