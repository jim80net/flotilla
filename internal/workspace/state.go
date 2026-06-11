package workspace

import (
	"os"
	"path/filepath"
)

// File names within a workspace.
const (
	StateFileName     = "state.md"
	HeartbeatFileName = "HEARTBEAT.md"
)

// StatePointer returns the `/takeover` state pointer for an agent: the workspace
// state.md when it exists AND is non-empty, else the flat recipe's state field, else
// "" (no pointer). An empty scaffolded state.md MUST NOT print a pointer to an empty
// file — this mirrors resume's existing `state != ""` guard. resume surfaces the
// pointer for the operator/skill to drive /takeover; it never auto-restores context.
func StatePointer(agent, flatState string) (string, error) {
	dir, err := Dir(agent)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, StateFileName)
	if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() && info.Size() > 0 {
		return p, nil
	}
	return flatState, nil
}
