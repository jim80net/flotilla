package roster

import (
	"fmt"
	"os"
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
	return layerClockPath(rosterDir, coordinator, "alive")
}

// LayerSettledPath returns the canonical per-coordinator settle/idle marker path
// (flotilla-<coordinator>-settled). Watch resolves the on-disk path via
// ResolveLayerClockPath so legacy flotilla-xo-settled files keep working until migrated.
func LayerSettledPath(rosterDir, coordinator string) string {
	return layerClockPath(rosterDir, coordinator, "settled")
}

// LayerAwaitingPath returns the canonical per-coordinator awaiting-operator veto marker path
// (flotilla-<coordinator>-awaiting). Watch resolves the on-disk path via
// ResolveLayerClockPath so legacy flotilla-xo-awaiting files keep working until migrated.
func LayerAwaitingPath(rosterDir, coordinator string) string {
	return layerClockPath(rosterDir, coordinator, "awaiting")
}

func layerClockPath(rosterDir, coordinator, suffix string) string {
	return filepath.Join(rosterDir, "flotilla-"+coordinator+"-"+suffix)
}

// ResolveLayerClockPath picks the on-disk path for a coordinator clock artifact.
// An explicit flag/env path always wins. Otherwise the per-coordinator canonical path
// is preferred; when only the legacy flotilla-xo-* file exists, that path is kept so
// existing deployments behave byte-identically until the operator migrates.
func ResolveLayerClockPath(rosterDir, coordinator, explicit, legacyBasename, suffix string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	legacy := filepath.Join(rosterDir, legacyBasename)
	if coordinator == "" {
		return legacy
	}
	layer := layerClockPath(rosterDir, coordinator, suffix)
	if _, err := os.Stat(layer); err == nil {
		return layer
	}
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return layer
}

// LayerBufferPath returns the adjutant interrupt buffer sidecar for a coordinator layer.
func LayerBufferPath(rosterDir, coordinator string) string {
	return filepath.Join(rosterDir, "flotilla-"+coordinator+"-buffer.json")
}

// LayerBufferDeliveredPath returns the consumed-item ledger for seam-brief dedup (#469).
func LayerBufferDeliveredPath(rosterDir, coordinator string) string {
	return filepath.Join(rosterDir, "flotilla-"+coordinator+"-buffer-delivered.json")
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

// HasAdjutant reports whether any agent declares adjutant_for (or legacy assistant_for).
func (c *Config) HasAdjutant() bool {
	for _, a := range c.Agents {
		if a.adjutantTarget() != "" {
			return true
		}
	}
	return false
}

// validateSafeAgentName rejects path separators and traversal in agent identifiers
// used to build layer sidecar paths (LayerAckPath / LayerBufferPath / LayerCharterPath).
func validateSafeAgentName(path, name, field string) error {
	if name == "" {
		return nil
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("roster %q: %s %q contains a path separator", path, field, name)
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return fmt.Errorf("roster %q: %s %q contains path traversal", path, field, name)
	}
	return nil
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
	if c.HasAdjutant() && c.ChangeDetector {
		mode := c.LivenessPingMode
		if mode == "" || mode == "none" {
			return fmt.Errorf("roster %q: adjutant_for requires liveness_ping_mode interval or consecutive when change_detector is on (evaluation tick needs pings; none starves settled leaders)", path)
		}
	}
	return nil
}
