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
	paradeFooterRE = regexp.MustCompile(`\n*(?:Parade egress contract: include this exact marker in your turn-final footer:\n)?\[flotilla parade egress: ` + "`" + `([0-9a-f]{16})` + "`" + `\]\s*$`)
)

type paradePending struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

func paradePendingPath(rosterDir, agent, token string) (string, error) {
	if err := sessionmirror.ValidateAgentName(agent); err != nil {
		return "", fmt.Errorf("parade marker: %w", err)
	}
	if !paradeTokenRE.MatchString(token) {
		return "", fmt.Errorf("parade marker: invalid token")
	}
	return filepath.Join(rosterDir, "flotilla-"+agent+"-parade-pending-"+token+".json"), nil
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
	path, err := paradePendingPath(rosterDir, agent, token)
	if err != nil {
		return err
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
	match := paradeFooterRE.FindStringSubmatch(turnFinal)
	if len(match) != 2 {
		return false
	}
	path, err := paradePendingPath(rosterDir, agent, match[1])
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
	if match[1] != pending.Token {
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

func clearParadePending(rosterDir, agent, token string) {
	path, err := paradePendingPath(rosterDir, agent, token)
	if err != nil {
		return
	}
	// Token-specific filenames make cleanup atomic with respect to a later parade:
	// removing this request can never remove another request's marker.
	_ = os.Remove(path)
}
