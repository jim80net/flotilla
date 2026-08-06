// Package messagebuffer implements the durable recipient-owned inbox used by
// pull-by-push delivery. Message bodies are committed here before any pane nudge.
package messagebuffer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/sessionmirror"
)

const Version = 1

var ErrNotFound = errors.New("message buffer: id not found")

// Entry is one immutable message body plus mutable pull/ack lifecycle metadata.
type Entry struct {
	ID              string     `json:"id"`
	Sender          string     `json:"sender"`
	Recipient       string     `json:"recipient"`
	Message         string     `json:"message"`
	Nonce           string     `json:"nonce,omitempty"`
	SenderSequence  uint64     `json:"sender_sequence"`
	EnqueuedAt      time.Time  `json:"enqueued_at"`
	PulledAt        *time.Time `json:"pulled_at,omitempty"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	Supersedes      []string   `json:"supersedes,omitempty"`
	SupersededBy    string     `json:"superseded_by,omitempty"`
	MigratedFrom    string     `json:"migrated_from,omitempty"`
	LegacyDeferrals int        `json:"legacy_deferrals,omitempty"`
}

// EnqueueOptions carries identity and migration/supersession metadata.
type EnqueueOptions struct {
	ID              string
	EnqueuedAt      time.Time
	Supersedes      []string
	MigratedFrom    string
	LegacyDeferrals int
}

type file struct {
	Version       int               `json:"version"`
	NextSenderSeq map[string]uint64 `json:"next_sender_sequence,omitempty"`
	Entries       []Entry           `json:"entries"`
}

// Summary is the third-party inspectable backlog view for one seat.
type Summary struct {
	Recipient  string        `json:"recipient"`
	Pending    int           `json:"pending"`
	Unread     int           `json:"unread"`
	Pulled     int           `json:"pulled"`
	Superseded int           `json:"superseded"`
	OldestAge  time.Duration `json:"-"`
	OldestAgeS int64         `json:"oldest_age_seconds"`
}

type Store struct{ path string }

func Path(rosterDir, recipient string) (string, error) {
	if err := sessionmirror.ValidateAgentName(recipient); err != nil {
		return "", fmt.Errorf("message buffer: %w", err)
	}
	return filepath.Join(rosterDir, "flotilla-"+recipient+"-buffer.json"), nil
}

func NewStore(path string) Store { return Store{path: path} }

func NewID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("message buffer: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Enqueue commits a body and supersession edges in one recipient-file transaction.
// Reusing an ID is idempotent only when immutable identity and body agree.
func Enqueue(rosterDir, sender, recipient, message string, opts EnqueueOptions) (Entry, bool, error) {
	path, err := Path(rosterDir, recipient)
	if err != nil {
		return Entry{}, false, err
	}
	if sender == "" || recipient == "" || strings.TrimSpace(message) == "" {
		return Entry{}, false, fmt.Errorf("message buffer: sender, recipient, and message are required")
	}
	if opts.ID == "" {
		opts.ID, err = NewID()
		if err != nil {
			return Entry{}, false, err
		}
	}
	st := NewStore(path)
	var result Entry
	var deduped bool
	err = st.withLock(func() error {
		f, err := st.read()
		if err != nil {
			return err
		}
		for _, existing := range f.Entries {
			if existing.ID != opts.ID {
				continue
			}
			if existing.Sender != sender || existing.Recipient != recipient || existing.Message != message {
				return fmt.Errorf("message buffer: id %q already names a different message", opts.ID)
			}
			result, deduped = existing, true
			return nil
		}
		targets := make(map[string]int, len(opts.Supersedes))
		for _, id := range opts.Supersedes {
			if id == "" || id == opts.ID {
				return fmt.Errorf("message buffer: invalid supersedes id %q", id)
			}
			found := -1
			for i := range f.Entries {
				if f.Entries[i].ID == id && f.Entries[i].Sender == sender && f.Entries[i].Recipient == recipient {
					found = i
					break
				}
			}
			if found < 0 {
				return fmt.Errorf("message buffer: superseded id %q is not a %s→%s message", id, sender, recipient)
			}
			targets[id] = found
		}
		if f.NextSenderSeq == nil {
			f.NextSenderSeq = make(map[string]uint64)
		}
		seq := f.NextSenderSeq[sender] + 1
		f.NextSenderSeq[sender] = seq
		enqueued := opts.EnqueuedAt.UTC()
		if enqueued.IsZero() {
			enqueued = time.Now().UTC()
		}
		result = Entry{
			ID: opts.ID, Sender: sender, Recipient: recipient, Message: message,
			Nonce: inbound.ParseOwnDispatchNonce(message), SenderSequence: seq,
			EnqueuedAt: enqueued, Supersedes: append([]string(nil), opts.Supersedes...),
			MigratedFrom: opts.MigratedFrom, LegacyDeferrals: opts.LegacyDeferrals,
		}
		f.Entries = append(f.Entries, result)
		for _, i := range targets {
			f.Entries[i].SupersededBy = result.ID
		}
		return st.save(f)
	})
	return result, deduped, err
}

// Pull stamps first observation durably and returns every unacknowledged entry.
// Repeated pulls return the same entries until ack, while newly arrived safety
// messages are visible immediately rather than hidden behind a leased batch.
func Pull(rosterDir, recipient string, now time.Time) ([]Entry, error) {
	path, err := Path(rosterDir, recipient)
	if err != nil {
		return nil, err
	}
	st := NewStore(path)
	var result []Entry
	err = st.withLock(func() error {
		f, err := st.read()
		if err != nil {
			return err
		}
		changed := false
		stamp := now.UTC()
		if stamp.IsZero() {
			stamp = time.Now().UTC()
		}
		for i := range f.Entries {
			if f.Entries[i].AcknowledgedAt != nil {
				continue
			}
			if f.Entries[i].PulledAt == nil {
				t := stamp
				f.Entries[i].PulledAt = &t
				changed = true
			}
			result = append(result, cloneEntry(f.Entries[i]))
		}
		if changed {
			return st.save(f)
		}
		return nil
	})
	return result, err
}

// AckID marks a pulled message handled. It is idempotent and never deletes audit history.
func AckID(rosterDir, recipient, id string, now time.Time) (Entry, bool, error) {
	path, err := Path(rosterDir, recipient)
	if err != nil {
		return Entry{}, false, err
	}
	st := NewStore(path)
	var result Entry
	var already bool
	err = st.withLock(func() error {
		f, err := st.read()
		if err != nil {
			return err
		}
		for i := range f.Entries {
			if f.Entries[i].ID != id {
				continue
			}
			if f.Entries[i].Recipient != recipient {
				return fmt.Errorf("message buffer: id %q belongs to recipient %q", id, f.Entries[i].Recipient)
			}
			if f.Entries[i].AcknowledgedAt != nil {
				result, already = cloneEntry(f.Entries[i]), true
				return nil
			}
			stamp := now.UTC()
			if stamp.IsZero() {
				stamp = time.Now().UTC()
			}
			f.Entries[i].AcknowledgedAt = &stamp
			result = cloneEntry(f.Entries[i])
			return st.save(f)
		}
		return fmt.Errorf("message buffer: id %q not found for %q", id, recipient)
	})
	return result, already, err
}

func FindNonce(rosterDir, recipient, nonce string) (Entry, bool) {
	path, err := Path(rosterDir, recipient)
	if err != nil {
		return Entry{}, false
	}
	for _, e := range NewStore(path).Load() {
		if e.Nonce == nonce {
			return e, true
		}
	}
	return Entry{}, false
}

// Cancel appends a visible cancellation control message and links the target as
// superseded. A recipient that already pulled the old instruction sees the stop
// on its next pull; history is never silently erased.
func Cancel(rosterDir, id string) (Entry, Entry, error) {
	var matches []Entry
	for _, e := range ListAll(rosterDir) {
		if e.ID == id {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return Entry{}, Entry{}, ErrNotFound
	}
	if len(matches) > 1 {
		return Entry{}, Entry{}, fmt.Errorf("message buffer: id %q is ambiguous", id)
	}
	target := matches[0]
	body := fmt.Sprintf("[flotilla cancellation] Stop work from buffered message %s. Its sender withdrew that instruction; do not take another action under it.", id)
	message, _, err := inbound.AppendDispatchNonce(body)
	if err != nil {
		return Entry{}, target, err
	}
	cancel, _, err := Enqueue(rosterDir, target.Sender, target.Recipient, message, EnqueueOptions{Supersedes: []string{id}})
	return cancel, target, err
}

func (s Store) Load() []Entry {
	f, err := s.read()
	if err != nil {
		log.Printf("flotilla message buffer: load %q failed: %v", s.path, err)
		return nil
	}
	out := make([]Entry, len(f.Entries))
	for i := range f.Entries {
		out[i] = cloneEntry(f.Entries[i])
	}
	return out
}

func Inspect(rosterDir, recipient string, now time.Time) (Summary, error) {
	path, err := Path(rosterDir, recipient)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Recipient: recipient}
	for _, e := range NewStore(path).Load() {
		if e.AcknowledgedAt != nil {
			continue
		}
		s.Pending++
		if e.PulledAt == nil {
			s.Unread++
		} else {
			s.Pulled++
		}
		if e.SupersededBy != "" {
			s.Superseded++
		}
		age := now.Sub(e.EnqueuedAt)
		if age > s.OldestAge {
			s.OldestAge = age
		}
	}
	if s.OldestAge > 0 {
		s.OldestAge = s.OldestAge.Round(time.Second)
		s.OldestAgeS = int64(s.OldestAge / time.Second)
	}
	return s, nil
}

func InspectAll(rosterDir string, now time.Time) ([]Summary, error) {
	matches, err := filepath.Glob(filepath.Join(rosterDir, "flotilla-*-buffer.json"))
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, path := range matches {
		base := filepath.Base(path)
		recipient := strings.TrimSuffix(strings.TrimPrefix(base, "flotilla-"), "-buffer.json")
		s, err := Inspect(rosterDir, recipient, now)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Recipient < out[j].Recipient })
	return out, nil
}

// ListAll returns every retained entry across recipient buffers for status/audit reads.
func ListAll(rosterDir string) []Entry {
	matches, err := filepath.Glob(filepath.Join(rosterDir, "flotilla-*-buffer.json"))
	if err != nil {
		return nil
	}
	var out []Entry
	for _, path := range matches {
		out = append(out, NewStore(path).Load()...)
	}
	return out
}

func (s Store) read() (file, error) {
	f := file{Version: Version, NextSenderSeq: make(map[string]uint64)}
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return file{}, fmt.Errorf("read %q: %w", s.path, err)
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return file{}, fmt.Errorf("decode %q: %w", s.path, err)
	}
	if f.Version != 0 && f.Version != Version {
		return file{}, fmt.Errorf("message buffer %q has unsupported version %d", s.path, f.Version)
	}
	f.Version = Version
	if f.NextSenderSeq == nil {
		f.NextSenderSeq = make(map[string]uint64)
		for _, e := range f.Entries {
			if e.SenderSequence > f.NextSenderSeq[e.Sender] {
				f.NextSenderSeq[e.Sender] = e.SenderSequence
			}
		}
	}
	return f, nil
}

func (s Store) save(f file) error {
	f.Version = Version
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (s Store) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	return fn()
}

func cloneEntry(e Entry) Entry {
	e.Supersedes = append([]string(nil), e.Supersedes...)
	if e.PulledAt != nil {
		t := *e.PulledAt
		e.PulledAt = &t
	}
	if e.AcknowledgedAt != nil {
		t := *e.AcknowledgedAt
		e.AcknowledgedAt = &t
	}
	return e
}
