package watch

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPaneMutexesSerializeSameAgent(t *testing.T) {
	// Two writers to the SAME agent's pane (a confirmed delivery and a rotate) must serialize
	// — the invariant that keeps a `/clear` from interleaving mid-delivery.
	p := NewPaneMutexes()
	var inFlight, overlap int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := p.Lock("hydra-ops")
			defer unlock()
			if atomic.AddInt32(&inFlight, 1) != 1 {
				atomic.StoreInt32(&overlap, 1)
			}
			time.Sleep(time.Millisecond) // widen the window for an overlap to show
			atomic.AddInt32(&inFlight, -1)
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&overlap) != 0 {
		t.Error("two writers to the same pane overlapped — paneMutexes did not serialize (a /clear could interleave a confirmed delivery)")
	}
}

func TestPaneMutexesDistinctAgentsAreConcurrent(t *testing.T) {
	// A lock on one pane must NOT block a writer to a DIFFERENT pane — delivery to other desks
	// proceeds while one pane is held across a confirmed delivery.
	p := NewPaneMutexes()
	unlockA := p.Lock("desk-a")
	defer unlockA()
	done := make(chan struct{})
	go func() {
		unlockB := p.Lock("desk-b") // must NOT block on desk-a's held lock
		unlockB()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("a lock on desk-a blocked a writer to desk-b — distinct panes must be independent")
	}
}
