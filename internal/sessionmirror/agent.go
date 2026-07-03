package sessionmirror

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// agentNamePattern matches roster agent slugs — same shape as accounts.NormalizeID
// (lowercase letter first, then [a-z0-9_-], max 64 runes). Rejects path separators
// and ".." so LedgerPath cannot traverse outside rosterDir.
var agentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// ValidateAgentName rejects empty or path-unsafe agent identifiers.
func ValidateAgentName(agent string) error {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return fmt.Errorf("sessionmirror: agent name is required")
	}
	if !agentNamePattern.MatchString(agent) {
		return fmt.Errorf("sessionmirror: agent name %q: want lowercase slug without path separators", agent)
	}
	return nil
}

// LedgerPath returns the per-agent jsonl path under rosterDir.
func LedgerPath(rosterDir, agent string) (string, error) {
	if err := ValidateAgentName(agent); err != nil {
		return "", err
	}
	return filepath.Join(rosterDir, "session-mirror", agent+".jsonl"), nil
}
