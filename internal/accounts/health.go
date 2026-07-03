package accounts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status values for accounts list / health probe.
const (
	StatusMissingCreds = "missing-creds"
	StatusUnreadable   = "unreadable"
	StatusExpired      = "expired"
	StatusExpiresSoon  = "expires-soon"
	StatusOK           = "ok"
	StatusNoCredsFile  = "no-creds-file" // dir exists but no .credentials.json yet after init
)

// ExpiresSoonWindow is how far ahead expires-soon is reported.
const ExpiresSoonWindow = 24 * time.Hour

// Health is a non-secret probe of one subscription's Claude config dir.
type Health struct {
	SubscriptionID   string    `json:"subscription_id"`
	ConfigDir        string    `json:"config_dir"`
	CredFileExists   bool      `json:"cred_file_exists"`
	CredFileMtime    time.Time `json:"cred_file_mtime,omitempty"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	SubscriptionType string    `json:"subscription_type,omitempty"`
	Status           string    `json:"status"`
}

type oauthMeta struct {
	ClaudeAiOauth struct {
		ExpiresAt        int64  `json:"expiresAt"`
		SubscriptionType string `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// CredentialsPath returns the expected credentials file for a config dir.
func CredentialsPath(configDir string) string {
	return filepath.Join(configDir, CredentialsFile)
}

// ProbeHealth reads non-secret metadata from a subscription config dir.
func ProbeHealth(id string, now time.Time) (Health, error) {
	id, err := NormalizeID(id)
	if err != nil {
		return Health{}, err
	}
	dir, err := ConfigDir(id)
	if err != nil {
		return Health{}, err
	}
	h := Health{
		SubscriptionID: id,
		ConfigDir:      dir,
		Status:         StatusNoCredsFile,
	}
	credPath := CredentialsPath(dir)
	info, err := os.Stat(credPath)
	if err != nil {
		if os.IsNotExist(err) {
			if _, derr := os.Stat(dir); derr != nil {
				h.Status = StatusMissingCreds
			}
			return h, nil
		}
		h.Status = StatusUnreadable
		return h, nil
	}
	h.CredFileExists = true
	h.CredFileMtime = info.ModTime()

	raw, err := os.ReadFile(credPath)
	if err != nil {
		h.Status = StatusUnreadable
		return h, nil
	}
	var meta oauthMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		h.Status = StatusUnreadable
		return h, nil
	}
	if meta.ClaudeAiOauth.ExpiresAt > 0 {
		h.ExpiresAt = time.UnixMilli(meta.ClaudeAiOauth.ExpiresAt)
	}
	h.SubscriptionType = strings.TrimSpace(meta.ClaudeAiOauth.SubscriptionType)
	h.Status = deriveStatus(h.ExpiresAt, now)
	return h, nil
}

func deriveStatus(expiresAt, now time.Time) string {
	if expiresAt.IsZero() {
		return StatusOK
	}
	if !expiresAt.After(now) {
		return StatusExpired
	}
	if expiresAt.Sub(now) <= ExpiresSoonWindow {
		return StatusExpiresSoon
	}
	return StatusOK
}

// List scans the accounts root and probes each subscription subdirectory.
func List(now time.Time) ([]Health, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read accounts root %q: %w", root, err)
	}
	var out []Health
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if err := ValidateID(id); err != nil {
			continue // skip non-subscription dirs without failing the whole list
		}
		h, err := ProbeHealth(id, now)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}
