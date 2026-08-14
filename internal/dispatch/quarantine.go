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

const (
	ReasonRecipientClosedOut = "recipient-closed-out"
	ReasonProviderRestored   = "provider-restored-confirmed-delivery"
	recipientStateKind       = "recipient-state"
)

// QuarantineEntry marks one undelivered row as recipient-unavailable. It is deliberately
// separate from ConsumedEntry: quarantine is not an acknowledgement or work-completion claim,
// preserves the source row, and may be lifted after the recipient accepts a confirmed delivery.
type QuarantineEntry struct {
	Kind          string    `json:"kind"`
	RowID         string    `json:"row_id"`
	Nonce         string    `json:"nonce,omitempty"`
	Sender        string    `json:"sender,omitempty"`
	Recipient     string    `json:"recipient"`
	PayloadHash   string    `json:"payload_hash,omitempty"`
	QuarantinedAt time.Time `json:"quarantined_at"`
	ReopenedAt    time.Time `json:"reopened_at,omitempty"`
	Reason        string    `json:"reason"`
}

type quarantineFile struct {
	Entries []QuarantineEntry `json:"entries"`
}

// QuarantinePath is the visible durable disposition ledger. It never aliases the consumed registry.
func QuarantinePath(rosterDir string) string {
	if rosterDir == "" {
		return ""
	}
	return filepath.Join(rosterDir, "flotilla-dispatch-quarantine.json")
}

// QuarantineRegistry stores reversible recipient-unavailable dispositions.
type QuarantineRegistry struct{ path string }

func NewQuarantineRegistry(rosterDir string) *QuarantineRegistry {
	return &QuarantineRegistry{path: QuarantinePath(rosterDir)}
}

// Quarantine records a row idempotently. The key is kind + row ID + recipient so unrelated
// delivery edges cannot suppress one another.
func (r *QuarantineRegistry) Quarantine(e QuarantineEntry) (inserted bool, err error) {
	if r == nil || r.path == "" {
		return false, nil
	}
	e.Kind = strings.TrimSpace(e.Kind)
	e.RowID = strings.TrimSpace(e.RowID)
	e.Recipient = strings.TrimSpace(e.Recipient)
	if e.Kind == "" || e.RowID == "" || e.Recipient == "" {
		return false, nil
	}
	if e.QuarantinedAt.IsZero() {
		e.QuarantinedAt = time.Now().UTC()
	}
	if e.Reason == "" {
		e.Reason = ReasonRecipientClosedOut
	}
	err = r.withLock(func() error {
		f, readErr := r.readFileForUpdate()
		if readErr != nil {
			return readErr
		}
		for i, p := range f.Entries {
			if sameQuarantineKey(p, e.Kind, e.RowID, e.Recipient) && p.ReopenedAt.IsZero() {
				return nil
			}
			if sameQuarantineKey(p, e.Kind, e.RowID, e.Recipient) {
				// Reuse the edge slot for a later close-out episode. The original source row
				// remains the durable payload/provenance; this registry records its latest
				// reversible disposition without growing once per lifecycle forever.
				f.Entries[i] = e
				inserted = true
				return r.save(f)
			}
		}
		f.Entries = append(f.Entries, e)
		inserted = true
		return r.save(f)
	})
	return inserted, err
}

func (r *QuarantineRegistry) IsQuarantined(kind, rowID, recipient string) (bool, error) {
	entries, err := r.loadStrict()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if sameQuarantineKey(e, kind, rowID, recipient) && e.ReopenedAt.IsZero() {
			return true, nil
		}
	}
	return false, nil
}

// Generation returns the latest disposition transition for recipient. Sweep folds it into the
// process-local alert key so a same-process reopen cannot inherit a pre-quarantine Fired mark.
func (r *QuarantineRegistry) Generation(recipient string) (int64, error) {
	entries, err := r.loadStrict()
	if err != nil {
		return 0, err
	}
	var latest time.Time
	for _, e := range entries {
		if e.Recipient != recipient {
			continue
		}
		for _, at := range []time.Time{e.QuarantinedAt, e.ReopenedAt} {
			if at.After(latest) {
				latest = at
			}
		}
	}
	if latest.IsZero() {
		return 0, nil
	}
	return latest.UnixNano(), nil
}

// ActiveByNonce returns active quarantined edges for status/audit. Callers must treat multiple
// matches as ambiguous because one dispatch nonce may traverse multiple recipient edges.
func (r *QuarantineRegistry) ActiveByNonce(nonce string) ([]QuarantineEntry, error) {
	entries, err := r.loadStrict()
	if err != nil {
		return nil, err
	}
	var out []QuarantineEntry
	for _, e := range entries {
		if e.Nonce == strings.TrimSpace(nonce) && e.ReopenedAt.IsZero() {
			out = append(out, e)
		}
	}
	return out, nil
}

// HasActiveRecipient reports whether any preserved row for recipient is currently quarantined.
func (r *QuarantineRegistry) HasActiveRecipient(recipient string) (bool, error) {
	entries, err := r.loadStrict()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Kind != recipientStateKind && e.Recipient == strings.TrimSpace(recipient) && e.ReopenedAt.IsZero() {
			return true, nil
		}
	}
	return false, nil
}

// ActiveRecipientCount returns the number of active quarantined source rows of kind for recipient.
// It is a strict read: callers deciding whether work is actionable must fail closed on error.
func (r *QuarantineRegistry) ActiveRecipientCount(kind, recipient string) (int, error) {
	entries, err := r.loadStrict()
	if err != nil {
		return 0, err
	}
	kind = strings.TrimSpace(kind)
	recipient = strings.TrimSpace(recipient)
	n := 0
	for _, e := range entries {
		if e.Kind == kind && e.Recipient == recipient && e.ReopenedAt.IsZero() {
			n++
		}
	}
	return n, nil
}

// ActiveRecipientEntries returns active source-row dispositions of kind for recipient. Callers
// correlating quarantine with another ledger must use these durable identities, never row counts.
func (r *QuarantineRegistry) ActiveRecipientEntries(kind, recipient string) ([]QuarantineEntry, error) {
	entries, err := r.loadStrict()
	if err != nil {
		return nil, err
	}
	kind = strings.TrimSpace(kind)
	recipient = strings.TrimSpace(recipient)
	var active []QuarantineEntry
	for _, e := range entries {
		if e.Kind == kind && e.Recipient == recipient && e.ReopenedAt.IsZero() {
			active = append(active, e)
		}
	}
	return active, nil
}

// RecipientRestoredAt returns the latest durable, turn-confirmed restoration for recipient.
// It lets an audit close-out document remain on disk without permanently overriding later
// delivery proof. A newer close-out event still wins.
func (r *QuarantineRegistry) RecipientRestoredAt(recipient string) (time.Time, error) {
	entries, err := r.loadStrict()
	if err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	for _, e := range entries {
		if e.Kind == recipientStateKind && e.Recipient == strings.TrimSpace(recipient) && e.ReopenedAt.After(latest) {
			latest = e.ReopenedAt
		}
	}
	return latest, nil
}

// ReopenRecipient tombstones active markers and records the confirmed restoration edge. Source
// inbound/outbox rows and the consumed registry are untouched; normal delivery/ack supervision
// resumes with a new durable alert generation. The single recipient-state entry is updated rather
// than appended on every later confirmed turn, keeping the audit ledger bounded per recipient.
func (r *QuarantineRegistry) ReopenRecipient(recipient string, now time.Time) (reopened int, err error) {
	if r == nil || r.path == "" || strings.TrimSpace(recipient) == "" {
		return 0, nil
	}
	recipient = strings.TrimSpace(recipient)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	err = r.withLock(func() error {
		f, readErr := r.readFileForUpdate()
		if readErr != nil {
			return readErr
		}
		// A sweep may notice a restoration race using its tick-start clock, which can be
		// older than the confirmed-delivery transition already recorded. Never move the
		// durable recipient state backward.
		for _, e := range f.Entries {
			if e.Kind == recipientStateKind && e.Recipient == recipient && e.ReopenedAt.After(now) {
				now = e.ReopenedAt
			}
		}
		for i := range f.Entries {
			if f.Entries[i].Kind != recipientStateKind && f.Entries[i].Recipient == recipient && f.Entries[i].ReopenedAt.IsZero() {
				f.Entries[i].ReopenedAt = now.UTC()
				reopened++
			}
		}
		for i := range f.Entries {
			if f.Entries[i].Kind == recipientStateKind && f.Entries[i].Recipient == recipient {
				f.Entries[i].ReopenedAt = now.UTC()
				f.Entries[i].Reason = ReasonProviderRestored
				return r.save(f)
			}
		}
		f.Entries = append(f.Entries, QuarantineEntry{
			Kind: recipientStateKind, RowID: recipient, Recipient: recipient,
			ReopenedAt: now.UTC(), Reason: ReasonProviderRestored,
		})
		return r.save(f)
	})
	return reopened, err
}

func (r *QuarantineRegistry) Load() []QuarantineEntry {
	entries, err := r.loadStrict()
	if err != nil {
		log.Printf("flotilla dispatch: read quarantine %q failed: %v", r.path, err)
		return nil
	}
	return entries
}

func (r *QuarantineRegistry) loadStrict() ([]QuarantineEntry, error) {
	if r == nil || r.path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f quarantineFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("decode quarantine %q: %w", r.path, err)
	}
	return append([]QuarantineEntry(nil), f.Entries...), nil
}

func sameQuarantineKey(e QuarantineEntry, kind, rowID, recipient string) bool {
	return e.Kind == strings.TrimSpace(kind) && e.RowID == strings.TrimSpace(rowID) && e.Recipient == strings.TrimSpace(recipient)
}

func (r *QuarantineRegistry) withLock(fn func() error) error {
	lock, err := acquireFileLock(r.path, dispatchLockTimeout)
	if err != nil {
		return err
	}
	defer lock.release()
	return fn()
}

func (r *QuarantineRegistry) readFileForUpdate() (quarantineFile, error) {
	raw, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return quarantineFile{}, nil
		}
		return quarantineFile{}, fmt.Errorf("read quarantine %q: %w", r.path, err)
	}
	var f quarantineFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return quarantineFile{}, fmt.Errorf("decode quarantine %q: %w", r.path, err)
	}
	return f, nil
}

func (r *QuarantineRegistry) save(f quarantineFile) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal quarantine: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("mkdir quarantine dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(r.path), filepath.Base(r.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create quarantine temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write quarantine temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close quarantine temp: %w", err)
	}
	if err := os.Rename(tmpName, r.path); err != nil {
		cleanup()
		return fmt.Errorf("rename quarantine into place: %w", err)
	}
	return nil
}
