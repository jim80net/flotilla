package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultRecentNotifySuppressTTL is how long after a successful flotilla notify the
// finish-edge auto-mirror skips Discord (#595 — one body per turn).
const DefaultRecentNotifySuppressTTL = 3 * time.Minute

// LastNotify is the post-notify stamp sidecar (flotilla-<agent>-last-notify.json).
type LastNotify struct {
	NotifiedAt time.Time `json:"notified_at"`
}

// RecordRecentNotify stamps a successful operator notify for agent.
func RecordRecentNotify(path string, at time.Time) error {
	if path == "" {
		return nil
	}
	rec := LastNotify{NotifiedAt: at.UTC()}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir recent notify dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RecentNotifyWithinTTL reports whether a notify stamp is within ttl of now.
// Missing file ⇒ false (mirror proceeds). Corrupt file ⇒ false (do not suppress mirror).
func RecentNotifyWithinTTL(path string, ttl time.Duration, now time.Time) bool {
	if path == "" || ttl <= 0 {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var rec LastNotify
	if err := json.Unmarshal(raw, &rec); err != nil || rec.NotifiedAt.IsZero() {
		return false
	}
	return now.Sub(rec.NotifiedAt.UTC()) < ttl
}
