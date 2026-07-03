package watch

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// relayQueueFile is the on-disk pending operator-relay queue. Entries are keyed by
// MessageID (the origin medium's message id — a Discord snowflake today) and survive
// watch restarts until confirmed delivery removes them.
type relayQueueFile struct {
	Pending []pendingRelay `json:"pending"`
}

type pendingRelay struct {
	MessageID      string    `json:"message_id"`
	Agent          string    `json:"agent"`
	Message        string    `json:"message"`
	OriginChannel  string    `json:"origin_channel,omitempty"`
	Deferrals      int       `json:"deferrals"`
	EnqueuedAt     time.Time `json:"enqueued_at"`
	LastStaleAlert time.Time `json:"last_stale_alert_at,omitempty"`
}

type relayQueueStore struct {
	path string
}

func newRelayQueueStore(path string) relayQueueStore {
	return relayQueueStore{path: path}
}

func (s relayQueueStore) load() []Job {
	if s.path == "" {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla watch: relay queue read failed for %q: %v (starting empty)", s.path, err)
		}
		return nil
	}
	var f relayQueueFile
	if err := json.Unmarshal(raw, &f); err != nil {
		log.Printf("flotilla watch: relay queue at %q is corrupt: %v (starting empty)", s.path, err)
		return nil
	}
	out := make([]Job, 0, len(f.Pending))
	for _, p := range f.Pending {
		if p.MessageID == "" || p.Agent == "" || p.Message == "" {
			continue
		}
		out = append(out, Job{
			MessageID:      p.MessageID,
			Agent:          p.Agent,
			Message:        p.Message,
			Kind:           "relay",
			OriginChannel:  p.OriginChannel,
			deferrals:      p.Deferrals,
			enqueuedAt:     p.EnqueuedAt,
			lastStaleAlert: p.LastStaleAlert,
		})
	}
	return out
}

func (s relayQueueStore) upsert(j Job) {
	if s.path == "" || j.MessageID == "" || !isRelay(j.Kind) {
		return
	}
	f := s.readFile()
	entry := pendingRelay{
		MessageID:      j.MessageID,
		Agent:          j.Agent,
		Message:        j.Message,
		OriginChannel:  j.OriginChannel,
		Deferrals:      j.deferrals,
		EnqueuedAt:     j.enqueuedAt,
		LastStaleAlert: j.lastStaleAlert,
	}
	replaced := false
	for i, p := range f.Pending {
		if p.MessageID == j.MessageID {
			f.Pending[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		f.Pending = append(f.Pending, entry)
	}
	if err := s.save(f); err != nil {
		log.Printf("flotilla watch: relay queue upsert failed: %v", err)
	}
}

func (s relayQueueStore) remove(messageID string) {
	if s.path == "" || messageID == "" {
		return
	}
	f := s.readFile()
	if len(f.Pending) == 0 {
		return
	}
	next := f.Pending[:0]
	for _, p := range f.Pending {
		if p.MessageID != messageID {
			next = append(next, p)
		}
	}
	if len(next) == len(f.Pending) {
		return
	}
	f.Pending = next
	if err := s.save(f); err != nil {
		log.Printf("flotilla watch: relay queue remove failed: %v", err)
	}
}

func (s relayQueueStore) readFile() relayQueueFile {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return relayQueueFile{}
	}
	var f relayQueueFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return relayQueueFile{}
	}
	return f
}

func (s relayQueueStore) save(f relayQueueFile) error {
	if s.path == "" {
		return nil
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal relay queue: %w", err)
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create relay queue temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write relay queue temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close relay queue temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		cleanup()
		return fmt.Errorf("rename relay queue into place: %w", err)
	}
	return nil
}
