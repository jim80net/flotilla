package watch

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// alertedRecord tracks one surfaced un-acked operator message. WakeDone is set
// only after a confirmed coordinator injection; a busy skip leaves it false so
// the next sweep retries the wake without re-alerting.
type alertedRecord struct {
	MessageID string    `json:"message_id"`
	ChannelID string    `json:"channel_id"`
	AlertedAt time.Time `json:"alerted_at"`
	WakeDone  bool      `json:"wake_done"`
}

type unackedState struct {
	Records []alertedRecord `json:"records"`
}

// unackedStateStore persists dedup state for the un-acked backstop. Records older
// than retention are pruned on every save so the file cannot grow unbounded.
type unackedStateStore struct {
	path      string
	retention time.Duration
}

func newUnackedStateStore(path string, retention time.Duration) unackedStateStore {
	return unackedStateStore{path: path, retention: retention}
}

func (s unackedStateStore) load(now time.Time) unackedState {
	st := unackedState{}
	if s.path == "" {
		return st
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla watch: unacked state read failed for %q: %v (cold-starting)", s.path, err)
		}
		return st
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		log.Printf("flotilla watch: unacked state at %q is corrupt: %v (cold-starting)", s.path, err)
		return unackedState{}
	}
	return s.prune(st, now)
}

func (s unackedStateStore) save(st unackedState, now time.Time) error {
	if s.path == "" {
		return nil
	}
	st = s.prune(st, now)
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal unacked state: %w", err)
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create unacked state temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write unacked state temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close unacked state temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		cleanup()
		return fmt.Errorf("rename unacked state: %w", err)
	}
	return nil
}

func (s unackedStateStore) prune(st unackedState, now time.Time) unackedState {
	if s.retention <= 0 {
		return st
	}
	cutoff := now.Add(-s.retention)
	out := st.Records[:0]
	for _, r := range st.Records {
		if r.AlertedAt.IsZero() || !r.AlertedAt.Before(cutoff) {
			out = append(out, r)
		}
	}
	st.Records = out
	return st
}

func (st *unackedState) index(messageID string) (int, bool) {
	for i, r := range st.Records {
		if r.MessageID == messageID {
			return i, true
		}
	}
	return -1, false
}
