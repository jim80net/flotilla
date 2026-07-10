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

// RelayQueuePendingLayer reports pending operator relays for leader or its adjutant (#523).
func RelayQueuePendingLayer(path, leader, adjutant string) bool {
	q := newRelayQueueStore(path)
	if q.PendingForAgent(leader) {
		return true
	}
	if adjutant != "" && q.PendingForAgent(adjutant) {
		return true
	}
	return false
}

// InjectorRelayPendingLayer reports in-flight relays for leader or its adjutant (#523).
func InjectorRelayPendingLayer(in *Injector, leader, adjutant string) bool {
	if in == nil {
		return false
	}
	if in.HasPendingRelayFor(leader) {
		return true
	}
	if adjutant != "" && in.HasPendingRelayFor(adjutant) {
		return true
	}
	return false
}

// PendingForAgent reports whether the durable relay queue has a pending entry for agent (#523).
func (s relayQueueStore) PendingForAgent(agent string) bool {
	if agent == "" || s.path == "" {
		return false
	}
	for _, j := range s.load() {
		if j.Agent == agent {
			return true
		}
	}
	return false
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
			MessageID:       p.MessageID,
			Agent:           p.Agent,
			Message:         p.Message,
			Kind:            KindRelay,
			OriginChannel:   p.OriginChannel,
			deferrals:       p.Deferrals,
			enqueuedAt:      p.EnqueuedAt,
			lastStaleAlert:  p.LastStaleAlert,
			ingressResolved: true, // disk holds leader-path jobs post-Apply (#592)
		})
	}
	return out
}

// upsert persists a deferred operator relay when the on-disk record is new or materially
// changed (alert transitions, first enqueue timestamp). Deferrals-only bumps every 5s are
// skipped — the in-memory job carries the live count; disk holds delivery identity + alert state.
func (s relayQueueStore) upsert(j Job) {
	if s.path == "" || j.MessageID == "" || !isRelay(j.Kind) {
		return
	}
	f, err := s.readFileForUpdate()
	if err != nil {
		log.Printf("flotilla watch: relay queue read for upsert failed: %v (upserting single entry)", err)
		f = relayQueueFile{}
	}
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
			if !queueEntryMateriallyChanged(p, entry) {
				return
			}
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

func queueEntryMateriallyChanged(prev, next pendingRelay) bool {
	if prev.Agent != next.Agent || prev.Message != next.Message || prev.OriginChannel != next.OriginChannel {
		return true
	}
	if prev.EnqueuedAt.IsZero() != next.EnqueuedAt.IsZero() {
		return true
	}
	if !prev.EnqueuedAt.Equal(next.EnqueuedAt) {
		return true
	}
	if prev.LastStaleAlert.IsZero() != next.LastStaleAlert.IsZero() {
		return true
	}
	if !prev.LastStaleAlert.Equal(next.LastStaleAlert) {
		return true
	}
	return false
}

func (s relayQueueStore) remove(messageID string) {
	if s.path == "" || messageID == "" {
		return
	}
	f, err := s.readFileForUpdate()
	if err != nil {
		log.Printf("flotilla watch: relay queue read for remove failed: %v", err)
		return
	}
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

// readFileForUpdate reads the queue for a mutating operation. A corrupt file is renamed to a
// .corrupt-<timestamp> sidecar (preserving bytes for recovery) before returning empty — never
// silently overwritten without logging.
func (s relayQueueStore) readFileForUpdate() (relayQueueFile, error) {
	if s.path == "" {
		return relayQueueFile{}, nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return relayQueueFile{}, nil
		}
		return relayQueueFile{}, fmt.Errorf("read relay queue %q: %w", s.path, err)
	}
	var f relayQueueFile
	if err := json.Unmarshal(raw, &f); err != nil {
		sidecar := s.path + ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
		if renameErr := os.Rename(s.path, sidecar); renameErr != nil {
			log.Printf("flotilla watch: relay queue at %q is corrupt (%v) and rename to sidecar failed: %v", s.path, err, renameErr)
		} else {
			log.Printf("flotilla watch: relay queue at %q is corrupt (%v); preserved as %q", s.path, err, sidecar)
		}
		return relayQueueFile{}, fmt.Errorf("corrupt relay queue %q: %w", s.path, err)
	}
	return f, nil
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
