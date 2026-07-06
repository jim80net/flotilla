// Package outbox is the durable per-sender queue for inter-agent `flotilla send`
// deliveries that could not land immediately (#475). Entries survive restarts and are
// swept by the watch daemon on heartbeat — the desk→coordinator complement of the
// operator relay queue (#286).
package outbox

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

// Entry is one pending inter-agent send keyed by ID within the sender's outbox file.
type Entry struct {
	ID         string    `json:"id"`
	Sender     string    `json:"sender"`
	Recipient  string    `json:"recipient"`
	Message    string    `json:"message"`
	Deferrals  int       `json:"deferrals"`
	EnqueuedAt time.Time `json:"enqueued_at"`
}

type file struct {
	Pending []Entry `json:"pending"`
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
		out = append(out, e)
	}
	return out
}

// Upsert persists an entry when new or materially changed (not deferrals-only bumps).
func (s Store) Upsert(e Entry) {
	if s.path == "" || e.ID == "" {
		return
	}
	f, err := s.readFileForUpdate()
	if err != nil {
		log.Printf("flotilla outbox: read for upsert failed: %v (upserting single entry)", err)
		f = file{}
	}
	replaced := false
	for i, p := range f.Pending {
		if p.ID == e.ID {
			if !entryMateriallyChanged(p, e) {
				return
			}
			f.Pending[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		f.Pending = append(f.Pending, e)
	}
	if err := s.save(f); err != nil {
		log.Printf("flotilla outbox: upsert failed: %v", err)
	}
}

// Remove deletes an entry by id.
func (s Store) Remove(id string) {
	if s.path == "" || id == "" {
		return
	}
	f, err := s.readFileForUpdate()
	if err != nil {
		log.Printf("flotilla outbox: read for remove failed: %v", err)
		return
	}
	if len(f.Pending) == 0 {
		return
	}
	next := f.Pending[:0]
	for _, p := range f.Pending {
		if p.ID != id {
			next = append(next, p)
		}
	}
	if len(next) == len(f.Pending) {
		return
	}
	f.Pending = next
	if err := s.save(f); err != nil {
		log.Printf("flotilla outbox: remove failed: %v", err)
	}
}

// Enqueue appends a new pending send and returns its id.
func Enqueue(rosterDir, sender, recipient, message string) (string, error) {
	path, err := Path(rosterDir, sender)
	if err != nil {
		return "", err
	}
	id, err := NewID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	entry := Entry{
		ID:         id,
		Sender:     sender,
		Recipient:  recipient,
		Message:    message,
		EnqueuedAt: now,
	}
	NewStore(path).Upsert(entry)
	return id, nil
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
	return false
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