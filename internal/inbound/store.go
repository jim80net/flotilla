package inbound

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

type file struct {
	Pending []Entry `json:"pending"`
}

// Store is a disk-backed per-recipient inbound ledger at a single path.
type Store struct {
	path string
}

// Path returns <roster-dir>/flotilla-<recipient>-inbound.json.
func Path(rosterDir, recipient string) (string, error) {
	if err := sessionmirror.ValidateAgentName(recipient); err != nil {
		return "", fmt.Errorf("inbound: %w", err)
	}
	return filepath.Join(rosterDir, "flotilla-"+recipient+"-inbound.json"), nil
}

// NewStore opens the inbound ledger at path (may not exist yet).
func NewStore(path string) Store {
	return Store{path: path}
}

// Load reads all valid pending entries from the inbound file.
func (s Store) Load() []Entry {
	if s.path == "" {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla inbound: read failed for %q: %v (starting empty)", s.path, err)
		}
		return nil
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		log.Printf("flotilla inbound: %q is corrupt: %v (starting empty)", s.path, err)
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

// Track appends a confirmed inbound dispatch (idempotent on duplicate id).
func (s Store) Track(e Entry) {
	if e.ID == "" || e.Sender == "" || e.Recipient == "" || e.Message == "" {
		return
	}
	if e.DeliveredAt.IsZero() {
		e.DeliveredAt = time.Now().UTC()
	}
	if err := s.withLock(func() error {
		f, err := s.readFileForUpdate()
		if err != nil {
			log.Printf("flotilla inbound: read for track failed: %v (tracking single entry)", err)
			f = file{}
		}
		for _, p := range f.Pending {
			if p.ID == e.ID {
				return nil
			}
		}
		f.Pending = append(f.Pending, e)
		return s.save(f)
	}); err != nil {
		log.Printf("flotilla inbound: track failed: %v", err)
	}
}

// Upsert replaces or appends an entry. Unlike sender outbox, deferrals bumps ARE persisted
// (recipient-side reinject counting is load-bearing for #472).
func (s Store) Upsert(e Entry) {
	if s.path == "" || e.ID == "" {
		return
	}
	if err := s.withLock(func() error {
		f, err := s.readFileForUpdate()
		if err != nil {
			log.Printf("flotilla inbound: read for upsert failed: %v (upserting single entry)", err)
			f = file{}
		}
		replaced := false
		for i, p := range f.Pending {
			if p.ID == e.ID {
				f.Pending[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			f.Pending = append(f.Pending, e)
		}
		return s.save(f)
	}); err != nil {
		log.Printf("flotilla inbound: upsert failed: %v", err)
	}
}

// Remove deletes an entry by id under the same flock as Track.
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
		log.Printf("flotilla inbound: remove failed: %v", err)
	}
}

// OnFinish evaluates all pending entries against turnFinal and persists ack removals /
// deferral bumps. Returns actions for reinject or escalate-to-sender.
func (s Store) OnFinish(turnFinal string) []Action {
	if s.path == "" {
		return nil
	}
	var actions []Action
	if err := s.withLock(func() error {
		f, err := s.readFileForUpdate()
		if err != nil {
			return err
		}
		if len(f.Pending) == 0 {
			return nil
		}
		acts, remaining := evaluateFinish(f.Pending, turnFinal)
		actions = acts
		f.Pending = remaining
		return s.save(f)
	}); err != nil {
		log.Printf("flotilla inbound: on-finish failed: %v", err)
		return nil
	}
	return actions
}

// Record persists one confirmed inbound dispatch to the recipient's ledger file.
func Record(rosterDir string, e Entry) error {
	path, err := Path(rosterDir, e.Recipient)
	if err != nil {
		return err
	}
	if e.ID == "" {
		id, err := NewID()
		if err != nil {
			return err
		}
		e.ID = id
	}
	if e.Nonce == "" {
		nonce, err := NewNonce()
		if err != nil {
			return err
		}
		e.Nonce = nonce
	}
	if e.DeliveredAt.IsZero() {
		e.DeliveredAt = time.Now().UTC()
	}
	st := NewStore(path)
	if err := st.withLock(func() error {
		f, rerr := st.readFileForUpdate()
		if rerr != nil {
			f = file{}
		}
		for _, p := range f.Pending {
			if p.ID == e.ID {
				return nil
			}
		}
		f.Pending = append(f.Pending, e)
		return st.save(f)
	}); err != nil {
		return fmt.Errorf("inbound record: %w", err)
	}
	return nil
}

// ListAll scans rosterDir for flotilla-*-inbound.json files and returns all pending entries.
func ListAll(rosterDir string) []Entry {
	if rosterDir == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(rosterDir, "flotilla-*-inbound.json"))
	if err != nil {
		log.Printf("flotilla inbound: glob %q failed: %v", rosterDir, err)
		return nil
	}
	var out []Entry
	for _, path := range matches {
		out = append(out, NewStore(path).Load()...)
	}
	return out
}

// RecipientFromPath extracts the recipient slug from flotilla-<recipient>-inbound.json basename.
func RecipientFromPath(path string) string {
	base := filepath.Base(path)
	const prefix = "flotilla-"
	const suffix = "-inbound.json"
	if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(base, prefix), "-inbound.json")
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
		return file{}, fmt.Errorf("read inbound %q: %w", s.path, err)
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		sidecar := s.path + ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
		if renameErr := os.Rename(s.path, sidecar); renameErr != nil {
			log.Printf("flotilla inbound: %q is corrupt (%v) and rename failed: %v", s.path, err, renameErr)
		} else {
			log.Printf("flotilla inbound: %q is corrupt (%v); preserved as %q", s.path, err, sidecar)
		}
		return file{}, fmt.Errorf("corrupt inbound %q: %w", s.path, err)
	}
	return f, nil
}

func (s Store) save(f file) error {
	if s.path == "" {
		return nil
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal inbound: %w", err)
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create inbound temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write inbound temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close inbound temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		cleanup()
		return fmt.Errorf("rename inbound into place: %w", err)
	}
	return nil
}
