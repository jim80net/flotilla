package roster

import (
	"fmt"
	"path/filepath"
	"strings"
)

// adjutantTarget returns the coordinator this agent adjutants for, preferring
// adjutant_for over the legacy assistant_for alias.
func (a Agent) adjutantTarget() string {
	if a.AdjutantFor != "" {
		return a.AdjutantFor
	}
	return a.AssistantFor
}

// AdjutantFor returns the adjutant agent bound to coordinator, or "" when none
// is configured. The legacy assistant_for alias is accepted at load.
func (c *Config) AdjutantFor(coordinator string) string {
	if coordinator == "" {
		return ""
	}
	for _, a := range c.Agents {
		if a.adjutantTarget() == coordinator {
			return a.Name
		}
	}
	return ""
}

// LayerAckPath returns the per-coordinator liveness ack sidecar for coordinator
// (e.g. flotilla-alpha-xo-alive under rosterDir).
func LayerAckPath(rosterDir, coordinator string) string {
	return filepath.Join(rosterDir, "flotilla-"+coordinator+"-alive")
}

// LayerBufferPath returns the adjutant interrupt buffer sidecar for a coordinator layer.
func LayerBufferPath(rosterDir, coordinator string) string {
	return filepath.Join(rosterDir, "flotilla-"+coordinator+"-buffer.json")
}

// LayerCharterPath returns the first-presentation charter file for a coordinator/adjutant pair.
func LayerCharterPath(rosterDir, coordinator string) string {
	return filepath.Join(rosterDir, "flotilla-"+coordinator+"-adjutant-charter.md")
}

// UrgentMaterial reports whether any reason matches an urgent_windows entry (phase 1c).
func (c *Config) UrgentMaterial(reasons []string) bool {
	if len(c.UrgentWindows) == 0 || len(reasons) == 0 {
		return false
	}
	for _, r := range reasons {
		rl := strings.ToLower(r)
		for _, w := range c.UrgentWindows {
			m := strings.TrimSpace(w.Match)
			if m != "" && strings.Contains(rl, strings.ToLower(m)) {
				return true
			}
		}
	}
	return false
}

func (c *Config) validateAdjutantBindings(path string) error {
	seen := make(map[string]string) // coordinator → adjutant name
	for _, a := range c.Agents {
		target := a.adjutantTarget()
		if target == "" {
			continue
		}
		if a.AdjutantFor != "" && a.AssistantFor != "" && a.AdjutantFor != a.AssistantFor {
			return fmt.Errorf("roster %q: agent %q sets both adjutant_for and assistant_for to different values", path, a.Name)
		}
		if target == a.Name {
			return fmt.Errorf("roster %q: agent %q cannot adjutant_for itself", path, a.Name)
		}
		if !c.IsCoordinator(target) {
			return fmt.Errorf("roster %q: agent %q adjutant_for %q is not a coordinator (XO)", path, a.Name, target)
		}
		if prev, dup := seen[target]; dup {
			return fmt.Errorf("roster %q: coordinators %q and %q both adjutant_for %q (at most one adjutant per coordinator)", path, prev, a.Name, target)
		}
		seen[target] = a.Name
	}
	return nil
}
