package watch

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// shellDebounce is the number of CONSECUTIVE shell assessments required before a
// desk is treated as crashed (systems-review M2). Even though claude-code Assess
// now maps a transient pane-command read-error to StateUnknown (not StateShell),
// a desk genuinely mid-restart can momentarily present a real bare shell;
// requiring two consecutive StateShell reads suppresses that false alarm.
const shellDebounce = 2

// snapshotWriteFailThreshold is how many consecutive snapshot-write failures
// trip the LOUD persistence alert and the degrade-to-in-memory mode (H3).
const snapshotWriteFailThreshold = 3

// WakeKind labels why the detector is waking the XO, so the caller can compose
// the right prompt (the detector owns WHEN/WHY to wake; the caller owns the
// prompt text, which references deployment-specific file paths).
type WakeKind int

const (
	// WakeMaterial: an external desk transition or a tracker change (or the
	// cold-start reassess) — the reasons name what changed.
	WakeMaterial WakeKind = iota
	// WakeContinuation: the XO finished a turn and may have a next authorized
	// step; the prompt carries the narrow-answer discipline (advance or reply
	// idle, never manufacture work).
	WakeContinuation
	// WakePing: a liveness-only ping (ack and do nothing else) — the max-quiet
	// safety net so a healthy idle XO keeps acking.
	WakePing
)

// DetectorConfig wires a Detector. The collaborators are injected so the whole
// state machine is unit-testable without tmux, a clock, or the filesystem.
type DetectorConfig struct {
	XOAgent  string        // the XO's roster name (its own transitions feed self-continuation only)
	Desks    []string      // monitored agent names, INCLUDING the XO
	Interval time.Duration // detector tick cadence (drives the liveness windows)

	// Assess resolves a desk's current surface state (resolve pane + Driver.Assess);
	// an unresolvable pane SHOULD return StateUnknown (anti-flap, caught by ack age).
	Assess func(agent string) surface.State
	// SignalHash returns the OPTIONAL external signal file's content hash; ok=false
	// when no signal file is configured or it is absent/unreadable (treated as
	// unchanged — no wake-storm). This is NOT the XO's own state tracker: hashing the
	// tracker would self-wake the XO on its own writes (the heartbeat instructs the
	// XO to keep .flotilla-state.md current). External wake deltas (a desk/tool
	// dropping a signal) flow through here; unconfigured ⇒ inert (always ok=false).
	SignalHash func() (string, bool)
	// AckAge returns the wall-clock age of the XO's last liveness ack.
	AckAge func() time.Duration
	// Wake enqueues an XO wake of the given kind with human-readable reasons; the
	// caller composes the prompt (and appends the ack instruction — L1).
	Wake func(kind WakeKind, reasons []string)
	// Rotate rotates the XO context via surface.RotateContext (claude → /clear).
	Rotate func() error
	// Awaiting reports whether the awaiting-operator veto marker is present (gates
	// the rotate only).
	Awaiting func() bool
	// SettleConsume reports+consumes the XO's settle marker (the fast idle signal).
	SettleConsume func() bool
	// Alert raises a LOUD operator alert (down-alert path) — liveness + the H3
	// persistence failure.
	Alert func(string)
	// Persist writes the snapshot durably (atomic). Injected for tests; production
	// wires Snapshot.Save.
	Persist func(Snapshot) error
	// Now is the clock (tests pin it); defaults to time.Now.
	Now func() time.Time

	MaxMissedAcks       int    // K — the wedge-alert window base, in intervals
	MaxQuietIntervals   int    // N override — ping cadence; 0 ⇒ mode default
	LivenessPingMode    string // "none" (default, $0-idle) | "interval" | "consecutive"
	MaxSelfContinuation int    // H1 hard cap on consecutive no-external-change continuations
}

// Detector is the v2 heartbeat: a deterministic, no-LLM tick that wakes the XO
// ONLY on a material change, self-continues it without a blind timer, rotates
// its context between handlings, and detects liveness on wall-clock ack age. All
// mutable state is guarded by mu so the per-interval Tick and the operator-wake
// path (relay goroutine) are race-free single-writers (systems-review M3).
type Detector struct {
	mu sync.Mutex

	cfg           DetectorConfig
	pingEvery     int // mode-derived ping cadence (intervals); 0 disables pings
	alertInterval int // mode-derived wedge-alert window (intervals)

	snap        Snapshot
	cold        bool
	quietTicks  int
	selfCont    int
	shellStreak map[string]int
	writeFails  int
	degraded    bool
	wd          *Watchdog

	stop      chan struct{}
	done      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewDetector builds a detector from config, loading any persisted snapshot (a
// missing/corrupt one cold-starts → one conservative wake on the first tick).
func NewDetector(cfg DetectorConfig, snapPath string) *Detector {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Persist == nil {
		cfg.Persist = func(s Snapshot) error { return s.Save(snapPath) }
	}
	if cfg.SignalHash == nil {
		// No external signal file configured ⇒ inert (never a wake from this source).
		cfg.SignalHash = func() (string, bool) { return "", false }
	}
	if cfg.MaxSelfContinuation < 1 {
		cfg.MaxSelfContinuation = 3
	}
	ping, alert := livenessParams(cfg.LivenessPingMode, cfg.MaxMissedAcks, cfg.MaxQuietIntervals)

	snap, ok := LoadSnapshot(snapPath)
	d := &Detector{
		cfg:           cfg,
		pingEvery:     ping,
		alertInterval: alert,
		snap:          snap,
		cold:          !ok,
		shellStreak:   map[string]int{},
		// The detector computes the staleness threshold itself (age > alertInterval×
		// interval), so the watchdog only needs to trip on the first stale/crash
		// signal and debounce — maxMissed=1.
		wd:   NewWatchdog(1, cfg.Alert),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	return d
}

// livenessParams resolves the ping cadence and the wedge-alert window (both in
// intervals) from the ping mode, K, and an optional N override. The invariant it
// preserves: the safety ping always fires at least one interval BEFORE the alert
// window, so a healthy idle XO re-acks before it is ever declared wedged. The
// three modes are the C1b tradeoff made switchable WITHOUT a rebuild:
//   - "interval": ping every K-1 intervals, alert at K — the strict window, at
//     the cost of a cheap ack-ping each idle interval.
//   - "consecutive": ping every K-1, alert after ~2 missed pings (K-1+2) — the
//     middle ground.
//   - "none" (default, the XO's option (ii)): NO per-interval ping; only a WIDE
//     safety ping at 2K, alert at 2K+1 — true $0-idle, accepting a ~2K idle-fleet
//     wedge window (a crash is still immediate; a wedged XO on an idle fleet has
//     nothing to miss).
func livenessParams(mode string, k, nOverride int) (pingEvery, alertIntervals int) {
	if k < 1 {
		k = 1
	}
	maxOf := func(a, b int) int {
		if a > b {
			return a
		}
		return b
	}
	// slack is how far the alert window sits beyond the ping cadence; the alert is
	// always at least one interval past the ping so a healthy idle XO re-acks
	// before it is ever declared wedged (even in the degenerate K=1 case).
	slack := 1
	switch mode {
	case "interval":
		pingEvery = maxOf(1, k-1)
	case "consecutive":
		pingEvery = maxOf(1, k-1)
		slack = 2
	default: // "none" / "" — the XO default: true $0-idle, wide safety ping
		pingEvery = 2 * k
	}
	if nOverride > 0 {
		pingEvery = nOverride
	}
	alertIntervals = maxOf(k, pingEvery+slack)
	return pingEvery, alertIntervals
}

// Start launches the detector loop (ticks every Interval). interval <= 0 parks
// it until Stop (disabled).
func (d *Detector) Start() { d.startOnce.Do(func() { go d.loop() }) }

// Stop ends the loop and waits for it to exit. Idempotent.
func (d *Detector) Stop() {
	d.stopOnce.Do(func() { close(d.stop) })
	<-d.done
}

func (d *Detector) loop() {
	defer close(d.done)
	if d.cfg.Interval <= 0 {
		<-d.stop
		return
	}
	t := time.NewTicker(d.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-t.C:
			d.Tick()
		}
	}
}

// OperatorWake is called by the relay when an operator message is delivered to
// the XO: it clears the settled state and resets the self-continuation and quiet
// counters so a settled XO re-engages and the cap restarts (fork B #2 / H1). It
// holds the same mutex as Tick so the two never race (M3).
func (d *Detector) OperatorWake() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.snap.XOSettled = false
	d.selfCont = 0
	d.quietTicks = 0
	// Drop any settle signal the XO may have just dropped, so it cannot re-settle
	// the XO on the next tick after the operator has re-engaged it.
	if d.cfg.SettleConsume != nil {
		_ = d.cfg.SettleConsume()
	}
}

// Tick runs one detector cycle: snapshot → liveness → diff → wake-or-sleep →
// persist. It is the single per-interval writer; OperatorWake is the only other
// writer and shares the mutex.
func (d *Detector) Tick() {
	d.mu.Lock()
	defer d.mu.Unlock()

	woke := false
	wake := func(kind WakeKind, reasons []string) {
		woke = true
		if d.cfg.Wake != nil {
			d.cfg.Wake(kind, reasons)
		}
	}

	// 1. Gather current signals. Tracker absent/unreadable carries the prior hash
	//    forward (treat-unchanged — M4); states are shell-debounced (M2).
	cur := Snapshot{
		DeskStates: make(map[string]surface.State, len(d.cfg.Desks)),
		SignalHash: d.snap.SignalHash,
		XOSettled:  d.snap.XOSettled,
	}
	for _, name := range d.cfg.Desks {
		cur.DeskStates[name] = d.debounce(name, d.cfg.Assess(name))
	}
	if h, ok := d.cfg.SignalHash(); ok {
		cur.SignalHash = h
	}

	// 2. Cold start (missing/corrupt snapshot, or first boot): seed the baseline
	//    WITHOUT emitting per-desk transitions (L3), but wake ONCE conservatively
	//    so a change that happened while the detector was down is not missed.
	if d.cold {
		d.cold = false
		d.snap = cur
		d.evalLiveness(cur) // still cover liveness from tick one
		wake(WakeMaterial, []string{"change-detector started — reassess the fleet"})
		d.quietTicks = 0
		d.save()
		return
	}

	prev := d.snap

	// 3. Liveness — independent of the diff (H3): crash (shell-debounced) +
	//    wall-clock ack age. Kept in-memory + ack-file so a snapshot outage can
	//    never blind the watchdog.
	d.evalLiveness(cur)

	// 4. External material change (every desk EXCEPT the XO — H2). It re-engages a
	//    settled XO and resets the self-continuation cap.
	if ext, reasons := externalMaterial(prev, cur, d.cfg.XOAgent); ext {
		d.selfCont = 0
		cur.XOSettled = false
		wake(WakeMaterial, reasons)
	} else if xoFinishedTurn(prev, cur, d.cfg.XOAgent) && !cur.XOSettled {
		// 5. XO self-continuation — only when nothing external fired this tick (an
		//    external change already covers advancing the XO and resets the cap).
		d.continueXO(&cur, wake)
	}

	// 6. Max-quiet liveness ping (layer 3). Any wake above already refreshes
	//    liveness (L1), so only an entirely-quiet tick advances the quiet counter.
	if woke {
		d.quietTicks = 0
	} else {
		d.quietTicks++
		if d.pingEvery > 0 && d.quietTicks >= d.pingEvery {
			wake(WakePing, nil)
			d.quietTicks = 0
		}
	}

	// 7. Persist the new baseline (fail-safe — H3).
	d.snap = cur
	d.save()
}

// continueXO handles the XO's own Working→Idle: rotate to fresh context (unless
// awaiting an operator reply), then either settle (the XO signalled idle, or the
// hard cap is hit) or wake once more to advance the next authorized step.
func (d *Detector) continueXO(cur *Snapshot, wake func(WakeKind, []string)) {
	// Rotate between steps so each handling runs in fresh context — gated by the
	// awaiting-operator veto (never wipe an outstanding question thread).
	if d.cfg.Awaiting == nil || !d.cfg.Awaiting() {
		if d.cfg.Rotate != nil {
			if err := d.cfg.Rotate(); err != nil && !errors.Is(err, surface.ErrRestartRequired) {
				log.Printf("flotilla watch: XO context rotate failed: %v (continuing without rotate)", err)
			}
		}
	}

	// Fast settle: the XO signalled idle.
	if d.cfg.SettleConsume != nil && d.cfg.SettleConsume() {
		cur.XOSettled = true
		return
	}

	// Otherwise the XO claims a next step. Bound a runaway that never signals idle
	// and produces no external change (H1) — rotation erases its memory of looping,
	// so the cap is a code-level backstop, not a prompt promise.
	d.selfCont++
	if d.selfCont > d.cfg.MaxSelfContinuation {
		cur.XOSettled = true
		log.Printf("flotilla watch: XO self-continuation hit the cap (%d) with no external change — forcing settled", d.cfg.MaxSelfContinuation)
		return
	}
	wake(WakeContinuation, nil)
}

// debounce returns a desk's effective state, suppressing a single transient
// shell read: a shell is only believed after shellDebounce consecutive reads;
// before that the prior known state is held so a blip is never a crash (M2).
func (d *Detector) debounce(name string, raw surface.State) surface.State {
	if raw != surface.StateShell {
		d.shellStreak[name] = 0
		return raw
	}
	d.shellStreak[name]++
	if d.shellStreak[name] >= shellDebounce {
		return surface.StateShell
	}
	if prev, ok := d.snap.DeskStates[name]; ok {
		return prev // hold the last known state through the blip
	}
	return surface.StateUnknown
}

// evalLiveness drives the watchdog from the two cadence-independent signals
// (C1): a shell-debounced crash (immediate) and a wall-clock ack age over the
// mode-derived window while the XO is not a shell. The watchdog (maxMissed=1)
// debounces the alert and clears it on recovery.
func (d *Detector) evalLiveness(cur Snapshot) {
	shellStreak := d.shellStreak[d.cfg.XOAgent]
	crashed := shellStreak >= shellDebounce
	switch {
	case crashed:
		d.wd.Observe(false, true)
	case shellStreak == 0 && d.cfg.AckAge() > time.Duration(d.alertInterval)*d.cfg.Interval:
		// Wedged: alive (no shell suspicion) but not acking within the window. The
		// `shellStreak == 0` guard means a tick where a shell is suspected but not
		// yet confirmed does NOT fire the "wedged" message — the next tick confirms
		// the crash and the (debounced) alert carries the accurate crash wording.
		d.wd.Observe(false, false)
	default:
		d.wd.Observe(true, false) // healthy (or shell pending) → clear any down state
	}
}

// save persists the snapshot fail-safe (H3). The in-memory snapshot is the
// source of truth for diffs, so a write failure never causes wake-every-tick;
// after a run of failures it raises a loud alert and degrades to in-memory-only
// (failing toward not-spending). A later restart cold-starts (one wake), never
// a per-tick storm.
func (d *Detector) save() {
	if d.degraded {
		return
	}
	if err := d.cfg.Persist(d.snap); err != nil {
		d.writeFails++
		log.Printf("flotilla watch: snapshot persist failed (%d/%d): %v", d.writeFails, snapshotWriteFailThreshold, err)
		if d.writeFails >= snapshotWriteFailThreshold && d.cfg.Alert != nil {
			d.degraded = true
			d.cfg.Alert("change-detector snapshot persistence is failing — continuing in-memory only; detector state will not survive a restart, but waking is unaffected")
		}
		return
	}
	d.writeFails = 0
}

// xoFinishedTurn reports the XO's own Working→Idle transition (its self-
// continuation trigger). Kept separate from externalMaterial, which excludes the
// XO (H2).
func xoFinishedTurn(prev, cur Snapshot, xo string) bool {
	return prev.DeskStates[xo] == surface.StateWorking && cur.DeskStates[xo] == surface.StateIdle
}
