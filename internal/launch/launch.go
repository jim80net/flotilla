// Package launch describes the HOST-LOCAL launch recipes that let flotilla
// deterministically (re)start a dead desk. The committable roster stays portable
// (names, surface, watch config — no host paths); the recipes — a desk's launch
// command, working directory, and optional tmux target / state pointer — are
// host-specific and live in a separate, gitignored file (a sibling of
// flotilla-secrets.env), loaded by Load. This mirrors the secrets-file pattern:
// a committable roster plus a host-local file trusted at the secrets level.
package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jim80net/flotilla/internal/accounts"
)

// Recipe is one desk's host-local launch recipe.
type Recipe struct {
	// Launch (required) is the shell command that (re)starts the desk. It is the
	// pane's foreground process (tmux runs it via the pane's `sh -c`), so a
	// compound `cd x && claude --continue` works, and when it exits the pane dies
	// (a dead recipe surfaces as a dead pane the watchdog catches). Recipes are
	// therefore shell-interpreted; the launch file is host-local and trusted at
	// the secrets level.
	//
	// When no Primary/Fallbacks chain is declared, this flat Launch IS the
	// primary slot (the backward-compatible default — see Slots).
	Launch string `json:"launch"`
	// Cwd (required) is the working directory / worktree to launch in. It MUST be
	// absolute (a host-independent typo guard); existence is NOT checked at load —
	// the file may be loaded on another host — so a missing dir surfaces as a
	// clear resume-time error, not a load error.
	Cwd string `json:"cwd"`
	// Tmux (optional) is the target `session:window` to (re)create the pane in.
	// Empty defaults at resume time to the per-agent session topology
	// `flotilla-<name>:desk` (see ResumeTarget). Legacy recipes use the shared
	// `flotilla` session with one window per agent (`flotilla:<name>`).
	Tmux string `json:"tmux,omitempty"`
	// State (optional) is a pointer to the desk's handoff/context doc, surfaced
	// for the operator/skill to drive `/takeover` (the CLI does NOT auto-inject it
	// — see the design's Non-goals).
	State string `json:"state,omitempty"`
	// Primary (optional) is the head of an explicit harness failover chain. When
	// it is nil (the common case), the flat Launch above is the implied primary
	// slot — every existing recipe keeps working byte-identically. When it is set,
	// it overrides the flat Launch as the primary harness. Cwd/Tmux/State stay
	// recipe-level: the DESK (worktree + pane) is stable across a switch; only the
	// foreground harness process changes.
	Primary *HarnessSlot `json:"primary,omitempty"`
	// Fallbacks (optional) are the ordered failover targets, tried primary-first
	// then in declared order when a harness/subscription must be swapped out (e.g.
	// a provider-wide throttle). Empty when no chain is declared.
	Fallbacks []HarnessSlot `json:"fallbacks,omitempty"`
	// SingleHarness explicitly acknowledges that this seat intentionally has no
	// failover target. It silences the doctor/watch chain warning; it does not
	// synthesize protection or alter slot selection. A recipe cannot declare this
	// acknowledgement and fallbacks at the same time.
	SingleHarness bool `json:"single_harness,omitempty"`
}

// HarnessSlot is one harness in a desk's failover chain: a specific surface +
// launch command + logical provider, sharing the recipe-level Cwd/Tmux/State.
type HarnessSlot struct {
	// Surface names the slot's terminal-surface driver (e.g. "claude-code",
	// "grok", "opencode"). Like roster.Agent.Surface it is a plain string here —
	// the known-driver check is deferred to switch/resume time (in cmd), keeping
	// this package free of an internal/surface import (an import cycle guard). An
	// empty Surface on the IMPLIED primary slot is filled by the caller from the
	// roster Agent.surface (or the default driver).
	Surface string `json:"surface,omitempty"`
	// Launch (required for an explicit slot) is this harness's shell command,
	// holding to the same shape rules as the flat Recipe.Launch (non-empty, no
	// \t/\n/\r — validated at load by ValidateRecipe).
	Launch string `json:"launch"`
	// Provider is the throttle/billing domain — the identity whose server-side
	// limit would affect this slot. It is not necessarily the model vendor: when a
	// gateway manages the subscription and quota, use the gateway (for example,
	// "opencode") even if it serves another vendor's model. This is LOAD-BEARING
	// for failover target selection: a server-side throttle poisons the whole
	// provider, so failover must cross to a slot with a DIFFERENT provider. It is
	// DISTINCT from SubscriptionID (two subscriptions of one provider share a
	// provider).
	Provider string `json:"provider,omitempty"`
	// Model (optional) pins a model for this slot.
	Model string `json:"model,omitempty"`
	// SubscriptionID (optional) is a billing/account bucket WITHIN a provider (NOT
	// a secret). An account-side throttle poisons only this bucket, leaving sibling
	// subscriptions of the same provider usable.
	SubscriptionID string `json:"subscription_id,omitempty"`
}

// ResolvedSlot is a chain slot paired with its canonical name ("primary" /
// "fallback-N"), the form the overlay and routing layers address slots by.
type ResolvedSlot struct {
	// Name is the canonical slot name: "primary" or "fallback-<index>".
	Name string
	HarnessSlot
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
		// Per-field validation (shared with the per-agent workspace, which reuses
		// ValidateRecipe for ~/.flotilla/<agent>/launch.json).
		if err := ValidateRecipe(fmt.Sprintf("launch recipes %q: agent %q", path, name), r); err != nil {
			return nil, err
		}
		// tmux cross-recipe uniqueness is fleet-level (not part of single-recipe
		// validation): two recipes sharing a non-empty target would resume into the
		// same window, mirroring roster's shared-title rejection.
		if r.Tmux != "" {
			if other, dup := seenTmux[r.Tmux]; dup {
				return nil, fmt.Errorf("launch recipes %q: agents %q and %q share tmux target %q (would resume into the same window)", path, other, name, r.Tmux)
			}
			seenTmux[r.Tmux] = name
		}
	}
	return &c, nil
}

// UpsertAgent inserts an agent's recipe into the flat launch file at path when absent.
// When overwrite is false and the agent already has an entry, the file is unchanged
// (idempotent scaffold). Creates the file when missing. Returns true when a new entry
// was written, false when an existing entry was kept.
func UpsertAgent(path string, rosterAgents map[string]bool, agent string, recipe Recipe, overwrite bool) (bool, error) {
	if !rosterAgents[agent] {
		return false, fmt.Errorf("launch recipes %q: agent %q is not in the roster (typo?)", path, agent)
	}
	if err := ValidateRecipe(fmt.Sprintf("launch recipes %q: agent %q", path, agent), recipe); err != nil {
		return false, err
	}
	var c Config
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &c); err != nil {
			return false, fmt.Errorf("parse launch recipes %q: %w", path, err)
		}
	case os.IsNotExist(err):
		c = Config{Agents: map[string]Recipe{}}
	default:
		return false, fmt.Errorf("read launch recipes %q: %w", path, err)
	}
	if c.Agents == nil {
		c.Agents = map[string]Recipe{}
	}
	if _, exists := c.Agents[agent]; exists && !overwrite {
		return false, nil
	}
	if recipe.Tmux != "" {
		for name, r := range c.Agents {
			if name == agent {
				continue
			}
			if r.Tmux == recipe.Tmux {
				return false, fmt.Errorf("launch recipes %q: agents %q and %q share tmux target %q (would resume into the same window)", path, name, agent, recipe.Tmux)
			}
		}
	}
	c.Agents[agent] = recipe
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal launch recipes %q: %w", path, err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("create launch recipes dir %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+"-*.tmp")
	if err != nil {
		return false, fmt.Errorf("create temp for launch recipes %q: %w", path, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return false, fmt.Errorf("write launch recipes %q: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return false, fmt.Errorf("close launch recipes temp %q: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return false, fmt.Errorf("finalize launch recipes %q: %w", path, err)
	}
	return true, nil
}

// ValidateRecipe checks a single recipe's fields with the same rules Load applies per
// entry: launch required and free of \t\n\r; cwd required, absolute, free of \t\n\r
// (existence is checked at resume time, not load — the recipe may be read on another
// host); tmux optional and, if present, a plain session:window; state free of \t\n\r
// (it is printed for the operator/skill to parse). It does NOT check cross-recipe
// uniqueness (the fleet-level shared-tmux rejection) or roster membership — those are
// the caller's. `where` prefixes error messages (the flat file passes its path+agent;
// the workspace passes the workspace launch.json path).
func ValidateRecipe(where string, r Recipe) error {
	if r.Launch == "" {
		return fmt.Errorf("%s has an empty launch command", where)
	}
	if strings.ContainsAny(r.Launch, "\t\n\r") {
		return fmt.Errorf("%s launch %q contains a tab/newline", where, r.Launch)
	}
	if r.Cwd == "" {
		return fmt.Errorf("%s has an empty cwd", where)
	}
	if strings.ContainsAny(r.Cwd, "\t\n\r") {
		return fmt.Errorf("%s cwd %q contains a tab/newline", where, r.Cwd)
	}
	if !filepath.IsAbs(r.Cwd) {
		return fmt.Errorf("%s cwd %q is not absolute", where, r.Cwd)
	}
	if r.Tmux != "" {
		if strings.ContainsAny(r.Tmux, "\t\n\r") {
			return fmt.Errorf("%s tmux %q contains a tab/newline", where, r.Tmux)
		}
		if !validTmuxTarget(r.Tmux) {
			return fmt.Errorf("%s tmux %q is not a valid session:window target", where, r.Tmux)
		}
	}
	if strings.ContainsAny(r.State, "\t\n\r") {
		return fmt.Errorf("%s state %q contains a tab/newline", where, r.State)
	}
	// Per-slot validation for an explicit failover chain. It is surface-agnostic
	// (non-empty launch, no \t\n\r) — the surface known-driver check is DEFERRED to
	// switch/resume time to keep this package free of an internal/surface import
	// (an import-cycle guard, mirroring roster's cmd-layer surface validation). An
	// absent chain (the common case) skips this entirely.
	if r.Primary != nil {
		if err := validateSlot(fmt.Sprintf("%s primary slot", where), *r.Primary); err != nil {
			return err
		}
	}
	for i, f := range r.Fallbacks {
		if err := validateSlot(fmt.Sprintf("%s fallback slot %d", where, i), f); err != nil {
			return err
		}
	}
	if r.SingleHarness && len(r.Fallbacks) > 0 {
		return fmt.Errorf("%s declares single_harness=true and fallback slots (choose one posture)", where)
	}
	return nil
}

// Recipe returns the recipe for an agent and whether one is declared. An agent
// present in the roster but absent here is "declared but not resumable" — the
// caller errors clearly rather than guessing.
func (c *Config) Recipe(agent string) (Recipe, bool) {
	r, ok := c.Agents[agent]
	return r, ok
}

// UnprotectedAgents returns roster seats that have neither a usable failover
// target nor an explicit single-harness acknowledgement. Missing recipes,
// flat recipes, and primary-only recipes are all unprotected: auto-switch needs
// at least one fallback to select. Results are sorted for stable diagnostics.
func (c *Config) UnprotectedAgents(rosterAgents map[string]bool) []string {
	var names []string
	for name := range rosterAgents {
		r, ok := c.Recipe(name)
		if !ok || (!r.SingleHarness && len(r.Fallbacks) == 0) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Slots enumerates the recipe's harness chain in failover order: the primary
// slot first, then each fallback. It is the single helper callers (switch /
// resume / overlay resolution) use so the backward-compat rule lives in ONE
// place: when no explicit Primary is declared, the flat Launch is synthesized as
// the implied "primary" slot with an EMPTY Surface — the caller fills that from
// the roster Agent.surface (or the default driver), exactly as before this
// change. When an explicit Primary is declared it wins, and fallbacks follow as
// "fallback-0", "fallback-1", …. The returned slots are addressed by these
// canonical names by the active-harness overlay and routing.
func (r Recipe) Slots() []ResolvedSlot {
	// SingleHarness does not change runtime slot enumeration; it only silences
	// the chain-lint diagnostic for an intentionally unprotected seat.
	slots := make([]ResolvedSlot, 0, 1+len(r.Fallbacks))
	if r.Primary != nil {
		slots = append(slots, ResolvedSlot{Name: "primary", HarnessSlot: *r.Primary})
	} else {
		// Backward-compat: the flat Launch is the implied primary slot. Surface is
		// left empty for the caller to fill (roster surface or default); Cwd/Tmux/
		// State stay recipe-level and are not slot fields.
		slots = append(slots, ResolvedSlot{Name: "primary", HarnessSlot: HarnessSlot{Launch: r.Launch}})
	}
	for i, f := range r.Fallbacks {
		slots = append(slots, ResolvedSlot{Name: fmt.Sprintf("fallback-%d", i), HarnessSlot: f})
	}
	return slots
}

// validateSlot checks one harness chain slot: launch required and free of
// \t\n\r, holding to the same shape rule the flat Recipe.Launch obeys. The
// surface known-driver check is intentionally NOT here — it is deferred to
// switch/resume time (in cmd) to keep this package free of an internal/surface
// import. `where` prefixes the error message (the caller passes the
// agent-qualified slot label).
func validateSlot(where string, s HarnessSlot) error {
	if s.Launch == "" {
		return fmt.Errorf("%s has an empty launch command", where)
	}
	if strings.ContainsAny(s.Launch, "\t\n\r") {
		return fmt.Errorf("%s launch %q contains a tab/newline", where, s.Launch)
	}
	if strings.TrimSpace(s.SubscriptionID) != "" {
		if err := accounts.ValidateID(s.SubscriptionID); err != nil {
			return fmt.Errorf("%s subscription_id: %w", where, err)
		}
	}
	return nil
}

// validTmuxTarget reports whether s is a plain `session:window` target: exactly
// one ":" with a non-empty session and a non-empty window, no tmux pane-index
// suffix on the window (a trailing ".<digits>"), and no spaces in either half.
// resume derives the pane itself, so a window ending in ".<digits>" (e.g.
// "xo.0", "rel-1.2") is rejected — tmux would parse it as a pane
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
