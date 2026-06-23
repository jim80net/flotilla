package watch

import (
	"sort"
	"sync"

	"github.com/jim80net/flotilla/internal/discord"
)

// defaultSeenCap bounds a per-channel seen-set. The set normally holds only ids in
// (cursor, latest] (pruned after every commit), so this cap only ever bites for a
// channel whose poll PERSISTENTLY fails to commit (a revoked permission / deleted
// channel) while the live path keeps adding ids — the F5 backstop. An evicted id
// at worst causes a re-relay (a duplicate) if the poll later recovers, never a
// lost relay.
const defaultSeenCap = 1024

// seenSet is a bounded set of recently-relayed snowflake ids for one channel.
// Not safe for concurrent use on its own — the dedup mutex serializes all access.
type seenSet struct {
	ids map[uint64]struct{}
	cap int
}

func newSeenSet(capacity int) *seenSet {
	if capacity <= 0 {
		capacity = defaultSeenCap
	}
	return &seenSet{ids: make(map[uint64]struct{}), cap: capacity}
}

func (s *seenSet) has(id uint64) bool { _, ok := s.ids[id]; return ok }

func (s *seenSet) add(id uint64) {
	s.ids[id] = struct{}{}
	if len(s.ids) > s.cap {
		s.evictLowest(len(s.ids) - s.cap)
	}
}

// pruneLE removes every id <= cursor. Because the poller only ever fetches
// after=cursor, a pruned id can never be re-fetched, so pruning is safe and keeps
// the set bounded to (cursor, latest] in the normal (committing) case.
func (s *seenSet) pruneLE(cursor uint64) {
	for id := range s.ids {
		if id <= cursor {
			delete(s.ids, id)
		}
	}
}

// evictLowest drops the n smallest ids — the oldest, most likely to fall below a
// future cursor. Only invoked on the rare never-commits overflow path, so the
// O(n log n) sort cost is acceptable.
func (s *seenSet) evictLowest(n int) {
	keys := make([]uint64, 0, len(s.ids))
	for id := range s.ids {
		keys = append(keys, id)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for i := 0; i < n && i < len(keys); i++ {
		delete(s.ids, keys[i])
	}
}

// dedup is the single shared gate the live gateway path and the catch-up poller
// both consult so an operator message is relayed AT LEAST ONCE without a
// double-relay. Two pieces of per-channel state:
//
//   - cursor: the highest snowflake the POLLER has processed. Advanced ONLY by the
//     poller (commit), never by the live path — the leapfrog guard (Invariant 1):
//     if the live path advanced it, a post-gap live message would push the cursor
//     past undelivered gap messages and the poller's after=cursor fetch would never
//     recover them.
//   - seen: ids already relayed (live or poll), so the two paths don't double-relay.
//
// The mutex is held ONLY for the in-memory map operations — NEVER across the REST
// fetch (the poller fetches off-lock) or injector.Enqueue (the poller enqueues
// off-lock); commit advances the cursor under the lock but persists OFF the lock.
type dedup struct {
	mu     sync.Mutex
	cursor map[string]uint64
	seen   map[string]*seenSet
	store  cursorStore
	cap    int
}

func newDedup(store cursorStore, seenCap int) *dedup {
	return &dedup{
		cursor: store.load(),
		seen:   map[string]*seenSet{},
		store:  store,
		cap:    seenCap,
	}
}

// cursorOf returns the channel's cursor and whether it has been initialized. An
// uninitialized channel first-boots (tail-init) on its next sweep.
func (d *dedup) cursorOf(channelID string) (uint64, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.cursor[channelID]
	return c, ok
}

// initCursor tail-initializes a channel's cursor to id WITHOUT relaying anything
// (first boot — never replay history). Persists off-lock.
func (d *dedup) initCursor(channelID string, id uint64) error {
	d.mu.Lock()
	d.cursor[channelID] = id
	snap := d.copyCursorLocked()
	d.mu.Unlock()
	return d.store.save(snap)
}

// liveNew reports whether a LIVE (gateway) message is new and should be relayed.
// It is called by Relay.Handle AFTER the Accept + empty-content guards, so the
// seen-set holds exactly the ids actually relayed (keeping Invariant 1's proof
// honest). It records the id in seen but DOES NOT advance the cursor. An id at or
// below the cursor is already covered by the poller's contiguous processing, so it
// is not re-relayed.
func (d *dedup) liveNew(channelID string, id uint64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if id <= d.cursor[channelID] {
		return false
	}
	s := d.seenOfLocked(channelID)
	if s.has(id) {
		return false
	}
	s.add(id)
	return true
}

// classify partitions an ascending, contiguous-from-cursor batch (the poller's
// fetched run) into the messages not already relayed (toRelay) and the new cursor
// (the batch's max id — the top of the fully-processed contiguous run). It marks
// toRelay as seen but DOES NOT advance the durable cursor and DOES NOT persist:
// the caller MUST enqueue every toRelay message FIRST, then call commit (F7 —
// enqueue-then-commit, so a crash in the window yields a duplicate, never a drop).
func (d *dedup) classify(channelID string, batch []discord.Message) (toRelay []discord.Message, newCursor uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cur := d.cursor[channelID]
	newCursor = cur
	s := d.seenOfLocked(channelID)
	for _, m := range batch {
		if m.SnowID <= cur {
			continue // below the frontier — already processed
		}
		if m.SnowID > newCursor {
			newCursor = m.SnowID
		}
		if s.has(m.SnowID) {
			continue // already relayed by the live path
		}
		s.add(m.SnowID)
		toRelay = append(toRelay, m)
	}
	return toRelay, newCursor
}

// commit advances the durable cursor to newCursor (monotonic), prunes the seen-set
// of ids now at or below the cursor, and persists. Called by the poller AFTER it
// has enqueued the toRelay messages (or surfaced them via an alert). Persist runs
// OFF the lock.
func (d *dedup) commit(channelID string, newCursor uint64) error {
	d.mu.Lock()
	if newCursor > d.cursor[channelID] {
		d.cursor[channelID] = newCursor
	}
	d.seenOfLocked(channelID).pruneLE(d.cursor[channelID])
	snap := d.copyCursorLocked()
	d.mu.Unlock()
	return d.store.save(snap)
}

// seenOfLocked returns (lazily creating) the channel's seen-set. Caller holds mu.
func (d *dedup) seenOfLocked(channelID string) *seenSet {
	s := d.seen[channelID]
	if s == nil {
		s = newSeenSet(d.cap)
		d.seen[channelID] = s
	}
	return s
}

// copyCursorLocked snapshots the cursor map for an off-lock persist. Caller holds mu.
func (d *dedup) copyCursorLocked() map[string]uint64 {
	out := make(map[string]uint64, len(d.cursor))
	for k, v := range d.cursor {
		out[k] = v
	}
	return out
}
