package watch

import (
	"sync"
	"time"
)

// DefaultHeartbeatPrompt is the idempotent self-continuation tick (design D6).
// It turns a turn-based XO into a self-continuing system. On each tick the XO
// does TWO duties rather than answering from memory: (A) check in on its
// monitored desks (the roster's other agents) and surface/advance anything
// actionable, and (B) advance its own already-authorized work, reading sources
// in order. A task — or a desk — blocked only from landing (a push gate, a
// pending review) is explicitly NOT idle. It names a convention:
// `.flotilla-state.md` is the top-level goal + task tracker, with the active
// openspec change as per-change detail (pluggable later — see #6). It must never
// drive the XO to manufacture unauthorized work. Deployments can override the
// wording per-roster via heartbeat_message (e.g. to name absolute source paths).
const DefaultHeartbeatPrompt = "This is an automated heartbeat, not a new instruction. " +
	"Emit a one-line liveness ack. Then do two duties, neither from memory. DUTY A — check in on " +
	"your monitored desks: for each agent in the roster other than yourself, capture its tmux pane " +
	"and assess its state (working / idle-awaiting-operator-decision / blocked / errored / finished " +
	"a task / low-context needing rotation); surface anything actionable in one line and advance " +
	"authorized coordination (relay a pending operator decision, rotate a low-context desk, collect " +
	"a finished desk's PR). A desk needing attention is NOT idle. DUTY B — advance your own work: " +
	"read your sources in order — (1) the top-level goal + task tracker `.flotilla-state.md` if " +
	"present; (2) the active openspec change's unchecked tasks if openspec is installed and a change " +
	"is active (run `openspec list`, then read its tasks.md); (3) the project roadmap / README — " +
	"then advance the next clear, already-authorized step without waiting for the operator and keep " +
	"`.flotilla-state.md` current. A task blocked only from landing (a push gate, a pending review) " +
	"is NOT idle — advance it locally, then surface the blocker in one line. Only if BOTH the desks " +
	"and your own sources have nothing actionable, reply 'idle' and do nothing. Never manufacture " +
	"work the operator did not authorize."

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

	startOnce sync.Once
	stopOnce  sync.Once
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
func (h *Heartbeat) Start() { h.startOnce.Do(func() { go h.loop() }) }

// Stop ends the loop and waits for it to exit. Idempotent (safe to call twice).
func (h *Heartbeat) Stop() {
	h.stopOnce.Do(func() { close(h.stop) })
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
				h.enqueue(Job{Agent: h.xoAgent, Message: h.prompt, Kind: "heartbeat"})
			}
			t.Reset(h.interval)
		}
	}
}
