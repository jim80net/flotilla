package watch

import "time"

// DefaultHeartbeatPrompt is the idempotent self-continuation tick (design D6).
// It turns a turn-based XO into a self-continuing system: advance clear,
// already-authorized laid-out work without waiting for the operator; otherwise
// idle. It must never drive the XO to manufacture unauthorized work.
const DefaultHeartbeatPrompt = "This is an automated heartbeat, not a new instruction. " +
	"Emit your one-line liveness ack. If there is clear, already-authorized work in flight " +
	"— an open task in the active openspec change, an unanswered desk report, an approved " +
	"plan step — advance it now without waiting for the operator. If nothing is laid out, " +
	"reply 'idle' and do nothing. Never manufacture work the operator did not authorize."

// Heartbeat injects the self-continuation tick into the XO pane after an
// inactivity interval, UNLESS the XO appears busy (idle-gate). The timer resets
// on every real delivery (an operator message is itself a tick), so the
// synthetic tick fires only after a true inactivity gap.
type Heartbeat struct {
	interval time.Duration
	xoAgent  string
	prompt   string
	enqueue  func(Job)
	busy     func(agent string) bool // idle-gate: true when the pane is mid-turn
	gate     func() bool             // per-interval hook; true → skip this tick (e.g. XO is down)

	reset chan struct{}
	stop  chan struct{}
	done  chan struct{}
}

// NewHeartbeat builds a heartbeat. interval <= 0 disables it (no ticks ever).
// An empty prompt uses DefaultHeartbeatPrompt. busy may be nil (treated as
// never-busy).
func NewHeartbeat(interval time.Duration, xoAgent, prompt string, enqueue func(Job), busy func(string) bool) *Heartbeat {
	if prompt == "" {
		prompt = DefaultHeartbeatPrompt
	}
	if busy == nil {
		busy = func(string) bool { return false }
	}
	return &Heartbeat{
		interval: interval,
		xoAgent:  xoAgent,
		prompt:   prompt,
		enqueue:  enqueue,
		busy:     busy,
		reset:    make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// SetGate installs a per-interval hook run at the start of every tick cycle
// (before the idle-gate). When it returns true the tick is skipped this cycle.
// The watchdog uses it to observe liveness every interval and suppress the tick
// while the XO is down (don't wind a dead clock). Must be set before Start.
func (h *Heartbeat) SetGate(gate func() bool) { h.gate = gate }

// Reset restarts the inactivity timer. Call it on every real delivery so a
// stream of operator activity suppresses the synthetic tick. Non-blocking.
func (h *Heartbeat) Reset() {
	select {
	case h.reset <- struct{}{}:
	default:
	}
}

// Start launches the heartbeat loop.
func (h *Heartbeat) Start() { go h.loop() }

// Stop ends the loop and waits for it to exit.
func (h *Heartbeat) Stop() {
	close(h.stop)
	<-h.done
}

func (h *Heartbeat) loop() {
	defer close(h.done)
	if h.interval <= 0 {
		<-h.stop // disabled: park until stopped
		return
	}
	t := time.NewTimer(h.interval)
	defer t.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-h.reset:
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(h.interval)
		case <-t.C:
			// (A coincident pending Reset is benign: select may pick this tick,
			// then the buffered reset is consumed next iteration and re-arms the
			// timer to the same interval — no double-tick, no drift.)
			// gate runs every interval (the watchdog observes liveness here);
			// when it reports the XO down, skip the tick — don't wind a dead clock.
			gated := h.gate != nil && h.gate()
			if !gated && !h.busy(h.xoAgent) {
				h.enqueue(Job{Agent: h.xoAgent, Message: h.prompt})
			}
			t.Reset(h.interval)
		}
	}
}
