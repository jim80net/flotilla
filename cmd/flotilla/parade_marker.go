package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

func paradePendingPath(rosterDir, agent string) (string, error) {
	if err := sessionmirror.ValidateAgentName(agent); err != nil {
		return "", fmt.Errorf("parade marker: %w", err)
	}
	return filepath.Join(rosterDir, "flotilla-"+agent+"-parade-pending"), nil
}

func markParadePending(rosterDir, agent string) error {
	path, err := paradePendingPath(rosterDir, agent)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(rosterDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
}

// claimParadePending is the explicit allow gate for turn-final Discord egress.
// Rename makes one parade marker single-consumer even if two finish seams race.
func claimParadePending(rosterDir, agent string) bool {
	path, err := paradePendingPath(rosterDir, agent)
	if err != nil {
		return false
	}
	claimed := path + ".claimed"
	if err := os.Rename(path, claimed); err != nil {
		return false
	}
	_ = os.Remove(claimed)
	return true
}
