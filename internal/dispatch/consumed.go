// Package dispatch implements CNS Stratum A delivery observability (#614):
// a durable consumed registry (nonce + payload-hash), undelivered escalation
// markers, and status lookup across outbox / inbound / consumed.
package dispatch

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Consume reasons recorded on the durable registry.
const (
	ReasonTurnFinalAck = "turn-final-ack"
	ReasonMerged       = "merged"
	ReasonManual       = "manual"
	ReasonSuppressed   = "suppressed"
)

// maxConsumedEntries caps fleet-wide registry growth.
const maxConsumedEntries = 2048

// ConsumedEntry is one permanently-settled dispatch identity (#614).
// Keyed by Nonce when present; PayloadHash disambiguates same-nonce collisions
// and supports hash-only lookup when a message lacked a nonce stamp.
type ConsumedEntry struct {
	Nonce       string    `json:"nonce,omitempty"`
	PayloadHash string    `json:"payload_hash"`
	ConsumedAt  time.Time `json:"consumed_at"`
	Reason      string    `json:"reason"`
	Sender      string    `json:"sender,omitempty"`
	Recipient   string    `json:"recipient,omitempty"`
}

type consumedFile struct {
	Entries []ConsumedEntry `json:"entries"`
}

// ConsumedPath returns <roster-dir>/flotilla-dispatch-consumed.json.
func ConsumedPath(rosterDir string) string {
	if rosterDir == "" {
		return ""
	}
	return filepath.Join(rosterDir, "flotilla-dispatch-consumed.json")
}

// Registry is the durable consumed ledger for a fleet roster directory.
type Registry struct {
	path string
}

// NewRegistry opens the consumed registry under rosterDir (file may not exist yet).
func NewRegistry(rosterDir string) *Registry {
	return &Registry{path: ConsumedPath(rosterDir)}
}

// IsConsumed reports whether (nonce, payloadHash) was already settled.
// Match rules (any):
//   - non-empty nonce equals a registry entry's nonce (nonce is authoritative)
//   - both nonce and hash match (hash collision safety when nonce empty on one side)
//   - nonce empty on query: hash-only match
func (r *Registry) IsConsumed(nonce, payloadHash string) bool {
	if r == nil || r.path == "" {
		return false
	}
	nonce = strings.TrimSpace(nonce)
	payloadHash = strings.TrimSpace(payloadHash)
	if nonce == "" && payloadHash == "" {
		return false
	}
	for _, e := range r.Load() {
		if nonce != "" && e.Nonce == nonce {
			// Nonce match is decisive — same dispatch id must not reinject even if
			// a later stamp changed incidental whitespace in the body hash.
			return true
		}
		if nonce == "" && payloadHash != "" && e.PayloadHash == payloadHash {
			return true
		}
	}
	return false
}

// LookupNonce returns the first consumed entry for nonce, if any.
func (r *Registry) LookupNonce(nonce string) (ConsumedEntry, bool) {
	nonce = strings.TrimSpace(nonce)
	if r == nil || nonce == "" {
		return ConsumedEntry{}, false
	}
	for _, e := range r.Load() {
		if e.Nonce == nonce {
			return e, true
		}
	}
	return ConsumedEntry{}, false
}

// Consume records a settled dispatch. Idempotent: a second call with the same
// nonce (or same hash when nonce empty) is a no-op and returns inserted=false.
func (r *Registry) Consume(e ConsumedEntry) (inserted bool, err error) {
	if r == nil || r.path == "" {
		return false, nil
	}
	e.Nonce = strings.TrimSpace(e.Nonce)
	e.PayloadHash = strings.TrimSpace(e.PayloadHash)
	if e.Nonce == "" && e.PayloadHash == "" {
		return false, nil
	}
	if e.ConsumedAt.IsZero() {
		e.ConsumedAt = time.Now().UTC()
	}
	if e.Reason == "" {
		e.Reason = ReasonManual
	}
	err = r.withLock(func() error {
		f, rerr := r.readFileForUpdate()
		if rerr != nil {
			log.Printf("flotilla dispatch: read consumed registry failed: %v (starting empty)", rerr)
			f = consumedFile{}
		}
		for _, p := range f.Entries {
			if e.Nonce != "" && p.Nonce == e.Nonce {
				return nil // already consumed
			}
			if e.Nonce == "" && e.PayloadHash != "" && p.PayloadHash == e.PayloadHash && p.Nonce == "" {
				return nil
			}
		}
		f.Entries = append(f.Entries, e)
		if len(f.Entries) > maxConsumedEntries {
			f.Entries = f.Entries[len(f.Entries)-maxConsumedEntries:]
		}
		inserted = true
		return r.save(f)
	})
	return inserted, err
}

// Load returns a copy of all consumed entries (oldest first).
func (r *Registry) Load() []ConsumedEntry {
	if r == nil || r.path == "" {
		return nil
	}
	raw, err := os.ReadFile(r.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla dispatch: read consumed %q failed: %v", r.path, err)
		}
		return nil
	}
	var f consumedFile
	if err := json.Unmarshal(raw, &f); err != nil {
		log.Printf("flotilla dispatch: consumed %q corrupt: %v (empty)", r.path, err)
		return nil
	}
	out := make([]ConsumedEntry, 0, len(f.Entries))
	for _, e := range f.Entries {
		if e.Nonce == "" && e.PayloadHash == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Consume records under rosterDir. Convenience for callers without a Registry handle.
func Consume(rosterDir string, e ConsumedEntry) (inserted bool, err error) {
	return NewRegistry(rosterDir).Consume(e)
}

// IsConsumed is a convenience wrapper around Registry.IsConsumed.
func IsConsumed(rosterDir, nonce, payloadHash string) bool {
	return NewRegistry(rosterDir).IsConsumed(nonce, payloadHash)
}

// ConsumeFromInbound builds a ConsumedEntry from an inbound pending dispatch.
func ConsumeFromInbound(nonce, message, reason, sender, recipient string) ConsumedEntry {
	return ConsumedEntry{
		Nonce:       nonce,
		PayloadHash: PayloadHash(message),
		ConsumedAt:  time.Now().UTC(),
		Reason:      reason,
		Sender:      sender,
		Recipient:   recipient,
	}
}

func (r *Registry) readFileForUpdate() (consumedFile, error) {
	if r.path == "" {
		return consumedFile{}, nil
	}
	raw, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return consumedFile{}, nil
		}
		return consumedFile{}, fmt.Errorf("read consumed %q: %w", r.path, err)
	}
	var f consumedFile
	if err := json.Unmarshal(raw, &f); err != nil {
		sidecar := r.path + ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
		if renameErr := os.Rename(r.path, sidecar); renameErr != nil {
			log.Printf("flotilla dispatch: consumed %q corrupt (%v) and rename failed: %v", r.path, err, renameErr)
		} else {
			log.Printf("flotilla dispatch: consumed %q corrupt (%v); preserved as %q", r.path, err, sidecar)
		}
		return consumedFile{}, fmt.Errorf("corrupt consumed %q: %w", r.path, err)
	}
	return f, nil
}

func (r *Registry) save(f consumedFile) error {
	if r.path == "" {
		return nil
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal consumed: %w", err)
	}
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir consumed dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(r.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create consumed temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write consumed temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close consumed temp: %w", err)
	}
	if err := os.Rename(tmpName, r.path); err != nil {
		cleanup()
		return fmt.Errorf("rename consumed into place: %w", err)
	}
	return nil
}
