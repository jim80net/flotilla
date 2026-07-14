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

	"github.com/jim80net/flotilla/internal/inbound"
)

// Consume reasons recorded on the durable registry.
const (
	ReasonTurnFinalAck = "turn-final-ack"
	ReasonDurableAck   = "durable-ack"
	ReasonQueuedAck    = "queued-ack"
	ReasonMerged       = "merged"
	ReasonManual       = "manual"
	ReasonSuppressed   = "suppressed"
	// ReasonCoordinatorRecipient: settled at send time because the recipient is a
	// coordinator seat, whose finish is deliberately not ack-gated (#472). Asserts
	// confirmed delivery only — NOT that the recipient addressed the work.
	ReasonCoordinatorRecipient = "coordinator-recipient"
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

// LookupNonce returns the consumed entry for nonce, if any. When both a real
// settlement (durable ack, turn-final ack, …) and a send-time
// coordinator-recipient entry exist for the same nonce (#707 — the same
// dispatch text can traverse more than one edge), the real settlement wins:
// it is the stronger claim.
func (r *Registry) LookupNonce(nonce string) (ConsumedEntry, bool) {
	nonce = strings.TrimSpace(nonce)
	if r == nil || nonce == "" {
		return ConsumedEntry{}, false
	}
	var coordinator *ConsumedEntry
	for _, e := range r.Load() {
		if e.Nonce != nonce {
			continue
		}
		if e.Reason != ReasonCoordinatorRecipient {
			return e, true
		}
		if coordinator == nil {
			c := e
			coordinator = &c
		}
	}
	if coordinator != nil {
		return *coordinator, true
	}
	return ConsumedEntry{}, false
}

// SettlesInboundRow reports whether a consumed entry settles the given
// recipient's inbound pending row. Like IsConsumed, except a send-time
// coordinator-recipient entry (#707) settles ONLY its own recipient's hop:
// the same dispatch text (same nonce) forwarded to a desk must keep that
// desk's reinject / escalation / undelivered supervision alive, because the
// coordinator hop's settlement says nothing about the desk having acted.
func (r *Registry) SettlesInboundRow(nonce, payloadHash, recipient string) bool {
	if r == nil || r.path == "" {
		return false
	}
	nonce = strings.TrimSpace(nonce)
	payloadHash = strings.TrimSpace(payloadHash)
	if nonce == "" && payloadHash == "" {
		return false
	}
	for _, e := range r.Load() {
		matched := false
		switch {
		case nonce != "" && e.Nonce == nonce:
			matched = true
		case nonce == "" && payloadHash != "" && e.PayloadHash == payloadHash:
			matched = true
		}
		if !matched {
			continue
		}
		if e.Reason == ReasonCoordinatorRecipient && e.Recipient != recipient {
			continue
		}
		return true
	}
	return false
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
				// A send-time coordinator-recipient entry settles only its own hop
				// (#707): per-edge settlements of the same nonce coexist, in either
				// insertion order — the hop entry must not block the true
				// recipient's real settlement, and an already-landed real
				// settlement must not block a later hop entry (whose absence would
				// re-break the coordinator's footer ack).
				if (p.Reason == ReasonCoordinatorRecipient || e.Reason == ReasonCoordinatorRecipient) &&
					p.Recipient != e.Recipient {
					continue
				}
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

// ConsumeCoordinatorRecipient durably settles a confirmed delivery to a
// coordinator seat at send time (#707). Coordinator recipients keep no inbound
// pending row (#472 — finish evaluation must not grow unbounded), which
// previously left their dispatches with NO durable trace anywhere: the send
// footer's `flotilla dispatch-ack` could never succeed ("not pending") and
// `dispatch-status` read unknown minutes after a confirmed delivery. Recording
// the nonce in the consumed registry closes both loops — dispatch-ack converges
// on the already-durable path and status resolves with this reason — without
// growing any per-seat pending ledger.
//
// Only the message's OWN footer nonce settles (ParseOwnDispatchNonce): a
// coordinator-directed report that merely QUOTES another dispatch's nonce in
// prose settles nothing — consuming a quoted nonce would silently disable the
// reinject / escalation supervision of the desk that dispatch actually targets.
func ConsumeCoordinatorRecipient(rosterDir, sender, recipient, message string) (inserted bool, err error) {
	nonce := inbound.ParseOwnDispatchNonce(message)
	if nonce == "" {
		return false, nil
	}
	return Consume(rosterDir, ConsumedEntry{
		Nonce:       nonce,
		PayloadHash: PayloadHash(message),
		Reason:      ReasonCoordinatorRecipient,
		Sender:      sender,
		Recipient:   recipient,
	})
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
