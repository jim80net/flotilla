package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultActiveConversationTTL is how long after a confirmed operator relay delivery
// the leader layer stays protected from routine adjutant seam inject (#523).
const DefaultActiveConversationTTL = 10 * time.Minute

// LastOperatorRelay is the post-confirmed relay tail sidecar (flotilla-<leader>-last-operator-relay.json).
type LastOperatorRelay struct {
	MessageID   string    `json:"message_id,omitempty"`
	DeliveredAt time.Time `json:"delivered_at"`
}

// RecordActiveConversation writes the confirmed-relay tail sidecar for leader.
func RecordActiveConversation(path, messageID string, at time.Time) error {
	if path == "" {
		return nil
	}
	rec := LastOperatorRelay{MessageID: messageID, DeliveredAt: at.UTC()}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir active conversation dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ActiveConversationTail reports whether the sidecar is within ttl of now.
// Unreadable or corrupt files fail-safe to protected (true).
func ActiveConversationTail(path string, ttl time.Duration, now time.Time) bool {
	if path == "" {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		return true
	}
	var rec LastOperatorRelay
	if err := json.Unmarshal(raw, &rec); err != nil || rec.DeliveredAt.IsZero() {
		return true
	}
	return now.Sub(rec.DeliveredAt.UTC()) < ttl
}

// ClearActiveConversation removes the tail sidecar when the leader resolves the seam.
func ClearActiveConversation(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
