package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// hbFixture is the #183 group 4-6 test rig: it drives the detector's recursive desk-heartbeat
// state machine through ticks and records the OFF-mutex deliveries (the per-desk beats via
// WakeDeskHeartbeat and the cap-escalations via DeskEscalate). It mirrors detFixture's
// minimal-collaborator style so every heartbeat test runs without tmux, a clock, or the filesystem.
//
// The cadence/cap are pinned per test (DeskHeartbeatEveryTicks / DeskHeartbeatCap) so each transition
// is deterministic; the per-desk enablement + settle-marker are injected closures over fixture maps.
type hbFixture struct {
	mu          sync.Mutex
	states      map[string]surface.State
	enabled     map[string]bool // agent → desk-heartbeat enabled (HeartbeatEnabled)
	settleNow   map[string]bool // agent → settle marker present (consumed when read)
	beats       []string        // WakeDeskHeartbeat agents, in delivery order
	escalations []string        // DeskEscalate agents, in delivery order
	clock       time.Time
}

func newHBFixture() *hbFixture {
	return &hbFixture{
		states:    map[string]surface.State{},
		enabled:   map[string]bool{},
		settleNow: map[string]bool{},
		clock:     time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	}
}

func (f *hbFixture) advance(d time.Duration) {
	f.mu.Lock()
	f.clock = f.clock.Add(d)
	f.mu.Unlock()
}

func (f *hbFixture) set(agent string, s surface.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[agent] = s
}

// config wires a detector with the recursive-desk-heartbeat seams. enabledDesks are the desks that
// resolve HeartbeatEnabled=true (everything else, incl. the XO, resolves false). cadence/cap pin the
// per-agent windows.
func (f *hbFixture) config(xo string, desks, enabledDesks []string, cadence, cap int) DetectorConfig {
	for _, d := range enabledDesks {
		f.enabled[d] = true
	}
	return DetectorConfig{
		XOAgent:  xo,
		Desks:    desks,
		Interval: time.Minute,
		Assess: func(a string) surface.State {
			f.mu.Lock()
			defer f.mu.Unlock()
			if s, ok := f.states[a]; ok {
				return s
			}
			return surface.StateUnknown
		},
		AckAge:  func() time.Duration { return 0 },
		Wake:    func(WakeKind, []string) {},
		Persist: func(Snapshot) error { return nil },
		HeartbeatEnabled: func(a string) bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.enabled[a]
		},
		DeskSettleConsume: func(a string) bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			was := f.settleNow[a]
			f.settleNow[a] = false
			return was
		},
		WakeDeskHeartbeat: func(a string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.beats = append(f.beats, a)
		},
		DeskEscalate: func(a string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.escalations = append(f.escalations, a)
		},
		DeskHeartbeatEveryTicks: cadence,
		DeskHeartbeatCap:        cap,
		Now: func() time.Time {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.clock
		},
	}
}

func (f *hbFixture) newDet(t *testing.T, cfg DetectorConfig) *Detector {
	t.Helper()
	return NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
}

func (f *hbFixture) beatLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.beats...)
}

func (f *hbFixture) escLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.escalations...)
}

func (f *hbFixture) resetLog() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beats = nil
	f.escalations = nil
}

// (1) The COLD-START tick seeds the baseline and owes NO desk a heartbeat — the per-desk block is
// reached only past the cold-start early-return, so a fresh boot never storm-beats every idle desk.
func TestDeskHeartbeat_ColdStartNoBeat(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg) // cold (missing snapshot)

	d.Tick()

	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("cold-start tick must owe no desk a beat, got %v", got)
	}
	if got := f.escLog(); len(got) != 0 {
		t.Fatalf("cold-start tick must escalate nothing, got %v", got)
	}
}

// (2) An IDLE desk that has accrued >= cadence ticks is OWED exactly one beat, and its cadence
// counter resets (so the next beat is a full cadence away).
func TestDeskHeartbeat_IdleCadenceBeats(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 2, 3)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	d.Tick() // anchor first idle tick — no beat
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("tick 1 (cadence not elapsed) must not beat, got %v", got)
	}
	f.advance(2 * time.Minute) // cadence=2 ticks × 1m ref
	d.Tick()                   // period elapsed ⇒ ONE beat
	if got := f.beatLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("tick 2 (cadence elapsed) must beat backend once, got %v", got)
	}
	f.advance(time.Minute)
	d.Tick() // within period after beat ⇒ no new beat
	if got := f.beatLog(); len(got) != 1 {
		t.Fatalf("tick 3 must not beat again (cadence reset), got %v", got)
	}
}

// (3) A SETTLED desk (its per-agent marker consumed) is suppressed — no beat, no cadence accrual —
// until it is re-armed by AgentWake.
func TestDeskHeartbeat_SettledSuppressedUntilRearm(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.settleNow["backend"] = true // backend touched its settle marker
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	d.Tick() // marker consumed ⇒ settled ⇒ suppressed
	d.Tick()
	d.Tick()
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("a settled desk must be suppressed (no beats), got %v", got)
	}

	// re-arm: an operator/XO message re-engages it → beats resume on the next cadence.
	d.AgentWake("backend")
	d.Tick() // anchor
	f.advance(time.Minute)
	d.Tick() // cadence 1: period elapsed ⇒ beat
	if got := f.beatLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("after AgentWake a settled desk must beat again, got %v", got)
	}
}

// (4) A WORKING desk is making progress: no beat, the cadence + cap counters reset, and the
// progressed latch is set (so an owed beat after it never counts toward the cap).
func TestDeskHeartbeat_WorkingResetsAndLatchesProgress(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3)
	f.set("xo", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	// pre-load some no-progress so the reset is observable
	d.deskNoProgress["backend"] = 2
	d.deskBeatEligibleAt["backend"] = f.clock.Add(-5 * time.Minute)

	f.set("backend", surface.StateWorking)
	d.Tick()

	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("a Working desk must not beat, got %v", got)
	}
	if d.deskNoProgress["backend"] != 0 {
		t.Fatalf("Working must reset cap(%d) to 0", d.deskNoProgress["backend"])
	}
	if _, ok := d.deskBeatEligibleAt["backend"]; ok {
		t.Fatal("Working must clear deskBeatEligibleAt cadence anchor")
	}
	if !d.deskProgressed["backend"] {
		t.Fatal("Working must latch deskProgressed=true")
	}
}

// (5) Idle→Working→Idle: the Working edge resets the cap so a desk that periodically makes progress
// NEVER escalates, even across many beats.
func TestDeskHeartbeat_ProgressBetweenBeatsNeverEscalates(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3)
	f.set("xo", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	// Each cycle: a beat (idle, progressed=false → cap++), then a Working edge resets the cap.
	for i := 0; i < 6; i++ {
		f.set("backend", surface.StateIdle)
		d.Tick() // owed a beat; but the progressed latch from the prior Working tick zeroes the cap
		f.set("backend", surface.StateWorking)
		d.Tick() // progress: resets cap, latches progressed
	}
	if got := f.escLog(); len(got) != 0 {
		t.Fatalf("a desk that makes progress between beats must never escalate, got %v", got)
	}
	if d.deskStopped["backend"] {
		t.Fatal("a progressing desk must never be stopped")
	}
}

// (6) capN consecutive no-progress beats ⇒ ONE escalation (edge-trigger on ==capN) + stopped; further
// ticks are silent (no more beats, no repeated escalation).
func TestDeskHeartbeat_CapEscalatesOnceThenStops(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	// cadence=1, cap=3: each idle tick owes a beat and (no progress) increments the cap.
	// tick1 anchors; ticks 2–4 (spaced by the heartbeat period) owe beats until cap.
	d.Tick()
	for i := 0; i < 3; i++ {
		f.advance(time.Minute)
		d.Tick()
	}
	if got := f.beatLog(); len(got) != 3 {
		t.Fatalf("expected 3 beats before the cap, got %v", got)
	}
	if got := f.escLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("expected exactly one escalation on the ==cap edge, got %v", got)
	}
	if !d.deskStopped["backend"] {
		t.Fatal("a capped desk must be stopped")
	}

	// further ticks are silent: no new beat, no repeated escalation.
	f.resetLog()
	d.Tick()
	d.Tick()
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("a stopped desk must not beat, got %v", got)
	}
	if got := f.escLog(); len(got) != 0 {
		t.Fatalf("a stopped desk must not re-escalate, got %v", got)
	}
}

// (7) AgentWake AFTER a desk was capped+stopped re-arms it → beats resume on the next cadence.
func TestDeskHeartbeat_RearmAfterStopResumesBeats(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	for i := 0; i < 4; i++ {
		if i > 0 {
			f.advance(time.Minute)
		}
		d.Tick()
	} // capped + stopped
	if !d.deskStopped["backend"] {
		t.Fatal("precondition: backend must be stopped after the cap")
	}
	f.resetLog()

	d.AgentWake("backend") // operator re-engages the wedged desk
	d.Tick()               // anchor
	f.advance(time.Minute)
	d.Tick() // cadence elapsed ⇒ a fresh beat
	if got := f.beatLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("after AgentWake a stopped desk must beat again, got %v", got)
	}
}

// (8) An opted-out desk (HeartbeatEnabled=false — e.g. an approval-sensitive desk default-off) is
// NEVER beaten and NEVER escalated, no matter how long it sits idle.
func TestDeskHeartbeat_OptedOutNeverBeats(t *testing.T) {
	f := newHBFixture()
	// "backend" enabled; "trader" present but NOT enabled (approval-sensitive default-off).
	cfg := f.config("xo", []string{"xo", "backend", "trader"}, []string{"backend"}, 1, 3)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	f.set("trader", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle, "trader": surface.StateIdle}, "h0")

	for i := 0; i < 5; i++ {
		d.Tick()
	}
	for _, a := range f.beatLog() {
		if a == "trader" {
			t.Fatalf("an opted-out desk must never beat, got beats %v", f.beatLog())
		}
	}
	for _, a := range f.escLog() {
		if a == "trader" {
			t.Fatalf("an opted-out desk must never escalate, got %v", f.escLog())
		}
	}
}

// (9) The primary XO is never desk-heartbeated (it has its own clock). The tickLocked block skips
// name == XOAgent outright; HeartbeatEnabled also resolves false for the XO at the wiring layer.
func TestDeskHeartbeat_PrimaryXONeverBeats(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateWorking) // keep backend out of the way
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")

	for i := 0; i < 5; i++ {
		d.Tick()
	}
	for _, a := range f.beatLog() {
		if a == "xo" {
			t.Fatalf("the primary XO must never be desk-heartbeated, got %v", f.beatLog())
		}
	}
}

// (10) An UNKNOWN-state desk (unresolvable pane) accrues NO cadence and is never beaten — an
// unreadable pane is not a confirmed Idle.
func TestDeskHeartbeat_UnknownNoBeatNoAccrual(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateUnknown)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateUnknown}, "h0")

	d.Tick()
	d.Tick()
	d.Tick()
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("an Unknown-state desk must never beat, got %v", got)
	}
	if _, ok := d.deskBeatEligibleAt["backend"]; ok {
		t.Fatal("an Unknown-state desk must accrue no cadence anchor")
	}
}

// (11) BYTE-INERT: with HeartbeatEnabled nil (the feature unwired), the detector's existing behavior
// is unchanged — no beats, no escalations, no panic. The exhaustive byte-inert proof is the existing
// detector/synthesis suites passing unchanged; this asserts the local invariant directly.
func TestDeskHeartbeat_ByteInertWhenUnwired(t *testing.T) {
	f := newHBFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, nil, 1, 3)
	cfg.HeartbeatEnabled = nil  // the feature is OFF
	cfg.WakeDeskHeartbeat = nil // and unwired
	cfg.DeskEscalate = nil      // and unwired
	cfg.DeskSettleConsume = nil // and unwired
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	for i := 0; i < 5; i++ {
		d.Tick() // must not panic, must not beat
	}
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("byte-inert: an unwired detector must never beat, got %v", got)
	}
	if got := f.escLog(); len(got) != 0 {
		t.Fatalf("byte-inert: an unwired detector must never escalate, got %v", got)
	}
}
