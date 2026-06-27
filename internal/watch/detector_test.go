package watch

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/surface"
)

// detFixture is an injectable test rig for the Detector: it backs every
// collaborator with plain fields so the state machine is exercised without tmux,
// a real clock, or the filesystem.
type detFixture struct {
	mu          sync.Mutex
	states      map[string]surface.State
	signal      string
	signalOK    bool
	ackAge      time.Duration
	awaiting    bool
	settle      bool // settle marker present (consumed when read)
	wakes       []wakeRec
	alerts      []string
	persistErr  error
	persisted   []Snapshot
	rotateCalls int
	rotateErr   error
	backlog     backlog.Status // the goal-driven loop gate; zero ⇒ inert (no unblocked items)
}

type wakeRec struct {
	kind    WakeKind
	reasons []string
}

func newFixture() *detFixture {
	return &detFixture{states: map[string]surface.State{}, signalOK: true}
}

func (f *detFixture) set(agent string, s surface.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[agent] = s
}

func (f *detFixture) config(xo string, desks []string, k int, mode string) DetectorConfig {
	return DetectorConfig{
		XOAgent:          xo,
		Desks:            desks,
		Interval:         time.Minute,
		MaxMissedAcks:    k,
		LivenessPingMode: mode,
		Assess: func(a string) surface.State {
			f.mu.Lock()
			defer f.mu.Unlock()
			if s, ok := f.states[a]; ok {
				return s
			}
			return surface.StateUnknown
		},
		SignalHash: func() (string, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.signal, f.signalOK
		},
		AckAge: func() time.Duration { f.mu.Lock(); defer f.mu.Unlock(); return f.ackAge },
		Wake: func(kind WakeKind, reasons []string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.wakes = append(f.wakes, wakeRec{kind, reasons})
		},
		Rotate:   func() error { f.mu.Lock(); defer f.mu.Unlock(); f.rotateCalls++; return f.rotateErr },
		Awaiting: func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.awaiting },
		SettleConsume: func() bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			was := f.settle
			f.settle = false
			return was
		},
		Alert: func(m string) { f.mu.Lock(); defer f.mu.Unlock(); f.alerts = append(f.alerts, m) },
		BacklogGate: func() backlog.Status {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.backlog
		},
		Persist: func(s Snapshot) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.persisted = append(f.persisted, s)
			return f.persistErr
		},
	}
}

func (f *detFixture) wakeCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.wakes) }
func (f *detFixture) lastWake() wakeRec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wakes[len(f.wakes)-1]
}
func (f *detFixture) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wakes = nil
	f.alerts = nil
	f.rotateCalls = 0
}

// newDet builds a detector with a missing snapshot path (cold). For tests that
// want to skip the cold tick, call seed() to install a baseline directly.
func newDet(t *testing.T, f *detFixture, cfg DetectorConfig) *Detector {
	t.Helper()
	return NewDetector(cfg, filepath.Join(t.TempDir(), "missing-snapshot.json"))
}

// seed installs a baseline snapshot and clears the cold flag, so the next Tick
// diffs against the given states rather than cold-starting.
func seed(d *Detector, states map[string]surface.State, signal string) {
	cp := map[string]surface.State{}
	for k, v := range states {
		cp[k] = v
	}
	d.snap = Snapshot{DeskStates: cp, SignalHash: signal}
	d.cold = false
}

func TestDetectorColdStartWakesOnceThenQuiet(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateWorking)
	f.signal = "h0"

	d.Tick() // cold
	if f.wakeCount() != 1 || f.lastWake().kind != WakeMaterial {
		t.Fatalf("cold start should wake once (material reassess), got %+v", f.wakes)
	}
	// Same states next tick → no spurious transition (L3 seed-without-emitting).
	f.reset()
	d.Tick()
	if f.wakeCount() != 0 {
		t.Errorf("steady fleet after cold start should be silent, got %+v", f.wakes)
	}
}

func TestDetectorDeskFinishedWakesTargeted(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle) // desk finished a turn
	f.signal = "h0"

	d.Tick()
	if f.wakeCount() != 1 || f.lastWake().kind != WakeMaterial {
		t.Fatalf("desk finish should wake material, got %+v", f.wakes)
	}
	if len(f.lastWake().reasons) != 1 || f.lastWake().reasons[0][:7] != "backend" {
		t.Errorf("wake reason should name the desk, got %v", f.lastWake().reasons)
	}
}

func TestDetectorIdleFleetIsSilent(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.signal = "h0"
	d.Tick()
	if f.wakeCount() != 0 {
		t.Errorf("idle fleet must cost zero wakes, got %+v", f.wakes)
	}
}

func TestDetectorShellDebounced(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateShell) // first shell read — a blip
	f.signal = "h0"

	d.Tick()
	if f.wakeCount() != 0 {
		t.Errorf("a single shell read must be debounced (no crash wake), got %+v", f.wakes)
	}
	// Second consecutive shell → confirmed crash transition → material wake.
	f.reset()
	d.Tick()
	if f.wakeCount() != 1 || f.lastWake().reasons[0][:7] != "backend" {
		t.Errorf("two consecutive shells should wake (confirmed crash), got %+v", f.wakes)
	}
}

func TestDetectorXOSelfContinuationOnceNotDeskFinished(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle) // XO finished a turn
	f.set("backend", surface.StateIdle)
	f.signal = "h0"

	d.Tick()
	if f.wakeCount() != 1 {
		t.Fatalf("XO finish should produce exactly one wake, got %+v", f.wakes)
	}
	if f.lastWake().kind != WakeContinuation {
		t.Errorf("XO Working→Idle must be a continuation wake (H2: not desk-finished), got %v", f.lastWake().kind)
	}
	if f.rotateCalls != 1 {
		t.Errorf("XO settle should rotate context, got %d rotate calls", f.rotateCalls)
	}
}

func TestDetectorSettleMarkerSleepsUntilExternalChange(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.signal = "h0"
	f.settle = true // the XO replied idle

	d.Tick()
	if f.wakeCount() != 0 {
		t.Fatalf("an idle-signalled XO must not be woken, got %+v", f.wakes)
	}
	// Further XO finishes do not re-wake while settled.
	f.reset()
	f.set("xo", surface.StateWorking)
	d.Tick()
	f.set("xo", surface.StateIdle)
	d.Tick()
	if f.wakeCount() != 0 {
		t.Fatalf("settled XO must stay asleep for self-continuation, got %+v", f.wakes)
	}
	// An external change (desk finishes) re-engages it.
	f.reset()
	f.set("backend", surface.StateWorking)
	d.Tick()
	f.set("backend", surface.StateIdle)
	d.Tick()
	if f.wakeCount() == 0 {
		t.Error("an external material change should re-engage a settled XO")
	}
}

func TestDetectorSelfContinuationCap(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	cfg.MaxSelfContinuation = 2
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.signal = "h0"

	// Drive repeated XO Working→Idle with no external change and no settle marker.
	contWakes := 0
	for i := 0; i < 5; i++ {
		f.set("xo", surface.StateWorking)
		d.Tick()
		f.set("xo", surface.StateIdle)
		d.Tick()
	}
	f.mu.Lock()
	for _, w := range f.wakes {
		if w.kind == WakeContinuation {
			contWakes++
		}
	}
	f.mu.Unlock()
	if contWakes != 2 {
		t.Errorf("continuation wakes = %d, want 2 (capped at MaxSelfContinuation)", contWakes)
	}
}

func TestDetectorOperatorWakeClearsSettled(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.signal = "h0"
	f.settle = true
	d.Tick() // settles
	if !d.snap.XOSettled {
		t.Fatal("XO should be settled")
	}

	d.OperatorWake() // operator message clears settled
	if d.snap.XOSettled {
		t.Error("OperatorWake must clear the settled flag")
	}
	// Now an XO finish self-continues again.
	f.reset()
	f.set("xo", surface.StateWorking)
	d.Tick()
	f.set("xo", surface.StateIdle)
	d.Tick()
	if f.wakeCount() == 0 {
		t.Error("after OperatorWake the XO should self-continue again")
	}
}

func TestDetectorRotateSkippedWhileAwaiting(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")
	f.set("xo", surface.StateIdle)
	f.signal = "h0"
	f.awaiting = true // outstanding operator question

	d.Tick()
	if f.rotateCalls != 0 {
		t.Errorf("rotate must be skipped while awaiting the operator, got %d calls", f.rotateCalls)
	}
}

// The post-unlock tail (runTail) MUST perform the /clear rotate BEFORE it enqueues the
// continuation wake — otherwise a trailing /clear would wipe the just-delivered continuation.
// The in-line under-mutex call used to give this order for free; with the rotate moved to the
// tail (so its cross-process txn-lock wait is off d.mu) the order is now straight-line code, so
// lock it with a test: a Working→Idle XO tick must record "rotate" strictly before "wake".
func TestDetectorTailRotatesBeforeContinuationWake(t *testing.T) {
	var mu sync.Mutex
	var events []string
	cfg := DetectorConfig{
		XOAgent:  "xo",
		Desks:    []string{"xo"},
		Interval: time.Minute,
		Assess:   func(string) surface.State { return surface.StateIdle },
		AckAge:   func() time.Duration { return 0 },
		Rotate:   func() error { mu.Lock(); defer mu.Unlock(); events = append(events, "rotate"); return nil },
		Wake:     func(WakeKind, []string) { mu.Lock(); defer mu.Unlock(); events = append(events, "wake") },
		Persist:  func(Snapshot) error { return nil },
	}
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")

	d.Tick() // XO Working→Idle (seeded Working, now assesses Idle) → continueXO → rotate + continuation

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "rotate" || events[1] != "wake" {
		t.Fatalf("tail must rotate THEN wake (so a /clear never wipes the continuation), got %v", events)
	}
}

// At-least-once crash semantics (cubic P1): the DURABLE snapshot persist MUST happen AFTER the
// tail enqueues the wakes — otherwise a crash in the save→tail window persists "transition
// processed" while the wake is lost, and the restart (loading a non-cold snapshot) never re-wakes,
// stalling the XO. Lock the ordering: a Working→Idle continuation tick must record persist LAST,
// strictly after both the rotate and the continuation wake.
func TestDetectorPersistsAfterWakes(t *testing.T) {
	var mu sync.Mutex
	var events []string
	cfg := DetectorConfig{
		XOAgent:  "xo",
		Desks:    []string{"xo"},
		Interval: time.Minute,
		Assess:   func(string) surface.State { return surface.StateIdle },
		AckAge:   func() time.Duration { return 0 },
		Rotate:   func() error { mu.Lock(); defer mu.Unlock(); events = append(events, "rotate"); return nil },
		Wake:     func(WakeKind, []string) { mu.Lock(); defer mu.Unlock(); events = append(events, "wake") },
		Persist:  func(Snapshot) error { mu.Lock(); defer mu.Unlock(); events = append(events, "persist"); return nil },
	}
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")

	d.Tick()

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 || events[0] != "rotate" || events[1] != "wake" || events[2] != "persist" {
		t.Fatalf("durable persist must follow the rotate+wake (at-least-once on crash), got %v", events)
	}
}

// The whole point of the tickLocked/runTail split: the rotate's BOUNDED cross-process txn-lock
// wait runs OUTSIDE d.mu, so it can never stall OperatorWake (which shares d.mu). Prove it by
// making Rotate block: while a tick is parked in runTail's rotate, OperatorWake must still return
// promptly. If the rotate were still under d.mu (the old ordering), OperatorWake would deadlock
// behind it until the timeout — the regression this restructure exists to prevent.
func TestDetectorOperatorWakeNotBlockedByRotate(t *testing.T) {
	rotating := make(chan struct{})
	release := make(chan struct{})
	cfg := DetectorConfig{
		XOAgent:  "xo",
		Desks:    []string{"xo"},
		Interval: time.Minute,
		Assess:   func(string) surface.State { return surface.StateIdle },
		AckAge:   func() time.Duration { return 0 },
		Rotate: func() error {
			close(rotating) // signal the rotate has begun (we are in runTail, d.mu released)
			<-release       // block here, holding the tick in the tail
			return nil
		},
		Wake:    func(WakeKind, []string) {},
		Persist: func(Snapshot) error { return nil },
	}
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")

	tickDone := make(chan struct{})
	go func() { d.Tick(); close(tickDone) }()
	<-rotating // the tick is now parked inside the (blocked) rotate, in runTail

	woke := make(chan struct{})
	go func() { d.OperatorWake(); close(woke) }()
	select {
	case <-woke: // OperatorWake acquired d.mu and returned — proving the rotate is NOT under d.mu
	case <-time.After(2 * time.Second):
		t.Fatal("OperatorWake blocked behind a rotate — the rotate is being held under d.mu (the restructure regressed)")
	}

	close(release) // let the parked rotate finish so the tick goroutine exits cleanly
	<-tickDone
}

func TestDetectorLivenessWedgeAndRecovery(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "interval") // alert at K=3 intervals
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.signal = "h0"

	f.ackAge = 2 * time.Minute // < 3×interval → healthy
	d.Tick()
	if len(f.alerts) != 0 {
		t.Fatalf("healthy ack age must not alert, got %v", f.alerts)
	}
	f.ackAge = 4 * time.Minute // > 3×interval → wedged
	d.Tick()
	if len(f.alerts) != 1 {
		t.Fatalf("stale ack must alert once, got %v", f.alerts)
	}
	// Recovery.
	f.ackAge = 1 * time.Minute
	d.Tick()
	if d.wd.Down() {
		t.Error("a fresh ack should clear the down state")
	}
}

func TestDetectorLivenessCrashImmediate(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 99, "none") // huge K — crash must still be immediate
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.set("xo", surface.StateShell)
	f.signal = "h0"
	d.Tick() // shell #1 — debounced, no crash
	if len(f.alerts) != 0 {
		t.Fatalf("single shell read must not crash-alert, got %v", f.alerts)
	}
	d.Tick() // shell #2 — confirmed crash
	if len(f.alerts) != 1 {
		t.Errorf("two consecutive shells must crash-alert immediately, got %v", f.alerts)
	}
}

func TestDetectorWedgeSuppressedWhileShellPending(t *testing.T) {
	// A stale ack coinciding with a shell blip must NOT fire the "wedged" message
	// on the pending tick; the next tick confirms the crash and the single
	// (debounced) alert carries the crash wording.
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "interval") // alert at 3 intervals
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.set("xo", surface.StateShell)
	f.signal = "h0"
	f.ackAge = 10 * time.Minute // also stale — but shell is pending, so no wedge yet

	d.Tick() // shell #1 (pending) + stale ack → NO alert
	if len(f.alerts) != 0 {
		t.Fatalf("wedge must be suppressed while a shell is pending, got %v", f.alerts)
	}
	d.Tick() // shell #2 → confirmed crash → exactly one alert
	if len(f.alerts) != 1 {
		t.Errorf("confirmed crash should alert exactly once, got %v", f.alerts)
	}
}

func TestDetectorMaxQuietPing(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "interval") // pingEvery = K-1 = 2
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.signal = "h0"
	f.ackAge = time.Minute // healthy, no alert

	d.Tick() // quiet 1
	d.Tick() // quiet 2 → ping fires
	pings := 0
	f.mu.Lock()
	for _, w := range f.wakes {
		if w.kind == WakePing {
			pings++
		}
	}
	f.mu.Unlock()
	if pings != 1 {
		t.Errorf("ping count after pingEvery quiet ticks = %d, want 1; wakes=%+v", pings, f.wakes)
	}
}

func TestDetectorSnapshotWriteFailureDegradesLoudly(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.signal = "h0"
	f.persistErr = errors.New("disk full")

	for i := 0; i < snapshotWriteFailThreshold; i++ {
		d.Tick()
	}
	if len(f.alerts) != 1 {
		t.Fatalf("persistent write failure should raise exactly one loud alert, got %v", f.alerts)
	}
	if !d.degraded {
		t.Error("detector should degrade to in-memory-only after the threshold")
	}
	// In degraded mode, diffs still work from the in-memory snapshot: a real
	// change still wakes; an idle fleet does NOT wake-every-tick.
	f.reset()
	d.Tick()
	if f.wakeCount() != 0 {
		t.Errorf("degraded mode must not wake-every-tick on an idle fleet, got %+v", f.wakes)
	}
	f.reset()
	f.set("backend", surface.StateWorking)
	d.Tick()
	f.set("backend", surface.StateIdle)
	d.Tick()
	if f.wakeCount() == 0 {
		t.Error("degraded mode should still wake on a real material change")
	}
}

func TestLivenessParams(t *testing.T) {
	cases := []struct {
		mode               string
		k, n               int
		wantPing, wantAlrt int
	}{
		{"interval", 3, 0, 2, 3},    // strict: ping at K-1, alert at K
		{"consecutive", 3, 0, 2, 4}, // middle: ping at K-1, alert ~2 misses later
		{"none", 3, 0, 6, 7},        // $0-idle: wide safety ping at 2K, alert 2K+1
		{"", 3, 0, 6, 7},            // empty defaults to none
		{"none", 3, 10, 10, 11},     // N override widens both, ping precedes alert
		{"interval", 1, 0, 1, 2},    // degenerate K=1: alert still kept above ping
	}
	for _, tc := range cases {
		ping, alrt := livenessParams(tc.mode, tc.k, tc.n)
		if ping != tc.wantPing || alrt != tc.wantAlrt {
			t.Errorf("livenessParams(%q,k=%d,n=%d) = (ping %d, alert %d), want (%d,%d)",
				tc.mode, tc.k, tc.n, ping, alrt, tc.wantPing, tc.wantAlrt)
		}
		if alrt <= ping {
			t.Errorf("livenessParams(%q): alert %d must exceed ping %d (ping precedes alert)", tc.mode, alrt, ping)
		}
	}
}

func TestDetectorOperatorWakeDuringTickRace(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.signal = "h0"

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); d.Tick() }()
		go func() { defer wg.Done(); d.OperatorWake() }()
	}
	wg.Wait() // -race verifies no data race on the shared detector state (M3)
}
