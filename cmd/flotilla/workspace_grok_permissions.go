package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Desk allowlist is the embed of deploy/grok-permission-allowlist.json (kept in
// package for go:embed). TestGrokDeskAllowlistMatchesDeploy guards drift.
//
//go:embed grok_desk_allowlist.json
var grokDeskAllowlistJSON []byte

// grokDeskAllowlist is the deploy/grok-permission-allowlist.json shape.
type grokDeskAllowlist struct {
	Tiers struct {
		ReadUnprompted struct {
			Allow []string `json:"allow"`
		} `json:"read_unprompted"`
		NeverAutonomous struct {
			Deny []string `json:"deny"`
		} `json:"never_autonomous"`
	} `json:"tiers"`
}

func parseGrokDeskAllowlist() (grokDeskAllowlist, error) {
	var doc grokDeskAllowlist
	if err := json.Unmarshal(grokDeskAllowlistJSON, &doc); err != nil {
		return doc, fmt.Errorf("parse grok desk allowlist: %w", err)
	}
	return doc, nil
}

// scaffoldGrokDeskPermissions writes worktree .claude/settings.local.json from
// the desk-tier allowlist (deploy/grok-permission-allowlist.json). Fail-safe:
// deny includes merge authority (gh pr merge) so a freshly provisioned desk is
// not unrestricted. Idempotent: keeps an existing settings file.
func scaffoldGrokDeskPermissions(worktreeAbs string) error {
	doc, err := parseGrokDeskAllowlist()
	if err != nil {
		return err
	}
	settings := map[string]any{
		// Gatekeeper posture marker (same abstain default as coordinator scaffold).
		"policy": map[string]any{
			"on_gatekeeper_error": "abstain",
			"description":         "When the gatekeeper/permission layer errors, abstain and let the native permission system decide.",
		},
		"permissions": map[string]any{
			"allow": doc.Tiers.ReadUnprompted.Allow,
			"deny":  doc.Tiers.NeverAutonomous.Deny,
		},
	}
	return writeGrokSettingsLocal(worktreeAbs, settings, "desk")
}

// writeGrokSettingsLocal creates .claude/settings.local.json unless present.
func writeGrokSettingsLocal(worktreeAbs string, settings map[string]any, label string) error {
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal grok %s settings: %w", label, err)
	}
	claudeDir := filepath.Join(worktreeAbs, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	path := filepath.Join(claudeDir, "settings.local.json")
	if info, statErr := os.Stat(path); statErr == nil {
		if info.IsDir() {
			return fmt.Errorf("grok %s settings %q exists but is a directory", label, path)
		}
		fmt.Printf("  kept    %s\n", path)
		return nil
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat grok %s settings %q: %w", label, path, statErr)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write grok %s settings %q: %w", label, path, err)
	}
	fmt.Printf("  created %s\n", path)
	return nil
}

// grokDeskLaunchCommand builds the execution-desk grok launch: model grok-4.5,
// always-approve (by-design for execution desks), plus --allow/--deny from the
// desk allowlist so a fresh workspace init is not fail-open.
func grokDeskLaunchCommand() (string, error) {
	doc, err := parseGrokDeskAllowlist()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("grok --model grok-4.5 --always-approve")
	for _, rule := range doc.Tiers.ReadUnprompted.Allow {
		b.WriteString(" --allow ")
		b.WriteString(shellQuote(rule))
	}
	for _, rule := range doc.Tiers.NeverAutonomous.Deny {
		b.WriteString(" --deny ")
		b.WriteString(shellQuote(rule))
	}
	return b.String(), nil
}
