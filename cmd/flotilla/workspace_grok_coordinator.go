package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

//go:embed grok_coordinator_allowlist.json
var grokCoordinatorAllowlistJSON []byte

type grokCoordinatorAllowlist struct {
	Policy struct {
		OnGatekeeperError string `json:"on_gatekeeper_error"`
		Description       string `json:"description"`
	} `json:"policy"`
	Tiers struct {
		ReadUnprompted struct {
			Allow []string `json:"allow"`
		} `json:"read_unprompted"`
		NeverAutonomous struct {
			Deny []string `json:"deny"`
		} `json:"never_autonomous"`
	} `json:"tiers"`
}

// grokCoordinatorCandidate reports whether the roster names this coordinator for grok.
func grokCoordinatorCandidate(cfg *roster.Config, agent, rosterSurface string) bool {
	return cfg.IsCoordinator(agent) && rosterSurface == "grok"
}

// refuseGrokCoordinatorWithoutProbe enforces symmetric fail-closed gate with codex:
// grok coordinators require ComposerStateProbe before workspace init scaffolds a seat.
func refuseGrokCoordinatorWithoutProbe(agent string) error {
	drv, ok := surface.Get("grok")
	if !ok {
		return fmt.Errorf("workspace init %q: grok coordinator provisioning refused — grok surface driver not registered", agent)
	}
	if _, ok := drv.(surface.ComposerStateProbe); !ok {
		return fmt.Errorf("workspace init %q: grok coordinator provisioning refused — grok driver lacks ComposerStateProbe (ship probe before init)", agent)
	}
	return nil
}

func scaffoldGrokCoordinatorPermissions(worktreeAbs string) error {
	var doc grokCoordinatorAllowlist
	if err := json.Unmarshal(grokCoordinatorAllowlistJSON, &doc); err != nil {
		return fmt.Errorf("parse grok coordinator allowlist: %w", err)
	}
	settings := map[string]any{
		"policy": doc.Policy,
		"permissions": map[string]any{
			"allow": doc.Tiers.ReadUnprompted.Allow,
			"deny":  doc.Tiers.NeverAutonomous.Deny,
		},
	}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal grok coordinator settings: %w", err)
	}
	claudeDir := filepath.Join(worktreeAbs, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	path := filepath.Join(claudeDir, "settings.local.json")
	if info, statErr := os.Stat(path); statErr == nil {
		if info.IsDir() {
			return fmt.Errorf("grok coordinator settings %q exists but is a directory", path)
		}
		fmt.Printf("  kept    %s\n", path)
		return nil
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat grok coordinator settings %q: %w", path, statErr)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write grok coordinator settings %q: %w", path, err)
	}
	fmt.Printf("  created %s\n", path)
	return nil
}
