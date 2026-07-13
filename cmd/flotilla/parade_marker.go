package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

const paradeMarkerTTL = 2 * time.Hour

var (
	paradeTokenRE  = regexp.MustCompile(`^[0-9a-f]{16}$`)
	paradeFooterRE = regexp.MustCompile(`(?m)\n*(?:Parade egress contract: include this exact marker in your turn-final footer:\n)?\[flotilla parade egress: ` + "`" + `([0-9a-f]{16})` + "`" + `\]\s*$`)
)

type paradePending struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

func paradePendingPath(rosterDir, agent string) (string, error) {
	if err := sessionmirror.ValidateAgentName(agent); err != nil {
		return "", fmt.Errorf("parade marker: %w", err)
	}
	return filepath.Join(rosterDir, "flotilla-"+agent+"-parade-pending.json"), nil
}

func appendParadeEgressFooter(message, token string) string {
	return strings.TrimRight(message, "\n") +
		"\n\nParade egress contract: include this exact marker in your turn-final footer:\n" +
		"[flotilla parade egress: `" + token + "`]\n"
}

func stripParadeEgressFooter(message string) string {
	return strings.TrimRight(paradeFooterRE.ReplaceAllString(message, ""), "\n")
}

func markParadePending(rosterDir, agent, token string, now time.Time) error {
	path, err := paradePendingPath(rosterDir, agent)
	if err != nil {
		return err
	}
	if !paradeTokenRE.MatchString(token) {
		return fmt.Errorf("parade marker: invalid token")
	}
	if err := os.MkdirAll(rosterDir, 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(paradePending{Token: token, CreatedAt: now.UTC()})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// claimParadePending authorizes only the turn carrying this request's random token.
// An unrelated completion leaves the marker untouched; stale markers expire fail-quiet.
func claimParadePending(rosterDir, agent, turnFinal string, now time.Time) bool {
	path, err := paradePendingPath(rosterDir, agent)
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var pending paradePending
	if json.Unmarshal(raw, &pending) != nil || pending.Token == "" || pending.CreatedAt.IsZero() || now.Sub(pending.CreatedAt) > paradeMarkerTTL || now.Before(pending.CreatedAt.Add(-time.Minute)) {
		_ = os.Remove(path)
		return false
	}
	if !strings.Contains(turnFinal, "[flotilla parade egress: `"+pending.Token+"`]") {
		return false
	}
	claimed := path + ".claimed"
	if err := os.Rename(path, claimed); err != nil {
		return false
	}
	if err := os.Remove(claimed); err != nil {
		log.Printf("flotilla parade: remove claimed marker %q: %v", claimed, err)
	}
	return true
}
