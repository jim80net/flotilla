package watch

import (
	"log"
	"sync"

	"github.com/jim80net/flotilla/internal/outbox"
)

// OutboxSweeper enqueues pending inter-agent sends from per-sender outbox files (#475).
type OutboxSweeper struct {
	rosterDir   string
	enqueue     func(Job)
	observeHead func(outbox.Entry)
	inFlight    sync.Map // entry key sender/id → struct{}
}

// NewOutboxSweeper builds a sweeper that delivers via the injector enqueue hook.
func NewOutboxSweeper(rosterDir string, enqueue func(Job)) *OutboxSweeper {
	return &OutboxSweeper{rosterDir: rosterDir, enqueue: enqueue}
}

// SetHeadObserver installs the sender→recipient lane-head observability hook. It
// runs for every admitted lane head on each sweep, including while one is in flight.
func (s *OutboxSweeper) SetHeadObserver(fn func(outbox.Entry)) { s.observeHead = fn }

func entryKey(sender, id string) string { return sender + "/" + id }

type outboxLane struct {
	sender    string
	recipient string
}

// SweepAll loads every pending outbox entry and enqueues KindSend jobs. Call once at watch
// startup (before live traffic) and on each heartbeat tick.
func (s *OutboxSweeper) SweepAll() int {
	if s == nil || s.rosterDir == "" || s.enqueue == nil {
		return 0
	}
	pending := outbox.ListAll(s.rosterDir)
	n := 0
	seenLane := make(map[outboxLane]bool)
	for _, e := range pending {
		if !outbox.Current(s.rosterDir, e) {
			_, err := outbox.RemoveIfNonCurrent(s.rosterDir, e)
			if err != nil {
				log.Printf("flotilla watch: failed to garbage-collect canceled or superseded send %s from %q to %q (epoch %d): %v", e.ID, e.Sender, e.Recipient, e.Epoch, err)
			}
			continue
		}
		// Admit only the oldest current order in each sender→recipient lane. A
		// busy head keeps its own corrections/retractions ordered, but cannot
		// starve another sender's lane to the same recipient.
		lane := outboxLane{sender: e.Sender, recipient: e.Recipient}
		if seenLane[lane] {
			continue
		}
		seenLane[lane] = true
		if s.observeHead != nil {
			s.observeHead(e)
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
