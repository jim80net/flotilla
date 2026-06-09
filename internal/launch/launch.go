// Package launch describes the HOST-LOCAL launch recipes that let flotilla
// deterministically (re)start a dead desk. The committable roster stays portable
// (names, surface, watch config — no host paths); the recipes — a desk's launch
// command, working directory, and optional tmux target / state pointer — are
// host-specific and live in a separate, gitignored file (a sibling of
// flotilla-secrets.env), loaded by Load. This mirrors the secrets-file pattern:
// a committable roster plus a host-local file trusted at the secrets level.
//
// See docs/agent-launch-recipes-design.md for the full design and validation
// table.
package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Recipe is one desk's host-local launch recipe.
type Recipe struct {
	// Launch (required) is the shell command that (re)starts the desk. It is the
	// pane's foreground process (tmux runs it via the pane's `sh -c`), so a
	// compound `cd x && claude --continue` works, and when it exits the pane dies
	// (a dead recipe surfaces as a dead pane the watchdog catches). Recipes are
	// therefore shell-interpreted; the launch file is host-local and trusted at
	// the secrets level.
	Launch string `json:"launch"`
	// Cwd (required) is the working directory / worktree to launch in. It MUST be
	// absolute (a host-independent typo guard); existence is NOT checked at load —
	// the file may be loaded on another host — so a missing dir surfaces as a
	// clear resume-time error, not a load error.
	Cwd string `json:"cwd"`
	// Tmux (optional) is the target `session:window` to (re)create the pane in;
	// empty defaults to `flotilla:<name>` (a canonical `flotilla` session, one
	// window per agent) at resume time.
	Tmux string `json:"tmux,omitempty"`
	// State (optional) is a pointer to the desk's handoff/context doc, surfaced
	// for the operator/skill to drive `/takeover` (the CLI does NOT auto-inject it
	// — see the design's Non-goals).
	State string `json:"state,omitempty"`
}

// Config is the host-local set of launch recipes, keyed by agent name.
type Config struct {
	Agents map[string]Recipe `json:"agents"`
}

// DefaultPath returns the conventional launch-file path: a sibling of the roster
// named flotilla-launch.json. Mirrors the watch defaults (`<roster-dir>/…`).
func DefaultPath(rosterPath string) string {
	return filepath.Join(filepath.Dir(rosterPath), "flotilla-launch.json")
}

// Load reads and validates a launch-recipe file, holding it to roster.Load's
// discipline. rosterAgents is the set of agent names declared in the roster;
// every key in the file MUST be one of them (an unknown key is a typo and a load
// error). Load is FAIL-CLOSED: a single malformed recipe blocks loading the whole
// file, so resume for every desk fails until it is fixed — the correct safety
// posture (never resume on a half-parsed file). The recovery skill must
// document that one bad entry blocks recovering the entire fleet.
func Load(path string, rosterAgents map[string]bool) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read launch recipes %q: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse launch recipes %q: %w", path, err)
	}
	// seenTmux rejects two recipes sharing a non-empty tmux target — they would
	// resume into the same window, mirroring roster's shared-title rejection.
	seenTmux := make(map[string]string, len(c.Agents))
	for name, r := range c.Agents {
		// Every key must name a roster agent (catches typos — a recipe for an
		// agent that does not exist can never be resumed and signals a mistake).
		if !rosterAgents[name] {
			return nil, fmt.Errorf("launch recipes %q: agent %q is not in the roster (typo?)", path, name)
		}
		// launch: required, non-empty, no tab/newline (it flows onto tmux argv and
		// the wire format the marker shares; reject \t \n \r like roster.go).
		if r.Launch == "" {
			return nil, fmt.Errorf("launch recipes %q: agent %q has an empty launch command", path, name)
		}
		if strings.ContainsAny(r.Launch, "\t\n\r") {
			return nil, fmt.Errorf("launch recipes %q: agent %q launch %q contains a tab/newline", path, name, r.Launch)
		}
		// cwd: required, non-empty, absolute (a host-independent typo guard;
		// existence is checked at resume time, not load — the file may be loaded
		// on another host).
		if r.Cwd == "" {
			return nil, fmt.Errorf("launch recipes %q: agent %q has an empty cwd", path, name)
		}
		if strings.ContainsAny(r.Cwd, "\t\n\r") {
			return nil, fmt.Errorf("launch recipes %q: agent %q cwd %q contains a tab/newline", path, name, r.Cwd)
		}
		if !filepath.IsAbs(r.Cwd) {
			return nil, fmt.Errorf("launch recipes %q: agent %q cwd %q is not absolute", path, name, r.Cwd)
		}
		// tmux: optional; if present must parse as session:window (a single ":"
		// with non-empty halves) and carry no tab/newline.
		if r.Tmux != "" {
			if strings.ContainsAny(r.Tmux, "\t\n\r") {
				return nil, fmt.Errorf("launch recipes %q: agent %q tmux %q contains a tab/newline", path, name, r.Tmux)
			}
			if !validTmuxTarget(r.Tmux) {
				return nil, fmt.Errorf("launch recipes %q: agent %q tmux %q is not a valid session:window target", path, name, r.Tmux)
			}
			if other, dup := seenTmux[r.Tmux]; dup {
				return nil, fmt.Errorf("launch recipes %q: agents %q and %q share tmux target %q (would resume into the same window)", path, other, name, r.Tmux)
			}
			seenTmux[r.Tmux] = name
		}
		// state: optional; reject \t \n \r — it is PRINTED for the operator/skill
		// to parse, so a newline would corrupt that output line.
		if strings.ContainsAny(r.State, "\t\n\r") {
			return nil, fmt.Errorf("launch recipes %q: agent %q state %q contains a tab/newline", path, name, r.State)
		}
	}
	return &c, nil
}

// Recipe returns the recipe for an agent and whether one is declared. An agent
// present in the roster but absent here is "declared but not resumable" — the
// caller errors clearly rather than guessing.
func (c *Config) Recipe(agent string) (Recipe, bool) {
	r, ok := c.Agents[agent]
	return r, ok
}

// validTmuxTarget reports whether s is a plain `session:window` target: exactly
// one ":" with a non-empty session and a non-empty window, no tmux pane-index
// suffix on the window (a trailing ".<digits>"), and no spaces in either half.
// resume derives the pane itself, so a window ending in ".<digits>" (e.g.
// "hydra-ops.0", "rel-1.2") is rejected — tmux would parse it as a pane
// reference, not a window name. A non-numeric dot (e.g. "my.app") is fine.
// Spaces are rejected because they would break the downstream `tmux new-session
// -s <session> -n <window>` argv. (\t \n \r are already rejected by the caller
// before this runs.)
func validTmuxTarget(s string) bool {
	session, window, found := strings.Cut(s, ":")
	if !found || session == "" || window == "" {
		return false
	}
	// A second colon (e.g. "a:b:c") is an ambiguous target.
	if strings.Contains(window, ":") {
		return false
	}
	// A trailing ".<digits>" is a tmux pane index — resume derives the pane, so
	// it must not be baked into the window name.
	if i := strings.LastIndexByte(window, '.'); i >= 0 && isAllDigits(window[i+1:]) {
		return false
	}
	// Spaces would break the tmux argv for the cold-create commands.
	if strings.ContainsRune(session, ' ') || strings.ContainsRune(window, ' ') {
		return false
	}
	return true
}

// isAllDigits reports whether s is non-empty and entirely ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
