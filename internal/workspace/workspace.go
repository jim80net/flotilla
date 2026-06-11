// Package workspace describes the per-agent workspace `~/.flotilla/<agent>/` — the
// single host-local home for a desk's launch recipe (launch.json), heartbeat prompt
// (HEARTBEAT.md), working tracker (state.md), and identity in the agent's native
// instruction file (CLAUDE.md for claude-code, AGENTS.md for grok/cursor). It is the
// successor to the flat `flotilla-launch.json`, which remains a read-only migration
// fallback. See openspec/changes/agent-workspace/design.md.
//
// Resolution everywhere is fallback-defaulted, so a deployment with NO workspace
// behaves exactly as before the workspace existed (the flat launch file, the roster
// heartbeat prompt, the --tracker-file).
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// rootEnv overrides the workspace root (for tests and non-standard layouts),
// mirroring the watch flags' env-override pattern.
const rootEnv = "FLOTILLA_WORKSPACE_ROOT"

// Root returns the workspace root: $FLOTILLA_WORKSPACE_ROOT when set, else
// `<home>/.flotilla` where <home> is os.UserHomeDir() (which honors $HOME, then the
// passwd database). The daemon (flotilla watch) and the operator's interactive
// flotilla resume MUST resolve the same home, or a workspace the operator scaffolds
// would be invisible to the daemon — the shipped watch unit is a `systemctl --user`
// service (same user, same $HOME), which satisfies this.
func Root() (string, error) {
	if r := os.Getenv(rootEnv); r != "" {
		return r, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	return filepath.Join(home, ".flotilla"), nil
}

// Dir returns the workspace directory for an agent: <root>/<agent>.
func Dir(agent string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, agent), nil
}

// IdentityFileName returns the desk identity file name for a surface, by the agent's
// native convention: claude-code (and the empty default) → CLAUDE.md; grok/cursor →
// AGENTS.md. An unknown surface is an error rather than a guessed name — the per-surface
// load mechanism is verified per driver (the Grok/Cursor AGENTS.md load is unverified
// and deferred to the driver phase).
func IdentityFileName(surface string) (string, error) {
	switch surface {
	case "", "claude-code":
		return "CLAUDE.md", nil
	case "grok", "cursor":
		return "AGENTS.md", nil
	default:
		return "", fmt.Errorf("unknown surface %q: no identity-file convention", surface)
	}
}
