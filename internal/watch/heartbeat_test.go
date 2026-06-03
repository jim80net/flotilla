package watch

import (
	"strconv"
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
	h.Stop() // stop first (drains any in-flight tick) so the count is deterministic
	if got := c.count(); got != 0 {
		t.Errorf("heartbeat fired %d times despite continuous resets, want 0", got)
	}
}

func TestHeartbeatActivitySuppressesTick(t *testing.T) {
	c := &collector{}
	var n int64
	h := NewHeartbeat(80*time.Millisecond, "hydra-ops", "", c.enqueue, func(string) bool { return false })
	h.SetPollInterval(10 * time.Millisecond)
	// Fingerprint changes on every sample → the XO looks continuously active →
	// the idle clock keeps resetting → the 80ms timer never elapses.
	h.SetActivityProbe(func() string { return strconv.FormatInt(atomic.AddInt64(&n, 1), 10) })
	h.Start()
	time.Sleep(160 * time.Millisecond)
	h.Stop()
	if got := c.count(); got != 0 {
		t.Errorf("heartbeat fired %d times despite continuous pane activity, want 0", got)
	}
}

func TestHeartbeatActivityStableFires(t *testing.T) {
	c := &collector{}
	h := NewHeartbeat(40*time.Millisecond, "hydra-ops", "", c.enqueue, func(string) bool { return false })
	h.SetPollInterval(10 * time.Millisecond)
	// A stable fingerprint (XO idle, pane unchanged) must NOT block the tick.
	h.SetActivityProbe(func() string { return "stable-pane" })
	h.Start()
	time.Sleep(140 * time.Millisecond)
	h.Stop()
	if c.count() == 0 {
		t.Error("heartbeat never fired despite a stable (idle) pane")
	}
}

func TestDerivePollInterval(t *testing.T) {
	cases := []struct{ interval, want time.Duration }{
		{20 * time.Minute, 30 * time.Second},           // cap
		{40 * time.Second, 10 * time.Second},           // interval/4
		{2 * time.Second, time.Second},                 // 1s floor, still <= interval/2
		{1 * time.Second, 500 * time.Millisecond},      // interval/2 guard beats the floor
		{40 * time.Millisecond, 20 * time.Millisecond}, // interval/2 guard (sub-second)
	}
	for _, tc := range cases {
		got := derivePollInterval(tc.interval)
		if got != tc.want {
			t.Errorf("derivePollInterval(%v) = %v, want %v", tc.interval, got, tc.want)
		}
		// invariant (cubic P2): the probe must be sampled before the timer fires.
		if got >= tc.interval {
			t.Errorf("derivePollInterval(%v) = %v >= interval — would fire before sampling activity", tc.interval, got)
		}
	}
}
