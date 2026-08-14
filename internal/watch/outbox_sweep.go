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
	// Admit the oldest current order per SENDER-RECIPIENT LANE, not per recipient.
	// Ordering is only meaningful within a lane: two senders' messages to the same
	// recipient have no relative order to preserve, so coupling them throttles the
	// whole recipient to one delivery per sweep. Measured 2026-08-12: 448 of 453
	// queued messages had NEVER been attempted, and the busiest recipients were
	// GAINING backlog because admission (2-3/sweep fleet-wide) trailed inflow.
	seenLane := make(map[string]bool)
	for _, e := range pending {
		if !outbox.Current(s.rosterDir, e) {
			_, err := outbox.RemoveIfNonCurrent(s.rosterDir, e)
			if err != nil {
				log.Printf("flotilla watch: failed to garbage-collect canceled or superseded send %s from %q to %q (epoch %d): %v", e.ID, e.Sender, e.Recipient, e.Epoch, err)
			}
			continue
		}
		// Within a lane the head still cannot be queue-jumped: a busy-deferred head
		// blocks its own successors, preserving per-sender FIFO exactly as before.
		lane := e.Sender + "\x00" + e.Recipient
		if seenLane[lane] {
			continue
		}
		seenLane[lane] = true
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
