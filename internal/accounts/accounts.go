// Package accounts maps logical subscription_id buckets (launch.HarnessSlot metadata,
// NOT secrets) to isolated Claude Code config directories via CLAUDE_CONFIG_DIR.
// Credentials are created by the operator's one-time `claude /login`; flotilla only
// scaffolds dirs, probes health, and wraps launch commands at resolution time.
package accounts

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const rootEnv = "FLOTILLA_ACCOUNTS_ROOT"

// ClaudeConfigSubdir is the per-subscription Claude Code config root (CLAUDE_CONFIG_DIR target).
const ClaudeConfigSubdir = "claude-config"

// CredentialsFile is Claude Code's OAuth store relative to the config dir.
const CredentialsFile = ".credentials.json"

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Root returns the accounts root: $FLOTILLA_ACCOUNTS_ROOT when set, else <home>/.flotilla/accounts.
func Root() (string, error) {
	if r := os.Getenv(rootEnv); r != "" {
		return r, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve accounts root: %w", err)
	}
	return filepath.Join(home, ".flotilla", "accounts"), nil
}

// NormalizeID trims, validates, and returns the canonical subscription id used for
// paths and lookups. Callers must use the returned id — not the raw input — so
// validation and on-disk layout always agree.
func NormalizeID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("subscription id is required")
	}
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("subscription id %q: want lowercase slug matching %s", id, idPattern.String())
	}
	return id, nil
}

// ValidateID rejects path-unsafe or empty subscription ids. IDs are lowercase slugs —
// generic mechanism; deployment-specific names live only in host-local recipes.
func ValidateID(id string) error {
	_, err := NormalizeID(id)
	return err
}

// ConfigDir returns the absolute CLAUDE_CONFIG_DIR path for a subscription id.
func ConfigDir(id string) (string, error) {
	id, err := NormalizeID(id)
	if err != nil {
		return "", err
	}
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, id, ClaudeConfigSubdir), nil
}

// Init creates the subscription config directory (0700) if absent.
func Init(id string) (string, error) {
	dir, err := ConfigDir(id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config dir %q: %w", dir, err)
	}
	return dir, nil
}

// IsClaudeSurface reports whether a harness surface uses Claude Code OAuth routing.
func IsClaudeSurface(surface string) bool {
	return surface == "" || surface == "claude-code"
}

// WrapClaudeLaunch prefixes a claude-code launch with CLAUDE_CONFIG_DIR when subscriptionID
// is set. Idempotent: skips when launch already exports CLAUDE_CONFIG_DIR.
func WrapClaudeLaunch(surface, subscriptionID, launch string) (string, error) {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if !IsClaudeSurface(surface) || subscriptionID == "" {
		return launch, nil
	}
	if strings.Contains(launch, "CLAUDE_CONFIG_DIR=") {
		return launch, nil
	}
	dir, err := ConfigDir(subscriptionID)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return fmt.Sprintf("export CLAUDE_CONFIG_DIR=%s; %s", shellQuote(abs), launch), nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
