package watch

import (
	"log"
	"sync"

	"github.com/jim80net/flotilla/internal/outbox"
)

// OutboxSweeper enqueues pending inter-agent sends from per-sender outbox files (#475).
type OutboxSweeper struct {
	rosterDir string
	enqueue   func(Job)
	inFlight  sync.Map // entry key sender/id → struct{}
}

// NewOutboxSweeper builds a sweeper that delivers via the injector enqueue hook.
func NewOutboxSweeper(rosterDir string, enqueue func(Job)) *OutboxSweeper {
	return &OutboxSweeper{rosterDir: rosterDir, enqueue: enqueue}
}

func entryKey(sender, id string) string { return sender + "/" + id }

// SweepAll loads every pending outbox entry and enqueues KindSend jobs. Call once at watch
// startup (before live traffic) and on each heartbeat tick.
func (s *OutboxSweeper) SweepAll() int {
	if s == nil || s.rosterDir == "" || s.enqueue == nil {
		return 0
	}
	pending := outbox.ListAll(s.rosterDir)
	n := 0
	for _, e := range pending {
		if !outbox.Current(s.rosterDir, e) {
			log.Printf("flotilla watch: skipped canceled or superseded send %s from %q to %q (epoch %d)", e.ID, e.Sender, e.Recipient, e.Epoch)
			continue
		}
		key := entryKey(e.Sender, e.ID)
		if _, loaded := s.inFlight.LoadOrStore(key, struct{}{}); loaded {
			continue
		}
		s.enqueue(Job{
			Agent:               e.Recipient,
			IntendedRecipient:   e.Recipient,
			Message:             e.Message,
			Kind:                KindSend,
			MessageID:           e.ID,
			Sender:              e.Sender,
			Epoch:               e.Epoch,
			OutboxBound:         true,
			deferrals:           e.Deferrals,
			enqueuedAt:          e.EnqueuedAt,
			lastStaleEscalation: e.LastStaleEscalation,
		})
		n++
	}
	if n > 0 {
		log.Printf("flotilla watch: swept %d durable inter-agent send(s) from outboxes under %q", n, s.rosterDir)
	}
	return n
}

// Release clears the in-flight guard when a swept send completes or is dropped terminally.
func (s *OutboxSweeper) Release(sender, id string) {
	if s == nil || sender == "" || id == "" {
		return
	}
	s.inFlight.Delete(entryKey(sender, id))
}
