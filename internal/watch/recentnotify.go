package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultRecentNotifySuppressTTL is how long after a successful flotilla notify the
// finish-edge auto-mirror skips Discord (#595 — one body per turn).
const DefaultRecentNotifySuppressTTL = 3 * time.Minute

// DefaultRecentNotifySameBodyTTL is how long a same-body fingerprint continues to
// suppress mirror Discord after notify (#628 dual-egress residual). Covers cases where
// the TTL window is tight but the mirror body is still the same cargo as notify.
const DefaultRecentNotifySameBodyTTL = 15 * time.Minute

// LastNotify is the post-notify stamp sidecar (flotilla-<agent>-last-notify.json).
type LastNotify struct {
	NotifiedAt time.Time `json:"notified_at"`
	// BodyHash is a fingerprint of the notify body (whitespace-normalized) for
	// same-body suppress when mirror re-posts equivalent cargo (#628).
	BodyHash string `json:"body_hash,omitempty"`
}

// NotifyBodyHash fingerprints notify/mirror candidate text for dual-egress dedup.
// Normalization: trim + collapse internal whitespace so minor modeling diffs still match.
func NotifyBodyHash(body string) string {
	norm := strings.Join(strings.Fields(strings.TrimSpace(body)), " ")
	if norm == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:16])
}

// RecordRecentNotify stamps a successful operator notify for agent.
// body may be empty (time-only suppress); when non-empty, BodyHash enables same-body suppress.
func RecordRecentNotify(path string, at time.Time, body string) error {
	if path == "" {
		return nil
	}
	rec := LastNotify{
		NotifiedAt: at.UTC(),
		BodyHash:   NotifyBodyHash(body),
	}
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
	suppress, _ := ShouldSuppressMirrorDiscord(path, ttl, DefaultRecentNotifySameBodyTTL, now, "")
	return suppress
}

// ShouldSuppressMirrorDiscord decides whether finish-edge / mirror-self should skip
// the Discord POST after a recent notify (#595 / #628).
//
// Returns greppable reason for INFO logs (never silent skip).
//
//  1. Stamp within ttl → "recent notify within 3m"
//  2. Same body hash within sameBodyTTL → "same-body as recent notify"
//  3. Otherwise → do not suppress
func ShouldSuppressMirrorDiscord(path string, ttl, sameBodyTTL time.Duration, now time.Time, candidateBody string) (suppress bool, reason string) {
	if path == "" || ttl <= 0 {
		return false, ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, ""
	}
	var rec LastNotify
	if err := json.Unmarshal(raw, &rec); err != nil || rec.NotifiedAt.IsZero() {
		return false, ""
	}
	age := now.Sub(rec.NotifiedAt.UTC())
	if age < 0 {
		age = 0
	}
	if age < ttl {
		return true, "recent notify within 3m"
	}
	if sameBodyTTL > 0 && candidateBody != "" && rec.BodyHash != "" {
		if rec.BodyHash == NotifyBodyHash(candidateBody) && age < sameBodyTTL {
			return true, "same-body as recent notify"
		}
	}
	return false, ""
}
