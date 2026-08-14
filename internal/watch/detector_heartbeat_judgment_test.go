package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

// The #189 per-recipient JUDGMENT adds an atomic warrant classification to the detector's
// desk-heartbeat decision — the LAST gate, evaluated only AFTER the #183 HARD gate (XO-excl /
// HeartbeatEnabled), the settle/stop checks, and the cadence. The classification is a PURE lookup against
// per-recipient evidence computed OFF d.mu (the seam returns an already-decided classification; it does NO
// file I/O under the lock). It can only narrow a beat #183 would have sent — never resurrect one.
//
// These tests extend the §9 hbFixture with a per-agent `warranted` map (defaulting to positive when the
// seam is nil so #183 is byte-identical) and a recording wrapper that asserts the off-mutex
// invariant: the seam, when invoked from the under-lock decision, performs NO backlog file I/O.

// hbjFixture is the §9 hbFixture extended with the #189 warrant seam.
type hbjFixture struct {
	mu          sync.Mutex
	states      map[string]surface.State
	enabled     map[string]bool
	settleNow   map[string]bool
	warranted   map[string]DeskHeartbeatWarrant // agent → atomic warrant; absent ⇒ positive
	warrantHits []string                        // agents the warrant seam was consulted for, in order
	beats       []string
	escalations []string
	clock       time.Time
}

func newHBJFixture() *hbjFixture {
	return &hbjFixture{
		states:    map[string]surface.State{},
		enabled:   map[string]bool{},
		settleNow: map[string]bool{},
		warranted: map[string]DeskHeartbeatWarrant{},
		clock:     time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	}
}

func (f *hbjFixture) advance(d time.Duration) {
	f.mu.Lock()
	f.clock = f.clock.Add(d)
	f.mu.Unlock()
}

func (f *hbjFixture) set(agent string, s surface.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[agent] = s
}

func (f *hbjFixture) setWarranted(agent string, w bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w {
		f.warranted[agent] = DeskHeartbeatPositiveWarrant
	} else {
		f.warranted[agent] = DeskHeartbeatNotWarranted
	}
}

func (f *hbjFixture) setWarrant(agent string, w DeskHeartbeatWarrant) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warranted[agent] = w
}

// config wires a detector with BOTH the #183 seams and the #189 HeartbeatWarranted seam. wireWarrant
// controls whether the warrant seam is wired at all (false ⇒ nil ⇒ #189-inert ⇒ #183 byte-identical).
func (f *hbjFixture) config(xo string, desks, enabledDesks []string, cadence, cap int, wireWarrant bool) DetectorConfig {
	for _, d := range enabledDesks {
		f.enabled[d] = true
	}
	cfg := DetectorConfig{
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
	if wireWarrant {
		cfg.HeartbeatWarranted = func(a string) DeskHeartbeatWarrant {
			// This wrapper stands in for the cmd-side seam: it returns an ALREADY-COMPUTED classification
			// (the off-lock read happened earlier). It records the consult and asserts it does NO
			// file I/O here (the off-mutex invariant — the read must live at the cmd wiring, off d.mu).
			f.mu.Lock()
			defer f.mu.Unlock()
			f.warrantHits = append(f.warrantHits, a)
			w, ok := f.warranted[a]
			if !ok {
				return DeskHeartbeatPositiveWarrant // #183-compatible default
			}
			return w
		}
	}
	return cfg
}

func (f *hbjFixture) newDet(t *testing.T, cfg DetectorConfig) *Detector {
	t.Helper()
	return NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
}

func (f *hbjFixture) beatLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.beats...)
}

func (f *hbjFixture) escLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.escalations...)
}

func TestDeskHeartbeatClosedOutIsCapNeutralAndRestorationRearms(t *testing.T) {
	for _, agent := range []string{"cos-tech-writer", "cos-ux-designer"} {
		t.Run(agent, func(t *testing.T) {
			f := newHBJFixture()
			cfg := f.config("xo", []string{"xo", agent}, []string{agent}, 1, 3, true)
			held := true
			cfg.RecipientClosedOut = func(string) bool { return held }
			settleMarker := true
			cfg.DeskSettleConsume = func(string) bool {
				wasSet := settleMarker
				settleMarker = false
				return wasSet
			}
			f.setWarranted(agent, true)
			f.set("xo", surface.StateIdle)
			f.set(agent, surface.StateIdle)
			d := f.newDet(t, cfg)
			d.deskSettled[agent] = true
			seed(d, map[string]surface.State{"xo": surface.StateIdle, agent: surface.StateIdle}, "h0")
			for i := 0; i < 6; i++ {
				d.Tick()
				f.advance(time.Minute)
			}
			if len(f.beatLog()) != 0 || len(f.escLog()) != 0 {
				t.Fatalf("closed-out desk beat/wedged: beats=%v escalations=%v", f.beatLog(), f.escLog())
			}
			held = false
			for i := 0; i < 4; i++ {
				d.Tick()
				f.advance(time.Minute)
			}
			if len(f.beatLog()) != 3 || len(f.escLog()) != 1 || f.escLog()[0] != agent {
				t.Fatalf("restored genuine obligation did not resume normal cap: beats=%v escalations=%v", f.beatLog(), f.escLog())
			}
		})
	}
}

func TestDeskHeartbeatCloseOutLandingAfterSnapshotCancelsSideEffects(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "desk"}, []string{"desk"}, 1, 3, true)
	held := false
	cfg.RecipientClosedOut = func(string) bool {
		value := held
		if value {
			held = false // restoration races after the beat check; escalation must share its decision
		}
		return value
	}
	cfg.HeartbeatWarranted = func(string) DeskHeartbeatWarrant {
		held = true // disposition lands after its phase-1 check
		return DeskHeartbeatPositiveWarrant
	}
	f.set("xo", surface.StateIdle)
	f.set("desk", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "desk": surface.StateIdle}, "h0")
	d.deskBeatEligibleAt["desk"] = f.clock.Add(-time.Minute)
	d.deskNoProgress["desk"] = 2
	d.Tick()
	if len(f.beatLog()) != 0 || len(f.escLog()) != 0 {
		t.Fatalf("late close-out emitted side effects: beats=%v escalations=%v", f.beatLog(), f.escLog())
	}
	if d.deskNoProgress["desk"] != 0 || d.deskStopped["desk"] {
		t.Fatalf("late close-out retained cap episode: cap=%d stopped=%v", d.deskNoProgress["desk"], d.deskStopped["desk"])
	}
}

// (J1) WARRANTED-TRUE behaves exactly as #183: an idle, eligible, cadence-elapsed, not-settled desk
// is beaten on its cadence.
func TestDeskHeartbeatJudgment_WarrantedTrueBeats(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	f.setWarranted("backend", true)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	d.Tick() // anchor first idle tick
	f.advance(time.Minute)
	d.Tick() // cadence elapsed ⇒ owed a beat; warranted ⇒ beat delivered
	if got := f.beatLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("warranted desk must beat, got %v", got)
	}
}

// A fail-safe beat warrant (missing/torn/sectionless ledger) keeps #183's continuation beat but is
// not positive evidence of owed work. It therefore remains cap-neutral across capN+ beats: the desk
// is never escalated, stopped, or suppressed merely because it stays correctly idle.
func TestDeskHeartbeatJudgment_FailSafeBeatNeverWedges(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	f.setWarrant("backend", DeskHeartbeatFailSafeWarrant)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	d.Tick() // anchor
	for i := 0; i < 5; i++ {
		f.advance(time.Minute)
		d.Tick()
	}
	if got := f.beatLog(); len(got) != 5 {
		t.Fatalf("fail-safe warrant must preserve continuation beats across capN, got %v", got)
	}
	if got := f.escLog(); len(got) != 0 {
		t.Fatalf("no positive obligation must never wedge or escalate, got %v", got)
	}
	if d.deskNoProgress["backend"] != 0 || d.deskStopped["backend"] {
		t.Fatalf("parked desk must remain cap-neutral: noProgress=%d stopped=%v", d.deskNoProgress["backend"], d.deskStopped["backend"])
	}
}

func TestDeskHeartbeatJudgment_PaneIdleButLiveBusyNeverBeatsOrWedges(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	f.setWarrant("backend", DeskHeartbeatPositiveWarrant)
	cfg.HeartbeatLiveState = func(string) surface.State { return surface.StateWorking }
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle) // the reported misclassification
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	for i := 0; i < 5; i++ {
		f.advance(time.Minute)
		d.Tick()
	}
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("fresh live-busy join must suppress duplicate heartbeat, got %v", got)
	}
	if got := f.escLog(); len(got) != 0 {
		t.Fatalf("pane-idle alone must never wedge a live-busy desk, got %v", got)
	}
	if d.deskNoProgress["backend"] != 0 || d.deskStopped["backend"] {
		t.Fatalf("live-busy desk must be cap-neutral: noProgress=%d stopped=%v", d.deskNoProgress["backend"], d.deskStopped["backend"])
	}
}

func TestDeskHeartbeatJudgment_LiveIdleAndPositiveStillWedges(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	f.setWarrant("backend", DeskHeartbeatPositiveWarrant)
	cfg.HeartbeatLiveState = func(string) surface.State { return surface.StateIdle }
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	d.Tick()
	for i := 0; i < 3; i++ {
		f.advance(time.Minute)
		d.Tick()
	}
	if got := f.beatLog(); len(got) != 3 {
		t.Fatalf("genuinely idle positive obligation must receive capN beats, got %v", got)
	}
	if got := f.escLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("genuinely idle positive obligation must wedge exactly once, got %v", got)
	}
}

// (J0) COLD-START with the warrant WIRED: the cold-start tick still owes NO beat (the cold-start
// early-return precedes the per-desk decision), even though the warrant snapshot ran off-lock. This
// preserves the #183 no-restart-storm guarantee on the judgment-enabled path.
func TestDeskHeartbeatJudgment_ColdStartNoBeatWithWarrant(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	f.setWarranted("backend", true) // warranted, yet cold-start must still owe nothing
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg) // cold (missing snapshot)

	d.Tick()

	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("cold-start tick must owe no beat even with the warrant wired+true, got %v", got)
	}
}

// (J2) WARRANTED-FALSE SUPPRESSES the beat AND is cap- and cadence-neutral (treated like a settled
// tick): no beat, deskNoProgress unchanged, deskSinceBeat NOT advanced past the suppression.
func TestDeskHeartbeatJudgment_NotWarrantedSuppressesCapAndCadenceNeutral(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	f.setWarranted("backend", false) // legitimately idle — no live actionable work
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	d.Tick() // anchor first idle tick
	for i := 0; i < 5; i++ {
		f.advance(time.Minute)
		d.Tick() // cadence elapsed but not warranted ⇒ cadence-neutral suppress
	}
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("a not-warranted desk must NOT beat, got %v", got)
	}
	if got := f.escLog(); len(got) != 0 {
		t.Fatalf("a not-warranted desk must NOT escalate (cap-neutral, like a settled tick), got %v", got)
	}
	// Cap-neutral: deskNoProgress never advanced (a not-warranted tick delivers no beat, so no cap accrual).
	if d.deskNoProgress["backend"] != 0 {
		t.Errorf("deskNoProgress = %d, want 0 (a not-warranted idle tick accrues no cap)", d.deskNoProgress["backend"])
	}
	// Cadence-neutral: deskBeatEligibleAt is cleared — a suppressed tick is like settle.
	if _, ok := d.deskBeatEligibleAt["backend"]; ok {
		t.Error("deskBeatEligibleAt must be cleared (a not-warranted idle tick is cadence-neutral, like settle)")
	}
}

// (J3) A desk that FLIPS warranted false→true across ticks starts beating again — the judgment is a
// live per-tick decision, not a static config.
func TestDeskHeartbeatJudgment_FlipsBackToWarranted(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	f.setWarranted("backend", false)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	d.Tick()
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("not-warranted ⇒ no beat, got %v", got)
	}
	f.setWarranted("backend", true) // operator answered a question; a fresh [next] item appeared
	d.Tick()                        // anchor
	f.advance(time.Minute)
	d.Tick()
	if got := f.beatLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("flipping back to warranted must resume beats, got %v", got)
	}
}

// (J4) SEAM NIL ⇒ always-warranted ⇒ #183 byte-identical: re-run a representative #183 cadence case
// with the warrant seam UNWIRED and assert the exact #183 beat behavior.
func TestDeskHeartbeatJudgment_SeamNilIsByteIdentical(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 2, 3, false /* warrant seam UNWIRED */)
	if cfg.HeartbeatWarranted != nil {
		t.Fatal("warrant seam must be nil when unwired")
	}
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	d.Tick() // anchor first idle tick
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("tick 1 (cadence not elapsed) must not beat, got %v", got)
	}
	f.advance(2 * time.Minute) // cadence=2 ticks × 1m ref
	d.Tick()                   // period elapsed ⇒ ONE beat (exactly #183)
	if got := f.beatLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("seam nil must be #183-identical: tick 2 must beat backend once, got %v", got)
	}
	// Complete the #183 cap trace: an unwired seam retains the legacy positive default, so it wedges
	// after exactly capN beats and stops. This locks more than the first cadence edge.
	for i := 0; i < 2; i++ {
		f.advance(2 * time.Minute)
		d.Tick()
	}
	if got := f.beatLog(); len(got) != 3 {
		t.Fatalf("seam nil must preserve #183's full cap trace: got %v", got)
	}
	if got := f.escLog(); len(got) != 1 || got[0] != "backend" || !d.deskStopped["backend"] {
		t.Fatalf("seam nil must preserve #183 wedge+stop at cap: escalations=%v stopped=%v", got, d.deskStopped["backend"])
	}
	// And the warrant seam was never consulted (it's nil).
	if len(f.warrantHits) != 0 {
		t.Fatalf("an unwired warrant seam must never be consulted, got hits %v", f.warrantHits)
	}
}

// (J5) A WEDGE is NOT masked by the judgment: a desk that is warranted==true (live work) but stays
// idle across capN beats still escalates once and stops — the judgment does not interfere with the cap.
func TestDeskHeartbeatJudgment_WedgeStillEscalates(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	f.setWarranted("backend", true) // live actionable work the desk is NOT progressing and won't park
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	// cap=3: three no-progress beats ⇒ escalate ONCE on the ==3 edge, then stop.
	d.Tick() // anchor
	for i := 0; i < 3; i++ {
		f.advance(time.Minute)
		d.Tick()
	}
	beats := f.beatLog()
	if len(beats) != 3 {
		t.Fatalf("a wedge must be beaten exactly capN=3 times then stop, got %d beats: %v", len(beats), beats)
	}
	esc := f.escLog()
	if len(esc) != 1 || esc[0] != "backend" {
		t.Fatalf("a wedge must escalate exactly once, got %v", esc)
	}
}

// (J6) OFF-MUTEX INVARIANT (load-bearing): the warrant seam — which is the cmd-wiring's backlog FILE
// I/O — MUST be invoked OFF d.mu (in the phase-1 deskWarrantSnapshot, before tickLocked acquires the
// lock), never under it. This is the detector's load-bearing off-mutex invariant: a backlog read under
// d.mu would stall the tick loop and block OperatorWake (the relay goroutine).
//
// The proof is STRUCTURAL and deterministic: from inside the warrant seam we acquire d.mu via a method
// that locks it (d.deskNoProgress read under d.mu through a tiny exported-for-test re-lock). Go's
// sync.Mutex is NON-REENTRANT — if the seam ran while tickLocked already held d.mu, this re-lock would
// DEADLOCK (the test would hang and fail). It succeeds precisely because the seam runs off-lock. The
// seam doing real ReadFile is the production case; here we prove the SLOT it runs in is off-lock, which
// is the thing a regression (reading the backlog under d.mu) would break.
func TestDeskHeartbeatJudgment_WarrantSeamRunsOffLock(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	var d *Detector
	reentered := false
	cfg.HeartbeatWarranted = func(a string) DeskHeartbeatWarrant {
		f.mu.Lock()
		f.warrantHits = append(f.warrantHits, a)
		f.mu.Unlock()
		// Acquire the DETECTOR's mutex from inside the seam. If the seam ran under d.mu (the regression
		// this test guards against), this would deadlock (sync.Mutex is non-reentrant) and the test would
		// hang. It returns cleanly because the seam runs OFF-lock in deskWarrantSnapshot.
		if d != nil {
			d.mu.Lock()
			reentered = true
			d.mu.Unlock()
		}
		return DeskHeartbeatPositiveWarrant
	}
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d = f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	done := make(chan struct{})
	go func() { d.Tick(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Tick deadlocked — the warrant seam was invoked UNDER d.mu (off-mutex invariant violated)")
	}
	if !reentered {
		t.Fatal("the warrant seam re-lock did not run — the seam was not invoked off-lock as expected")
	}
	if len(f.warrantHits) == 0 {
		t.Fatal("the warrant seam was never consulted — the judgment is not wired into the decision")
	}
}

// (J8) END-TO-END integration through the REAL roster.Config.HeartbeatWarranted + backlog.Parse: a
// desk with a per-recipient backlog drives the full live loop across ticks —
//   - actionable [in-flight] item ⇒ beaten on cadence;
//   - the desk marks its last item [awaiting-auth] ⇒ next tick NOT beaten (legitimately idle,
//     cap-neutral) — the judgment is a LIVE per-recipient decision, not a static config;
//   - operator re-arm (AgentWake) + a fresh [next] item ⇒ beaten again.
//
// The warrant seam here is built EXACTLY as the cmd wiring builds it: parse a mutable per-recipient
// backlog markdown through backlog.Parse, then cfg.HeartbeatWarranted(agent, st). This proves the
// roster judgment, the backlog parser, and the detector conjunct compose correctly.
func TestDeskHeartbeatJudgment_EndToEndAcrossTicks(t *testing.T) {
	f := newHBJFixture()
	// A real roster: "backend" is an eligible (default-ON) desk; "xo" is the primary clock XO.
	rcfg := &roster.Config{
		OperatorUserID: "op", ChannelID: "C1", XOAgent: "xo",
		Agents: []roster.Agent{{Name: "xo"}, {Name: "backend"}},
	}

	// The per-recipient backlog, mutable across ticks (mimics the desk editing its own ledger file).
	var mu sync.Mutex
	backlogMD := "## Backlog\n- [in-flight] ship the feature\n" // start: live actionable work
	setBacklog := func(md string) { mu.Lock(); backlogMD = md; mu.Unlock() }

	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 99 /* high cap so the test never escalates */, false)
	// Wire the warrant seam through the REAL parser + REAL roster judgment (the production composition).
	cfg.HeartbeatWarranted = func(agent string) DeskHeartbeatWarrant {
		mu.Lock()
		md := backlogMD
		mu.Unlock()
		warranted := rcfg.HeartbeatWarranted(agent, backlog.Parse(md))
		if warranted {
			return DeskHeartbeatPositiveWarrant
		}
		return DeskHeartbeatNotWarranted
	}
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	// Phase 1: actionable work ⇒ beaten on cadence.
	d.Tick() // anchor
	f.advance(time.Minute)
	d.Tick()
	if got := f.beatLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("phase 1 (actionable work) must beat backend, got %v", got)
	}

	// Phase 2: the desk marks its only item [awaiting-auth] ⇒ no more live actionable work ⇒ NOT beaten.
	setBacklog("## Backlog\n- [awaiting-auth] flip the feed @operator\n")
	for i := 0; i < 4; i++ {
		d.Tick()
	}
	if got := f.beatLog(); len(got) != 1 {
		t.Fatalf("phase 2 (all awaiting-auth) must NOT beat again — the desk is legitimately idle, got %v", got)
	}
	if got := f.escLog(); len(got) != 0 {
		t.Fatalf("phase 2 must be cap-neutral (a parked desk is not a wedge), got escalations %v", got)
	}

	// Phase 3: operator re-arms the desk (AgentWake) AND a fresh [next] item appears ⇒ beaten again.
	setBacklog("## Backlog\n- [awaiting-auth] flip the feed @operator\n- [next] start the next chunk\n")
	d.AgentWake("backend")
	d.Tick() // anchor
	f.advance(time.Minute)
	d.Tick()
	if got := f.beatLog(); len(got) != 2 || got[1] != "backend" {
		t.Fatalf("phase 3 (re-arm + fresh actionable work) must beat backend again, got %v", got)
	}
}

// (J7) The HARD gate is checked BEFORE the warrant seam: an opted-out (HeartbeatEnabled=false) desk
// is never even CONSULTED for warrant, and never beaten — the judgment cannot resurrect it.
func TestDeskHeartbeatJudgment_HardGateShortCircuitsWarrant(t *testing.T) {
	f := newHBJFixture()
	// "trader" present but NOT enabled (approval-sensitive default-off). It is warranted-true on paper.
	cfg := f.config("xo", []string{"xo", "backend", "trader"}, []string{"backend"}, 1, 3, true)
	f.setWarranted("trader", true)
	f.setWarranted("backend", true)
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateWorking) // keep backend busy so only trader is in question
	f.set("trader", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking, "trader": surface.StateIdle}, "h0")

	for i := 0; i < 4; i++ {
		d.Tick()
	}
	for _, a := range f.beatLog() {
		if a == "trader" {
			t.Fatalf("an opted-out desk must never beat even when warranted, got %v", f.beatLog())
		}
	}
	// The HARD gate runs FIRST (continue before the warrant), so trader's warrant is never consulted.
	for _, a := range f.warrantHits {
		if a == "trader" {
			t.Fatalf("an opted-out desk's warrant must never be consulted (HARD gate short-circuits), got hits %v", f.warrantHits)
		}
	}
}
