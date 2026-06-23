package watch

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/jim80net/flotilla/internal/discord"
)

func newTestDedup(t *testing.T) *dedup {
	t.Helper()
	return newDedup(cursorStore{path: filepath.Join(t.TempDir(), "cursor.json")}, defaultSeenCap)
}

func dmsg(id uint64) discord.Message {
	return discord.Message{ID: itoa(id), SnowID: id, AuthorID: "op", Content: "m"}
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func snowIDs(ms []discord.Message) []uint64 {
	out := make([]uint64, len(ms))
	for i, m := range ms {
		out[i] = m.SnowID
	}
	return out
}

func TestLiveNew_RecordsButDoesNotAdvanceCursor(t *testing.T) {
	d := newTestDedup(t)
	if !d.liveNew("CH", 10) {
		t.Fatal("first id should be new")
	}
	if d.liveNew("CH", 10) {
		t.Fatal("repeat id should NOT be new")
	}
	if c, _ := d.cursorOf("CH"); c != 0 {
		t.Fatalf("liveNew advanced cursor to %d, want 0 (only the poller advances it)", c)
	}
}

func TestClassify_DedupsAgainstSeen_ReturnsMaxCursor(t *testing.T) {
	d := newTestDedup(t)
	d.liveNew("CH", 20) // pretend the live path already relayed 20
	toRelay, newCur := d.classify("CH", []discord.Message{dmsg(10), dmsg(20), dmsg(30)})
	if got := snowIDs(toRelay); len(got) != 2 || got[0] != 10 || got[1] != 30 {
		t.Fatalf("toRelay = %v, want [10 30] (20 already seen)", got)
	}
	if newCur != 30 {
		t.Fatalf("newCursor = %d, want 30", newCur)
	}
	// classify must NOT advance the durable cursor (that's commit's job, after enqueue).
	if c, _ := d.cursorOf("CH"); c != 0 {
		t.Fatalf("classify advanced cursor to %d, want 0", c)
	}
}

// TestLeapfrog is the load-bearing correctness scenario (Invariant 1): a post-gap
// live message must NOT push the cursor past undelivered gap messages.
func TestLeapfrog_LiveAfterGapDoesNotOrphanGapMessages(t *testing.T) {
	d := newTestDedup(t)
	// cursor starts at 2 (poller processed up to 2).
	if err := d.initCursor("CH", 2); err != nil {
		t.Fatal(err)
	}
	// Gap: m3, m4 arrive during a gateway gap (never seen live).
	// m5 IS delivered live after the gateway recovers.
	if !d.liveNew("CH", 5) {
		t.Fatal("m5 should be new")
	}
	if c, _ := d.cursorOf("CH"); c != 2 {
		t.Fatalf("live m5 advanced cursor to %d, want 2 — the leapfrog bug", c)
	}
	// Next poll fetches after=2 → [3,4,5] ascending. m3,m4 must be recovered; m5 skipped (seen).
	toRelay, newCur := d.classify("CH", []discord.Message{dmsg(3), dmsg(4), dmsg(5)})
	if got := snowIDs(toRelay); len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("recovered = %v, want [3 4] (gap messages); m5 already relayed live", got)
	}
	if newCur != 5 {
		t.Fatalf("newCursor = %d, want 5", newCur)
	}
}

// TestEnqueueThenCommit verifies F7: classify does not advance/persist the cursor;
// a "crash" (skipping commit) leaves the cursor old so the next sweep re-fetches
// (a duplicate), never a drop.
func TestEnqueueThenCommit_CrashBeforeCommitReDelivers(t *testing.T) {
	d := newTestDedup(t)
	d.initCursor("CH", 0)
	batch := []discord.Message{dmsg(10), dmsg(20)}

	// Sweep 1: classify, then "crash" before commit (do NOT call commit).
	toRelay, newCur := d.classify("CH", batch)
	if len(toRelay) != 2 {
		t.Fatalf("sweep1 toRelay = %v, want [10 20]", snowIDs(toRelay))
	}
	if c, _ := d.cursorOf("CH"); c != 0 {
		t.Fatalf("classify advanced cursor to %d before commit — would drop on crash", c)
	}

	// Restart: a fresh gate loads the persisted cursor (still 0 — commit never ran).
	d2 := newDedup(d.store, defaultSeenCap)
	if c, _ := d2.cursorOf("CH"); c != 0 {
		t.Fatalf("persisted cursor = %d after pre-commit crash, want 0 (re-fetch, not drop)", c)
	}
	// Sweep on restart re-fetches after=0 → re-delivers (duplicate, NOT a drop).
	toRelay2, newCur2 := d2.classify("CH", batch)
	if len(toRelay2) != 2 {
		t.Fatalf("restart re-delivery = %v, want [10 20] (at-least-once)", snowIDs(toRelay2))
	}
	// After a clean commit, the cursor advances and persists.
	if err := d2.commit("CH", newCur2); err != nil {
		t.Fatal(err)
	}
	_ = newCur
	d3 := newDedup(d.store, defaultSeenCap)
	if c, _ := d3.cursorOf("CH"); c != 20 {
		t.Fatalf("committed cursor = %d, want 20", c)
	}
}

func TestCommit_AdvancesPrunesAndPersists(t *testing.T) {
	d := newTestDedup(t)
	d.liveNew("CH", 10)
	d.liveNew("CH", 20)
	if err := d.commit("CH", 15); err != nil {
		t.Fatal(err)
	}
	if c, _ := d.cursorOf("CH"); c != 15 {
		t.Fatalf("cursor = %d, want 15", c)
	}
	// seen pruned of <=15: 10 gone, 20 kept → 20 not re-relayable live, 10 is (but 10<=cursor anyway).
	if d.liveNew("CH", 20) {
		t.Fatal("20 should still be seen (>cursor), not re-relayed")
	}
	if d.liveNew("CH", 10) {
		t.Fatal("10 is <= cursor, must not be re-relayed")
	}
	// commit is monotonic — a lower newCursor never regresses it.
	if err := d.commit("CH", 5); err != nil {
		t.Fatal(err)
	}
	if c, _ := d.cursorOf("CH"); c != 15 {
		t.Fatalf("cursor regressed to %d, want 15 (monotonic)", c)
	}
}

func TestLiveNew_BelowCursorNotRelayed(t *testing.T) {
	d := newTestDedup(t)
	d.initCursor("CH", 100)
	if d.liveNew("CH", 50) {
		t.Fatal("an id below the cursor must not be relayed (poller already covered it)")
	}
	if !d.liveNew("CH", 150) {
		t.Fatal("an id above the cursor is new")
	}
}

// TestSeenSizeCap is the F5 backstop: a channel whose poll never commits while live
// ids keep arriving must not grow the seen-set without bound.
func TestSeenSizeCap_BoundedWithoutCommit(t *testing.T) {
	d := newDedup(cursorStore{}, 8) // tiny cap
	for i := uint64(1); i <= 100; i++ {
		d.liveNew("CH", i)
	}
	d.mu.Lock()
	n := len(d.seen["CH"].ids)
	d.mu.Unlock()
	if n > 8 {
		t.Fatalf("seen size = %d, want <= cap 8 (F5 backstop)", n)
	}
}

func TestDedup_RaceLiveNewVsClassifyCommit(t *testing.T) {
	d := newTestDedup(t)
	d.initCursor("CH", 0)
	var wg sync.WaitGroup
	// Live path hammering liveNew.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(base uint64) {
			defer wg.Done()
			for i := uint64(0); i < 200; i++ {
				d.liveNew("CH", base*1000+i)
			}
		}(uint64(g + 1))
	}
	// Poller path: classify + commit in a loop (mirrors enqueue-then-commit, off-lock work omitted).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			batch := []discord.Message{dmsg(uint64(i*10 + 1)), dmsg(uint64(i*10 + 2))}
			_, nc := d.classify("CH", batch)
			_ = d.commit("CH", nc)
		}
	}()
	wg.Wait()
}
