// Package workspace describes the per-agent workspace `~/.flotilla/<agent>/` — the
// host-local home for a desk's heartbeat prompt (HEARTBEAT.md), working tracker
// (state.md), and runtime overlays (active-harness.json). Launch recipes live in the
// fleet-wide flat flotilla-launch.json only; per-workspace launch.json is deprecated.
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
// native convention: claude-code (and the empty default) → CLAUDE.md; aider →
// CONVENTIONS.md (its documented conventions file, loaded via `aider --read
// CONVENTIONS.md`); opencode/grok/cursor/codex/pi → AGENTS.md. An unknown surface is an error
// rather than a guessed name — the per-surface load mechanism is verified per driver
// (aider --read is documented; OpenCode loads AGENTS.md natively, packages/opencode/
// src/session/instruction.ts; Pi loads AGENTS.md natively (Pi coding-agent Context
// Files documentation); Grok → AGENTS.md (ASSUMED for xAI's official grok CLI — the
// deployed product; the prior provenance was superagent grok-dev, which the operator does not
// run, so re-verify AGENTS.md against the official grok and adjust if needed). Cursor is dropped;
// Pi loads AGENTS.md + CLAUDE.md natively — `pi --help` documents `--no-context-files` to
// disable "AGENTS.md and CLAUDE.md discovery"; flotilla identity scaffolding uses AGENTS.md
// as the portable desk identity, matching opencode/grok/codex).
func IdentityFileName(surface string) (string, error) {
	switch surface {
	case "", "claude-code":
		return "CLAUDE.md", nil
	case "aider":
		return "CONVENTIONS.md", nil
	case "opencode", "grok", "cursor", "codex", "pi":
		return "AGENTS.md", nil
	default:
		return "", fmt.Errorf("unknown surface %q: no identity-file convention", surface)
	}
}
