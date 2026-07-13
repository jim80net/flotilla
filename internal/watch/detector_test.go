package watch

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/roster"
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
	clock       time.Time
}

type wakeRec struct {
	kind    WakeKind
	reasons []string
}

func newFixture() *detFixture {
	return &detFixture{
		states:   map[string]surface.State{},
		signalOK: true,
		clock:    time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	}
}

func (f *detFixture) advance(d time.Duration) {
	f.mu.Lock()
	f.clock = f.clock.Add(d)
	f.mu.Unlock()
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
		Now: func() time.Time {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.clock
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

func TestDetectorUsageObservationIsOptionalAndDurable(t *testing.T) {
	f := newFixture()
	f.set("alpha", surface.StateWorking)
	cfg := f.config("alpha", []string{"alpha"}, 3, "normal")
	cfg.UsageProbePeriod = 30 * time.Minute
	cfg.Usage = func(agent string) (surface.UsageReport, string, string, bool) {
		if agent != "alpha" {
			t.Fatalf("usage agent = %q, want alpha", agent)
		}
		return surface.UsageReport{RemainingPercent: 8, Window: "weekly", Scope: surface.RateLimitAccountSide}, "gateway", "alpha-plan", true
	}
	d := newDet(t, f, cfg)
	d.Tick()

	got := f.persisted[len(f.persisted)-1].Usage["alpha"]
	if got.RemainingPercent != 8 || got.Window != "weekly" || got.Provider != "gateway" || got.SubscriptionID != "alpha-plan" {
		t.Fatalf("persisted usage = %+v", got)
	}
	if got.ObservedAt.IsZero() || got.StaleAfter.Sub(got.ObservedAt) != time.Hour {
		t.Fatalf("usage timestamps = observed %v stale %v, want 60m horizon", got.ObservedAt, got.StaleAfter)
	}
}

func TestDetectorUsageAbsenceNeverCreatesOrErasesEvidence(t *testing.T) {
	f := newFixture()
	f.set("beta", surface.StateIdle)
	available := true
	cfg := f.config("beta", []string{"beta"}, 3, "normal")
	cfg.UsageProbePeriod = 30 * time.Minute
	cfg.Usage = func(string) (surface.UsageReport, string, string, bool) {
		if !available {
			return surface.UsageReport{}, "", "", false
		}
		return surface.UsageReport{RemainingPercent: 42, Window: "weekly", Scope: surface.RateLimitAccountSide}, "gateway", "", true
	}
	d := newDet(t, f, cfg)
	d.Tick()
	first := f.persisted[len(f.persisted)-1].Usage["beta"]

	available = false
	f.advance(31 * time.Minute)
	d.Tick()
	retained := f.persisted[len(f.persisted)-1].Usage["beta"]
	if retained != first {
		t.Fatalf("missing probe changed prior evidence: first=%+v retained=%+v", first, retained)
	}

	cfg2 := f.config("beta", []string{"beta"}, 3, "normal")
	cfg2.Usage = func(string) (surface.UsageReport, string, string, bool) {
		return surface.UsageReport{}, "", "", false
	}
	d2 := newDet(t, f, cfg2)
	d2.Tick()
	if got := f.persisted[len(f.persisted)-1].Usage; len(got) != 0 {
		t.Fatalf("cold absent probe created usage: %+v", got)
	}
}

func TestDetectorPrunesUsageForRemovedDesk(t *testing.T) {
	f := newFixture()
	f.set("alpha", surface.StateIdle)
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := (Snapshot{
		DeskStates: map[string]surface.State{"alpha": surface.StateIdle},
		Usage: map[string]UsageObservation{
			"alpha":   {RemainingPercent: 8, Window: "weekly"},
			"removed": {RemainingPercent: 5, Window: "weekly"},
		},
	}).Save(path); err != nil {
		t.Fatal(err)
	}
	d := NewDetector(f.config("alpha", []string{"alpha"}, 3, "normal"), path)
	if _, ok := d.snap.Usage["removed"]; ok {
		t.Fatalf("removed desk usage survived prune: %+v", d.snap.Usage)
	}
	if _, ok := d.snap.Usage["alpha"]; !ok {
		t.Fatalf("monitored desk usage was pruned: %+v", d.snap.Usage)
	}
}

func TestDetectorUsageAsyncDispatchAndAttemptCadence(t *testing.T) {
	f := newFixture()
	f.set("alpha", surface.StateWorking)
	cfg := f.config("alpha", []string{"alpha"}, 3, "normal")
	cfg.UsageProbePeriod = 30 * time.Minute
	cfg.Usage = func(string) (surface.UsageReport, string, string, bool) {
		return surface.UsageReport{RemainingPercent: 8, Window: "weekly", Scope: surface.RateLimitAccountSide}, "gateway", "", true
	}
	var pending func()
	dispatches := 0
	cfg.UsageDispatch = func(run func()) {
		dispatches++
		pending = run
	}
	d := newDet(t, f, cfg)
	d.Tick()
	if dispatches != 1 || pending == nil {
		t.Fatalf("dispatches=%d pending=%v, want one deferred batch", dispatches, pending != nil)
	}
	if len(d.snap.Usage) != 0 {
		t.Fatalf("async usage folded before dispatch ran: %+v", d.snap.Usage)
	}
	pending()
	if got := d.snap.Usage["alpha"].RemainingPercent; got != 8 {
		t.Fatalf("async usage remaining = %d, want 8", got)
	}
	d.Tick()
	if dispatches != 1 {
		t.Fatalf("immediate tick retried slow probe: dispatches=%d", dispatches)
	}
}

func TestDetectorUsageRejectsInvalidReport(t *testing.T) {
	f := newFixture()
	f.set("alpha", surface.StateIdle)
	cfg := f.config("alpha", []string{"alpha"}, 3, "normal")
	cfg.Usage = func(string) (surface.UsageReport, string, string, bool) {
		return surface.UsageReport{RemainingPercent: 101, Window: "weekly"}, "gateway", "", true
	}
	d := newDet(t, f, cfg)
	d.Tick()
	if len(d.snap.Usage) != 0 {
		t.Fatalf("invalid usage became evidence: %+v", d.snap.Usage)
	}
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

// #470: subtree-only material must not reset the primary quiet clock or settled state.
func TestDetectorStackableWakesSubtreeOnlyPreservesPrimaryClock(t *testing.T) {
	f := newFixture()
	cfg := f.config("cos", []string{"cos", "alpha-xo", "backend"}, 3, "interval")
	cfg.StackableWakes = true
	cfg.OwningXO = func(agent string) string {
		if agent == "backend" {
			return "alpha-xo"
		}
		return "cos"
	}
	cfg.WakeLayer = func(string, WakeKind, []string) {}
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"cos": surface.StateWorking, "alpha-xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("cos", surface.StateIdle)
	f.settle = true
	f.signal = "h0"
	d.Tick() // primary settles
	if !d.snap.XOSettled {
		t.Fatal("primary should be settled")
	}

	f.reset()
	f.set("backend", surface.StateWorking)
	d.Tick()
	f.set("backend", surface.StateIdle)
	d.Tick() // subtree-only layer material
	if !d.snap.XOSettled {
		t.Fatal("subtree-only material must not clear primary settled")
	}
	// Quiet clock for primary must still advance: layer wake must not call OnWake.
	f.advance(2 * d.cfg.ReferenceInterval)
	d.Tick()
	pings := 0
	f.mu.Lock()
	for _, w := range f.wakes {
		if w.kind == WakePing {
			pings++
		}
	}
	f.mu.Unlock()
	if pings != 1 {
		t.Fatalf("primary quiet ping count = %d, want 1 after subtree-only material", pings)
	}
}

// #487 P1: primary-owned desk material must use the primary wake path, not WakeLayer.
func TestDetectorStackableWakesPrimaryOwnedDeskUsesPrimaryWake(t *testing.T) {
	f := newFixture()
	cfg := f.config("cos", []string{"cos", "frontend"}, 3, "none")
	cfg.StackableWakes = true
	cfg.OwningXO = func(string) string { return "cos" }
	layerCalled := false
	cfg.WakeLayer = func(string, WakeKind, []string) { layerCalled = true }
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"cos": surface.StateWorking, "frontend": surface.StateWorking}, "h0")
	f.set("cos", surface.StateWorking)
	f.set("frontend", surface.StateWorking)
	f.set("cos", surface.StateIdle)
	f.settle = true
	f.signal = "h0"
	d.Tick()
	if !d.snap.XOSettled {
		t.Fatal("primary should settle")
	}

	f.reset()
	f.set("frontend", surface.StateIdle)
	d.Tick()
	if layerCalled {
		t.Fatal("primary-owned desk material must not route through WakeLayer")
	}
	if f.wakeCount() != 1 || f.lastWake().kind != WakeMaterial {
		t.Fatalf("primary-owned desk material must wake primary, got %+v", f.wakes)
	}
	if d.snap.XOSettled {
		t.Fatal("primary-owned desk material must re-engage settled primary")
	}
}

func TestDetectorStackableWakesScopesSubtreeToOwner(t *testing.T) {
	f := newFixture()
	cfg := f.config("cos", []string{"cos", "alpha-xo", "backend"}, 3, "none")
	cfg.StackableWakes = true
	cfg.OwningXO = func(agent string) string {
		if agent == "backend" {
			return "alpha-xo"
		}
		return "cos"
	}
	var layerWakes []struct {
		owner   string
		kind    WakeKind
		reasons []string
	}
	cfg.WakeLayer = func(owner string, kind WakeKind, reasons []string) {
		f.mu.Lock()
		layerWakes = append(layerWakes, struct {
			owner   string
			kind    WakeKind
			reasons []string
		}{owner, kind, reasons})
		f.mu.Unlock()
	}
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"cos": surface.StateIdle, "alpha-xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	f.set("backend", surface.StateIdle)
	d.Tick()

	if f.wakeCount() != 0 {
		t.Fatalf("primary Wake must not fire for subtree-only material, got %+v", f.wakes)
	}
	if len(layerWakes) != 1 || layerWakes[0].owner != "alpha-xo" {
		t.Fatalf("layer wake = %+v, want alpha-xo", layerWakes)
	}
}

func TestDetectorStackableWakesFleetWideStaysPrimary(t *testing.T) {
	f := newFixture()
	cfg := f.config("cos", []string{"cos", "backend"}, 3, "none")
	cfg.StackableWakes = true
	cfg.OwningXO = func(string) string { return "cos" }
	cfg.WakeLayer = func(string, WakeKind, []string) {
		t.Fatal("fleet-wide material must not use WakeLayer")
	}
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"cos": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.signal = "h1"
	d.Tick()
	if f.wakeCount() != 1 || f.lastWake().reasons[0] != "external signal changed" {
		t.Fatalf("signal wake = %+v, want primary external signal", f.wakes)
	}
}

func TestDetectorStackableWakesOffPreservesLegacyRouting(t *testing.T) {
	f := newFixture()
	cfg := f.config("cos", []string{"cos", "alpha-xo", "backend"}, 3, "none")
	cfg.StackableWakes = false
	cfg.OwningXO = func(agent string) string {
		if agent == "backend" {
			return "alpha-xo"
		}
		return "cos"
	}
	cfg.WakeLayer = func(string, WakeKind, []string) { t.Fatal("WakeLayer must be inert when flag off") }
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"cos": surface.StateIdle, "alpha-xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	f.set("backend", surface.StateIdle)
	d.Tick()
	if f.wakeCount() != 1 || f.lastWake().kind != WakeMaterial {
		t.Fatalf("legacy routing = %+v", f.wakes)
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
		xoFinishTurn(d, f)
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

// AdjutantSeamOnFinish enqueues a leader brief wake — same pane-ordering contract as the
// continuation wake: rotate MUST complete before the seam enqueue so a trailing /clear never
// wipes the just-delivered adjutant brief.
func TestDetectorTailRotatesBeforeAdjutantSeam(t *testing.T) {
	var mu sync.Mutex
	var events []string
	cfg := DetectorConfig{
		XOAgent:  "xo",
		Desks:    []string{"xo"},
		Interval: time.Minute,
		Assess:   func(string) surface.State { return surface.StateIdle },
		AckAge:   func() time.Duration { return 0 },
		Rotate: func() error {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "rotate")
			return nil
		},
		AdjutantFor: func(owner string) string {
			if owner == "xo" {
				return "xo-adj"
			}
			return ""
		},
		AdjutantSeamOnFinish: func(owner string) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "adjutant-seam:"+owner)
		},
		Wake:    func(WakeKind, []string) {},
		Persist: func(Snapshot) error { return nil },
	}
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")

	d.Tick()

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "rotate" || events[1] != "adjutant-seam:xo" {
		t.Fatalf("tail must rotate THEN adjutant seam, got %v", events)
	}
}

func TestDetectorAdjutantSeamPerOwner(t *testing.T) {
	f := newFixture()
	var drained []string
	cfg := f.config("cos", []string{"cos", "alpha-xo"}, 3, "none")
	cfg.AdjutantFor = func(owner string) string {
		if owner == "alpha-xo" {
			return "alpha-adj"
		}
		return ""
	}
	cfg.AdjutantSeamOnFinish = func(owner string) { drained = append(drained, owner) }
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"cos": surface.StateIdle, "alpha-xo": surface.StateIdle}, "h0")
	f.set("cos", surface.StateIdle)
	f.set("alpha-xo", surface.StateIdle)
	d.Tick()
	if len(drained) != 0 {
		t.Fatalf("steady fleet should not drain, got %v", drained)
	}
	f.set("alpha-xo", surface.StateWorking)
	d.Tick()
	f.set("alpha-xo", surface.StateIdle)
	d.Tick()
	if len(drained) != 1 || drained[0] != "alpha-xo" {
		t.Fatalf("alpha-xo seam drain = %v, want [alpha-xo]", drained)
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

	d.Tick() // quiet 1 — start the wall-time quiet clock
	f.advance(2 * d.cfg.ReferenceInterval)
	d.Tick() // quiet period elapsed → ping fires
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

func TestDetectorRateLimitMaterialWake(t *testing.T) {
	f := newFixture()
	probeCalls := 0
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		if agent != "backend" {
			return false, 0, "", false
		}
		probeCalls++
		if probeCalls < 2 {
			return false, surface.RateLimitServerSide, "", true
		}
		return true, surface.RateLimitServerSide, "Server is temporarily limiting requests", true
	}
	cfg.RateLimitReset = func(string) {}
	d := newDet(t, f, cfg)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.signal = "h0"

	d.Tick() // cold-start wake only (no rate-limit probe — early return)
	f.wakes = nil

	f.advance(d.cfg.ReferenceInterval)
	d.Tick() // probe #1 OFF mutex → pending not material; fold-back: no wake yet
	if len(f.wakes) != 0 {
		t.Fatalf("tick 1 wakes = %v, want none (pending fold-back)", f.wakes)
	}

	f.advance(d.cfg.ReferenceInterval)
	d.Tick() // read pending (not material) + probe #2 → pending material; still no wake
	if len(f.wakes) != 0 {
		t.Fatalf("tick 2 wakes = %v, want none (material pending folds next tick)", f.wakes)
	}

	d.Tick() // read pending (material) → wake
	if len(f.wakes) != 1 || f.wakes[0].kind != WakeMaterial {
		t.Fatalf("tick 3 wakes = %+v, want one WakeMaterial", f.wakes)
	}
	if len(f.wakes[0].reasons) != 1 || f.wakes[0].reasons[0] != "backend: rate-limited (server-side — switch eligible)" {
		t.Fatalf("reasons = %v", f.wakes[0].reasons)
	}

	f.wakes = nil
	d.Tick() // same episode — no repeat wake
	if len(f.wakes) != 0 {
		t.Fatalf("tick 4 wakes = %v, want none (edge-triggered)", f.wakes)
	}
}

func TestDetectorRateLimitConcurrentUnderRace(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		time.Sleep(2 * time.Millisecond) // simulate slow tmux capture
		if agent != "backend" {
			return false, 0, "", false
		}
		return false, 0, "", true
	}
	cfg.RateLimitReset = func(string) {}
	cfg.RateLimitDispatch = func(run func()) { go run() }
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
	wg.Wait() // -race: rate-limit probe path must not race with OperatorWake
}

func TestDetectorAutoSwitchEnqueuesOncePerEpisode(t *testing.T) {
	f := newFixture()
	var autoCalls []RateLimitAutoSwitchCandidate
	var det *Detector
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	probeCalls := 0
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		if agent != "backend" {
			return false, 0, "", false
		}
		probeCalls++
		if probeCalls < 2 {
			return false, surface.RateLimitServerSide, "", true
		}
		return true, surface.RateLimitServerSide, "limited", true
	}
	cfg.RateLimitAutoSwitchEligible = func(agent string) bool { return agent == "backend" }
	cfg.RateLimitAutoSwitch = func(candidates []RateLimitAutoSwitchCandidate) {
		autoCalls = append(autoCalls, candidates...)
		for _, c := range candidates {
			det.EndAutoSwitchFlight(c.Agent)
		}
	}
	det = newDet(t, f, cfg)
	d := det
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.signal = "h0"

	d.Tick()
	f.advance(d.cfg.ReferenceInterval)
	d.Tick()
	f.advance(d.cfg.ReferenceInterval)
	d.Tick() // material episode + auto-switch candidate
	if len(autoCalls) != 1 || autoCalls[0].Agent != "backend" {
		t.Fatalf("auto-switch calls = %+v, want one backend candidate", autoCalls)
	}
	autoCalls = nil
	d.Tick() // same episode — no second auto-switch
	if len(autoCalls) != 0 {
		t.Fatalf("tick 4 auto-switch = %+v, want none (edge-triggered)", autoCalls)
	}
}

func TestDetectorAutoSwitchSkipsNonClaudeFromSurface(t *testing.T) {
	f := newFixture()
	var autoCalls []RateLimitAutoSwitchCandidate
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		return true, surface.RateLimitServerSide, "limited", true
	}
	fromSurface := "grok" // mirrors watch.go agentSurface != claude-code
	cfg.RateLimitAutoSwitchEligible = func(agent string) bool {
		return agent == "backend" && fromSurface == surface.DefaultSurface
	}
	cfg.RateLimitAutoSwitch = func(candidates []RateLimitAutoSwitchCandidate) {
		autoCalls = append(autoCalls, candidates...)
	}
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("backend", surface.StateIdle)
	f.signal = "h0"
	d.Tick()
	d.Tick()
	if len(autoCalls) != 0 {
		t.Fatalf("grok FROM desk must not auto-switch, got %+v", autoCalls)
	}
}

func TestDetectorAutoSwitchRefusesIneligibleDesk(t *testing.T) {
	f := newFixture()
	var autoCalls []RateLimitAutoSwitchCandidate
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		return true, surface.RateLimitServerSide, "limited", true
	}
	cfg.RateLimitAutoSwitchEligible = func(string) bool { return false }
	cfg.RateLimitAutoSwitch = func(candidates []RateLimitAutoSwitchCandidate) {
		autoCalls = append(autoCalls, candidates...)
	}
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("backend", surface.StateIdle)
	f.signal = "h0"
	d.Tick()
	d.Tick()
	if len(autoCalls) != 0 {
		t.Fatalf("ineligible desk must not auto-switch, got %+v", autoCalls)
	}
}

func TestDetectorAutoSwitchConcurrentUnderRace(t *testing.T) {
	f := newFixture()
	var det *Detector
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		time.Sleep(2 * time.Millisecond)
		if agent != "backend" {
			return false, 0, "", false
		}
		return true, surface.RateLimitServerSide, "limited", true
	}
	cfg.RateLimitAutoSwitchEligible = func(agent string) bool { return agent == "backend" }
	cfg.RateLimitAutoSwitch = func(candidates []RateLimitAutoSwitchCandidate) {
		for _, c := range candidates {
			go func(agent string) {
				time.Sleep(time.Millisecond)
				det.EndAutoSwitchFlight(agent)
			}(c.Agent)
		}
	}
	cfg.RateLimitDispatch = func(run func()) { go run() }
	det = newDet(t, f, cfg)
	d := det
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("backend", surface.StateIdle)
	f.signal = "h0"
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); d.Tick() }()
		go func() { defer wg.Done(); d.OperatorWake() }()
	}
	wg.Wait() // -race: auto-switch flight map must not race with OperatorWake
}

// #510: primary XO is included in the rate-limit probe batch (Idle).
func TestDetectorRateLimitProbesPrimaryXO(t *testing.T) {
	f := newFixture()
	probed := map[string]int{}
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		probed[agent]++
		return false, 0, "", true
	}
	cfg.RateLimitReset = func(string) {}
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.signal = "h0"
	f.advance(d.cfg.ReferenceInterval)
	d.Tick() // schedules probe work
	// runRateLimitProbes is sync when RateLimitDispatch is nil
	d.Tick()
	if probed["xo"] == 0 {
		t.Fatalf("primary XO must be rate-limit probed when Idle, probed=%v", probed)
	}
	if probed["backend"] == 0 {
		t.Fatalf("backend must still be probed, probed=%v", probed)
	}
}

// #510: coordinator rate-limit edge fires leader-exhaustion callback + auto-switch.
func TestDetectorLeaderExhaustionAndCoordinatorAutoSwitch(t *testing.T) {
	f := newFixture()
	var autoCalls []RateLimitAutoSwitchCandidate
	var exhausted []string
	var det *Detector
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	probeN := 0
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		if agent != "xo" {
			return false, 0, "", true
		}
		probeN++
		if probeN < 2 {
			return false, 0, "", true
		}
		return true, surface.RateLimitAccountSide, "usage limit", true
	}
	cfg.IsCoordinator = func(name string) bool { return name == "xo" }
	cfg.RateLimitAutoSwitchEligible = func(agent string) bool { return agent == "xo" }
	cfg.RateLimitAutoSwitch = func(candidates []RateLimitAutoSwitchCandidate) {
		autoCalls = append(autoCalls, candidates...)
		for _, c := range candidates {
			det.EndAutoSwitchFlight(c.Agent)
		}
	}
	cfg.RateLimitLeaderExhausted = func(agent string, scope surface.RateLimitScope) {
		exhausted = append(exhausted, agent+":"+scope.String())
	}
	det = newDet(t, f, cfg)
	d := det
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.signal = "h0"

	// Warm probes until material episode folds.
	for i := 0; i < 6; i++ {
		f.advance(d.cfg.ReferenceInterval)
		d.Tick()
		if len(autoCalls) > 0 {
			break
		}
	}
	if len(autoCalls) != 1 || autoCalls[0].Agent != "xo" {
		t.Fatalf("auto-switch = %+v, want xo", autoCalls)
	}
	if len(exhausted) != 1 || exhausted[0] != "xo:account-side" {
		t.Fatalf("leader exhaustion = %v, want xo:account-side", exhausted)
	}
}

// #510: two consecutive clear folds enqueue auto-revert when eligible.
func TestDetectorAutoRevertAfterClearHysteresis(t *testing.T) {
	f := newFixture()
	var reverts []string
	var det *Detector
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		if agent != "backend" {
			return false, 0, "", true
		}
		return false, 0, "", true // always clear
	}
	cfg.RateLimitAutoRevertEligible = func(agent string) bool { return agent == "backend" }
	cfg.RateLimitAutoRevert = func(agents []string) {
		reverts = append(reverts, agents...)
		for _, a := range agents {
			det.EndAutoSwitchFlight(a)
		}
	}
	det = newDet(t, f, cfg)
	d := det
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("backend", surface.StateIdle)
	f.signal = "h0"

	// Probe tick → fold clear #1 → fold clear #2 (hysteresis) → revert.
	for i := 0; i < 8 && len(reverts) == 0; i++ {
		f.advance(d.cfg.ReferenceInterval)
		d.Tick()
	}
	if len(reverts) != 1 || reverts[0] != "backend" {
		t.Fatalf("auto-revert = %v, want [backend] after hysteresis", reverts)
	}
}

// xoFinishLiveTick drives one XO Working→Idle cycle and advances only the live tick
// interval (not referenceInterval). Use when testing wall-gated sub-cadences at a
// faster live tick than the roster ceiling.
func xoFinishLiveTick(d *Detector, f *detFixture) {
	f.set("xo", surface.StateWorking)
	d.Tick()
	f.set("xo", surface.StateIdle)
	d.Tick()
	f.advance(d.cfg.Interval)
}

func wallCadenceConfig(f *detFixture) DetectorConfig {
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	cfg.Interval = 2 * time.Minute
	cfg.ReferenceInterval = 20 * time.Minute
	return cfg
}

func TestLivenessParamsWall(t *testing.T) {
	ref := 20 * time.Minute
	cases := []struct {
		mode               string
		k, n               int
		wantPing, wantAlrt time.Duration
	}{
		{"interval", 3, 0, 40 * time.Minute, 60 * time.Minute},
		{"consecutive", 3, 0, 40 * time.Minute, 80 * time.Minute},
		{"none", 3, 0, 120 * time.Minute, 140 * time.Minute},
		{"", 3, 0, 120 * time.Minute, 140 * time.Minute},
		{"none", 3, 10, 200 * time.Minute, 220 * time.Minute},
		{"interval", 1, 0, 20 * time.Minute, 40 * time.Minute},
	}
	for _, tc := range cases {
		ping, alrt := livenessParamsWall(tc.mode, tc.k, ref, tc.n)
		if ping != tc.wantPing || alrt != tc.wantAlrt {
			t.Errorf("livenessParamsWall(%q,k=%d,n=%d,ref=20m) = (ping %v, alert %v), want (%v,%v)",
				tc.mode, tc.k, tc.n, ping, alrt, tc.wantPing, tc.wantAlrt)
		}
		if alrt <= ping {
			t.Errorf("livenessParamsWall(%q): alert %v must exceed ping %v", tc.mode, alrt, ping)
		}
	}
}

// #467: with rotate policy never, backlog drive must deliver WakeBacklog without rotating.
func TestDetectorContinueXOBacklogSkipsRotateWhenPolicyNever(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	cfg.RotatePolicy = roster.XORotateNever
	cfg.ReferenceInterval = time.Minute
	f.backlog = backlog.Status{Unblocked: []string{"ship the knob PR"}}
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")
	f.signal = "h0"
	f.set("xo", surface.StateIdle)
	d.Tick()
	if f.rotateCalls != 0 {
		t.Fatalf("rotate policy never: got %d rotate calls, want 0", f.rotateCalls)
	}
	if f.wakeCount() != 1 || f.lastWake().kind != WakeBacklog {
		t.Fatalf("want one WakeBacklog, got %+v", f.wakes)
	}
}

// PR 1 gate: at a 2m live tick, the continuation path must call requestRotate at most once
// per referenceInterval even when the XO repeatedly finishes turns with an empty backlog.
func TestDetectorContinueXORotateWall(t *testing.T) {
	f := newFixture()
	cfg := wallCadenceConfig(f)
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.signal = "h0"

	for i := 0; i < 10; i++ {
		xoFinishLiveTick(d, f)
	}
	if f.rotateCalls != 1 {
		t.Fatalf("continuation rotates = %d, want 1 (at most one per referenceInterval at 2m live tick)", f.rotateCalls)
	}

	f.reset()
	f.advance(20 * time.Minute)
	xoFinishLiveTick(d, f)
	if f.rotateCalls != 1 {
		t.Fatalf("after referenceInterval elapsed, want one more rotate, got %d total in window", f.rotateCalls)
	}
}

// PR 1 gate: rotate-on-settle is preserved — settle still rotates unless Awaiting, even when
// continuationDue() is false because a recent continuation already fired.
func TestDetectorContinueXOSettleRotate(t *testing.T) {
	f := newFixture()
	cfg := wallCadenceConfig(f)
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")
	f.signal = "h0"

	// Continuation path: sets lastContinuationAt; only 2m later (not referenceInterval).
	xoFinishLiveTick(d, f)
	if f.rotateCalls != 1 {
		t.Fatalf("precondition: continuation must rotate once, got %d", f.rotateCalls)
	}
	contRotates := f.rotateCalls

	// Settle path within the same referenceInterval window — must still rotate.
	f.settle = true
	f.set("xo", surface.StateWorking)
	d.Tick()
	f.set("xo", surface.StateIdle)
	d.Tick()

	if f.rotateCalls != contRotates+1 {
		t.Fatalf("settle must rotate even when !continuationDue(); rotates = %d, want %d", f.rotateCalls, contRotates+1)
	}
	if !d.snap.XOSettled {
		t.Fatal("settle path must mark XO settled")
	}

	// Awaiting suppresses rotate on settle (unchanged contract).
	f2 := newFixture()
	d2 := newDet(t, f2, wallCadenceConfig(f2))
	seed(d2, map[string]surface.State{"xo": surface.StateWorking}, "h0")
	f2.signal = "h0"
	f2.settle = true
	f2.awaiting = true
	f2.set("xo", surface.StateIdle)
	d2.Tick()
	if f2.rotateCalls != 0 {
		t.Fatalf("settle must skip rotate while awaiting operator, got %d", f2.rotateCalls)
	}
}

func TestDetectorRateLimitSkipsWorkingDesk(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	// Only backend reports limited when probed; Working desks must never be probed.
	cfg.RateLimitMaterial = func(agent string) (bool, surface.RateLimitScope, string, bool) {
		if agent == "backend" {
			return true, surface.RateLimitServerSide, "limited", true
		}
		return false, 0, "", true // Idle XO may be probed (#510) but is not limited here
	}
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateWorking)
	f.signal = "h0"
	d.Tick()
	f.wakes = nil
	d.Tick()
	for _, w := range f.wakes {
		for _, r := range w.reasons {
			if strings.Contains(r, "backend") && strings.Contains(r, "rate-limited") {
				t.Fatalf("mid-turn desk must not rate-limit wake, got %v", f.wakes)
			}
		}
	}
}
