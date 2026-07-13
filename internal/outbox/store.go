// Package outbox is the durable per-sender queue for inter-agent `flotilla send`
// deliveries that could not land immediately (#475). Entries survive restarts and are
// swept by the watch daemon on heartbeat — the desk→coordinator complement of the
// operator relay queue (#286).
package outbox

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/sessionmirror"
)

// Entry is one pending inter-agent send keyed by ID within the sender's outbox file.
type Entry struct {
	ID                  string    `json:"id"`
	Sender              string    `json:"sender"`
	Recipient           string    `json:"recipient"`
	Message             string    `json:"message"`
	Epoch               uint64    `json:"epoch,omitempty"`
	Deferrals           int       `json:"deferrals"`
	EnqueuedAt          time.Time `json:"enqueued_at"`
	LastStaleEscalation time.Time `json:"last_stale_escalation,omitzero"` // exactly-once coordinator alert (#477)
}

type file struct {
	Pending []Entry           `json:"pending"`
	Epochs  map[string]uint64 `json:"epochs,omitempty"`
}

// CancelResult describes the sender→recipient generation stood down by Cancel.
type CancelResult struct {
	Sender    string
	Recipient string
	Canceled  int
	Epoch     uint64
}

// Store is a disk-backed outbox at a single sender's path.
type Store struct {
	path string
}

// Path returns <roster-dir>/flotilla-<sender>-outbox.json.
func Path(rosterDir, sender string) (string, error) {
	if err := sessionmirror.ValidateAgentName(sender); err != nil {
		return "", fmt.Errorf("outbox: %w", err)
	}
	return filepath.Join(rosterDir, "flotilla-"+sender+"-outbox.json"), nil
}

// NewStore opens the outbox at path (may not exist yet).
func NewStore(path string) Store {
	return Store{path: path}
}

// NewID returns a random hex id for a new outbox entry.
func NewID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("outbox: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Load reads all valid pending entries from the outbox file.
func (s Store) Load() []Entry {
	if s.path == "" {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla outbox: read failed for %q: %v (starting empty)", s.path, err)
		}
		return nil
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		log.Printf("flotilla outbox: %q is corrupt: %v (starting empty)", s.path, err)
		return nil
	}
	out := make([]Entry, 0, len(f.Pending))
	for _, e := range f.Pending {
		if e.ID == "" || e.Sender == "" || e.Recipient == "" || e.Message == "" {
			continue
		}
		e.Epoch = effectiveEpoch(e.Epoch)
		out = append(out, e)
	}
	return out
}

// Insert appends a new pending send. When an identical pending entry already exists
// (same recipient + nonce-stripped message hash), returns the existing id without appending (#484).
func (s Store) Insert(e Entry) (id string, deduped bool, err error) {
	if s.path == "" || e.Sender == "" || e.Recipient == "" || e.Message == "" {
		return "", false, nil
	}
	if e.ID == "" {
		e.ID, err = NewID()
		if err != nil {
			return "", false, err
		}
	}
	hash := messageHash(e.Message)
	err = s.withLock(func() error {
		f, rerr := s.readFileForUpdate()
		if rerr != nil {
			log.Printf("flotilla outbox: read for insert failed: %v (inserting single entry)", rerr)
			f = file{}
		}
		e.Epoch = currentEpoch(f, e.Recipient)
		for _, p := range f.Pending {
			if p.Recipient == e.Recipient && effectiveEpoch(p.Epoch) == e.Epoch && messageHash(p.Message) == hash {
				id = p.ID
				deduped = true
				return nil
			}
		}
		if e.EnqueuedAt.IsZero() {
			e.EnqueuedAt = time.Now().UTC()
		}
		f.Pending = append(f.Pending, e)
		id = e.ID
		return s.save(f)
	})
	if err != nil {
		return "", false, err
	}
	if deduped {
		log.Printf("flotilla outbox: send already queued as %s (%s→%s)", id, e.Sender, e.Recipient)
	}
	return id, deduped, nil
}

// Cancel advances the durable epoch for the sender→recipient pair containing id and
// removes every queued entry in that generation. A later send to the same recipient is
// stamped with the new epoch; already-swept jobs from the old epoch therefore fail Current.
func Cancel(rosterDir, id string) (CancelResult, error) {
	if rosterDir == "" || id == "" {
		return CancelResult{}, fmt.Errorf("outbox cancel: id not found")
	}
	matches, err := filepath.Glob(filepath.Join(rosterDir, "flotilla-*-outbox.json"))
	if err != nil {
		return CancelResult{}, fmt.Errorf("outbox cancel: scan: %w", err)
	}
	var paths []string
	for _, path := range matches {
		for _, e := range NewStore(path).Load() {
			if e.ID == id {
				paths = append(paths, path)
				break
			}
		}
	}
	if len(paths) == 0 {
		return CancelResult{}, fmt.Errorf("outbox cancel: id %q not found", id)
	}
	if len(paths) > 1 {
		return CancelResult{}, fmt.Errorf("outbox cancel: id %q is ambiguous across %d sender outboxes", id, len(paths))
	}

	st := NewStore(paths[0])
	var result CancelResult
	err = st.withLock(func() error {
		f, err := st.readFileForUpdate()
		if err != nil {
			return err
		}
		var target *Entry
		for i := range f.Pending {
			if f.Pending[i].ID == id {
				target = &f.Pending[i]
				break
			}
		}
		if target == nil {
			return fmt.Errorf("id %q no longer pending", id)
		}
		result.Sender = target.Sender
		result.Recipient = target.Recipient
		current := currentEpoch(f, target.Recipient)
		result.Epoch = current + 1
		if f.Epochs == nil {
			f.Epochs = make(map[string]uint64)
		}
		f.Epochs[target.Recipient] = result.Epoch
		next := f.Pending[:0]
		for _, p := range f.Pending {
			if p.Recipient == target.Recipient && effectiveEpoch(p.Epoch) <= current {
				result.Canceled++
				continue
			}
			next = append(next, p)
		}
		f.Pending = next
		return st.save(f)
	})
	if err != nil {
		return CancelResult{}, fmt.Errorf("outbox cancel: %w", err)
	}
	return result, nil
}

// Current reports whether e is still pending in the active sender→recipient epoch.
// It is used both by the watch sweep and immediately before injector delivery.
func Current(rosterDir string, e Entry) bool {
	path, err := Path(rosterDir, e.Sender)
	if err != nil {
		return false
	}
	st := NewStore(path)
	current := false
	if err := st.withLock(func() error {
		f, err := st.readFileForUpdate()
		if err != nil {
			return err
		}
		wantEpoch := effectiveEpoch(e.Epoch)
		if currentEpoch(f, e.Recipient) != wantEpoch {
			return nil
		}
		for _, pending := range f.Pending {
			if pending.ID == e.ID && pending.Recipient == e.Recipient && effectiveEpoch(pending.Epoch) == wantEpoch {
				current = true
				break
			}
		}
		return nil
	}); err != nil {
		log.Printf("flotilla outbox: current check failed for %s/%s: %v", e.Sender, e.ID, err)
		return false
	}
	return current
}

// AttemptCurrent linearizes a delivery attempt with Cancel under the sender outbox
// lock. If the entry is no longer current, attempt is not called. A confirmed attempt
// (nil error) removes the entry before releasing the lock; a failed/busy attempt leaves
// it pending. Thus a successful Cancel cannot race between an epoch check and send.
func AttemptCurrent(rosterDir string, e Entry, attempt func() error) (bool, error) {
	path, err := Path(rosterDir, e.Sender)
	if err != nil {
		return false, err
	}
	st := NewStore(path)
	attempted := false
	var attemptErr error
	err = st.withLock(func() error {
		f, err := st.readFileForUpdate()
		if err != nil {
			return err
		}
		wantEpoch := effectiveEpoch(e.Epoch)
		if currentEpoch(f, e.Recipient) != wantEpoch {
			return nil
		}
		index := -1
		for i, pending := range f.Pending {
			if pending.ID == e.ID && pending.Recipient == e.Recipient && effectiveEpoch(pending.Epoch) == wantEpoch {
				index = i
				break
			}
		}
		if index < 0 {
			return nil
		}
		attempted = true
		attemptErr = attempt()
		if attemptErr != nil {
			return nil
		}
		f.Pending = append(f.Pending[:index], f.Pending[index+1:]...)
		return st.save(f)
	})
	if err != nil {
		return attempted, fmt.Errorf("outbox delivery transaction: %w", err)
	}
	return attempted, attemptErr
}

// Update persists deferral bumps and stale-escalation markers on an existing entry by id (#484).
// Unknown ids are ignored — new sends use Insert via Enqueue, not Update.
func (s Store) Update(e Entry) {
	if s.path == "" || e.ID == "" {
		return
	}
	if err := s.withLock(func() error {
		f, err := s.readFileForUpdate()
		if err != nil {
			log.Printf("flotilla outbox: read for update failed: %v", err)
			return err
		}
		for i, p := range f.Pending {
			if p.ID == e.ID {
				if !entryMateriallyChanged(p, e) {
					return nil
				}
				f.Pending[i] = e
				return s.save(f)
			}
		}
		return nil
	}); err != nil {
		log.Printf("flotilla outbox: update failed: %v", err)
	}
}

// Remove deletes an entry by id under the same flock as Insert/Update.
func (s Store) Remove(id string) {
	if s.path == "" || id == "" {
		return
	}
	if err := s.withLock(func() error {
		f, err := s.readFileForUpdate()
		if err != nil {
			return fmt.Errorf("read for remove: %w", err)
		}
		if len(f.Pending) == 0 {
			return nil
		}
		next := f.Pending[:0]
		for _, p := range f.Pending {
			if p.ID != id {
				next = append(next, p)
			}
		}
		if len(next) == len(f.Pending) {
			return nil
		}
		f.Pending = next
		return s.save(f)
	}); err != nil {
		log.Printf("flotilla outbox: remove failed: %v", err)
	}
}

// Enqueue inserts a new pending send and returns its id. Identical pending sends dedup
// to the existing id (#484).
func Enqueue(rosterDir, sender, recipient, message string) (id string, deduped bool, err error) {
	path, err := Path(rosterDir, sender)
	if err != nil {
		return "", false, err
	}
	st := NewStore(path)
	id, deduped, err = st.Insert(Entry{
		Sender: sender, Recipient: recipient, Message: message,
		EnqueuedAt: time.Now().UTC(),
	})
	if err != nil {
		return "", false, fmt.Errorf("outbox enqueue: %w", err)
	}
	return id, deduped, nil
}

func messageHash(message string) string {
	sum := sha256.Sum256([]byte(inbound.StripDispatchFooter(message)))
	return hex.EncodeToString(sum[:8])
}

// ListAll scans rosterDir for flotilla-*-outbox.json files and returns all pending entries.
func ListAll(rosterDir string) []Entry {
	if rosterDir == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(rosterDir, "flotilla-*-outbox.json"))
	if err != nil {
		log.Printf("flotilla outbox: glob %q failed: %v", rosterDir, err)
		return nil
	}
	var out []Entry
	for _, path := range matches {
		out = append(out, NewStore(path).Load()...)
	}
	return out
}

func entryMateriallyChanged(prev, next Entry) bool {
	if prev.Sender != next.Sender || prev.Recipient != next.Recipient || prev.Message != next.Message {
		return true
	}
	if prev.EnqueuedAt.IsZero() != next.EnqueuedAt.IsZero() {
		return true
	}
	if !prev.EnqueuedAt.Equal(next.EnqueuedAt) {
		return true
	}
	if prev.Deferrals != next.Deferrals {
		return true
	}
	if effectiveEpoch(prev.Epoch) != effectiveEpoch(next.Epoch) {
		return true
	}
	if !prev.LastStaleEscalation.Equal(next.LastStaleEscalation) {
		return true
	}
	return false
}

func effectiveEpoch(epoch uint64) uint64 {
	if epoch == 0 {
		return 1
	}
	return epoch
}

func currentEpoch(f file, recipient string) uint64 {
	current := uint64(1)
	if epoch := f.Epochs[recipient]; epoch > current {
		current = epoch
	}
	for _, e := range f.Pending {
		if e.Recipient == recipient && effectiveEpoch(e.Epoch) > current {
			current = effectiveEpoch(e.Epoch)
		}
	}
	return current
}

func (s Store) readFileForUpdate() (file, error) {
	if s.path == "" {
		return file{}, nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return file{}, nil
		}
		return file{}, fmt.Errorf("read outbox %q: %w", s.path, err)
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		sidecar := s.path + ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
		if renameErr := os.Rename(s.path, sidecar); renameErr != nil {
			log.Printf("flotilla outbox: %q is corrupt (%v) and rename failed: %v", s.path, err, renameErr)
		} else {
			log.Printf("flotilla outbox: %q is corrupt (%v); preserved as %q", s.path, err, sidecar)
		}
		return file{}, fmt.Errorf("corrupt outbox %q: %w", s.path, err)
	}
	return f, nil
}

func (s Store) save(f file) error {
	if s.path == "" {
		return nil
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal outbox: %w", err)
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create outbox temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write outbox temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close outbox temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		cleanup()
		return fmt.Errorf("rename outbox into place: %w", err)
	}
	return nil
}

// SenderFromPath extracts the sender slug from flotilla-<sender>-outbox.json basename.
func SenderFromPath(path string) string {
	base := filepath.Base(path)
	const prefix = "flotilla-"
	const suffix = "-outbox.json"
	if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(base, prefix), "-outbox.json")
}
