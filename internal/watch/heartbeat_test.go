package watch

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// collector records enqueued jobs safely for assertions.
type collector struct {
	mu   sync.Mutex
	jobs []Job
}

func (c *collector) enqueue(j Job) {
	c.mu.Lock()
	c.jobs = append(c.jobs, j)
	c.mu.Unlock()
}

func (c *collector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.jobs)
}

func TestHeartbeatDisabled(t *testing.T) {
	c := &collector{}
	h := NewHeartbeat(0, "hydra-ops", "", c.enqueue, nil)
	h.Start()
	time.Sleep(40 * time.Millisecond)
	h.Stop()
	if c.count() != 0 {
		t.Errorf("disabled heartbeat fired %d times, want 0", c.count())
	}
}

func TestHeartbeatFiresWhenIdle(t *testing.T) {
	c := &collector{}
	h := NewHeartbeat(20*time.Millisecond, "hydra-ops", "", c.enqueue, func(string) bool { return false })
	h.Start()
	time.Sleep(120 * time.Millisecond)
	h.Stop()
	if c.count() == 0 {
		t.Error("idle heartbeat never fired")
	}
	if c.jobs[0].Agent != "hydra-ops" || c.jobs[0].Message != DefaultHeartbeatPrompt {
		t.Errorf("tick = %+v, want XO + default prompt", c.jobs[0])
	}
}

func TestHeartbeatSkippedWhenBusy(t *testing.T) {
	c := &collector{}
	var busy int32 = 1
	h := NewHeartbeat(20*time.Millisecond, "hydra-ops", "", c.enqueue,
		func(string) bool { return atomic.LoadInt32(&busy) == 1 })
	h.Start()
	time.Sleep(120 * time.Millisecond)
	h.Stop()
	if c.count() != 0 {
		t.Errorf("busy heartbeat fired %d times, want 0 (idle-gate)", c.count())
	}
}

func TestHeartbeatResetSuppressesTick(t *testing.T) {
	c := &collector{}
	h := NewHeartbeat(80*time.Millisecond, "hydra-ops", "", c.enqueue, func(string) bool { return false })
	h.Start()
	// Reset every 20ms for 120ms — the 80ms timer should never elapse.
	for i := 0; i < 6; i++ {
		time.Sleep(20 * time.Millisecond)
		h.Reset()
	}
	got := c.count()
	h.Stop()
	if got != 0 {
		t.Errorf("heartbeat fired %d times despite continuous resets, want 0", got)
	}
}
