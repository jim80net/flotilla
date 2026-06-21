package claudestore

import (
	"testing"
	"time"
)

// waitQuiescent must BLOCK until the session file's size holds stable across a beat, so a turn-final
// still being flushed to disk is never read (and posted) mid-write — the silent-truncation class the
// XO hook's BUG-3 loop guards against.
func TestWaitQuiescentWaitsUntilSizeStable(t *testing.T) {
	origSleep, origStat := sleep, statSize
	defer func() { sleep, statSize = origSleep, origStat }()
	sleep = func(time.Duration) {} // no real wait in the test

	// 100 → 200 → 300 → 300 (the write settles on the 3rd beat). Count the beats so we PROVE a
	// stable (cur==prev) read was actually observed, not merely that reads were consumed: the
	// initial read is 100, then three beats (200, 300, 300) — the third is the first cur==prev.
	sizes := []int64{100, 200, 300, 300, 300}
	i := 0
	statSize = func(string) int64 {
		v := sizes[i]
		if i < len(sizes)-1 {
			i++
		}
		return v
	}
	beats := 0
	sleep = func(time.Duration) { beats++ }
	waitQuiescent("ignored")
	if beats != 3 {
		t.Fatalf("waitQuiescent should wait exactly 3 beats until the size stabilizes (100→200→300→300), got %d", beats)
	}
}

// An already-stable file settles in a single beat without spinning to the bound.
func TestWaitQuiescentStableFileReturnsFast(t *testing.T) {
	origSleep, origStat := sleep, statSize
	defer func() { sleep, statSize = origSleep, origStat }()
	beats := 0
	sleep = func(time.Duration) { beats++ }
	statSize = func(string) int64 { return 4242 } // never changes

	waitQuiescent("ignored")
	if beats != 1 {
		t.Fatalf("a stable file should settle in one beat, got %d", beats)
	}
}
