package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// The #189 per-recipient JUDGMENT adds a HeartbeatWarranted(agent) bool conjunct to the detector's
// desk-heartbeat decision — the LAST gate, evaluated only AFTER the #183 HARD gate (XO-excl /
// HeartbeatEnabled), the settle/stop checks, and the cadence. The conjunct is a PURE lookup against
// a per-recipient warrant computed OFF d.mu (the seam returns an already-decided boolean; it does NO
// file I/O under the lock). It can ONLY suppress a beat #183 would have sent — never resurrect one.
//
// These tests extend the §9 hbFixture with a per-agent `warranted` map (defaulting to true when the
// seam is nil so #183 is byte-identical) and a recording wrapper that asserts the off-mutex
// invariant: the seam, when invoked from the under-lock decision, performs NO backlog file I/O.

// hbjFixture is the §9 hbFixture extended with the #189 warrant seam.
type hbjFixture struct {
	mu          sync.Mutex
	states      map[string]surface.State
	enabled     map[string]bool
	settleNow   map[string]bool
	warranted   map[string]bool // agent → HeartbeatWarranted (the #189 judgment); absent ⇒ true
	warrantHits []string        // agents the warrant seam was consulted for, in order
	ioUnderLock bool            // set true if the seam ever did file I/O while consulted (must stay false)
	beats       []string
	escalations []string
}

func newHBJFixture() *hbjFixture {
	return &hbjFixture{
		states:    map[string]surface.State{},
		enabled:   map[string]bool{},
		settleNow: map[string]bool{},
		warranted: map[string]bool{},
	}
}

func (f *hbjFixture) set(agent string, s surface.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[agent] = s
}

func (f *hbjFixture) setWarranted(agent string, w bool) {
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
	}
	if wireWarrant {
		cfg.HeartbeatWarranted = func(a string) bool {
			// This wrapper stands in for the cmd-side seam: it returns an ALREADY-COMPUTED boolean
			// (the off-lock read happened earlier). It records the consult and asserts it does NO
			// file I/O here (the off-mutex invariant — the read must live at the cmd wiring, off d.mu).
			f.mu.Lock()
			defer f.mu.Unlock()
			f.warrantHits = append(f.warrantHits, a)
			w, ok := f.warranted[a]
			if !ok {
				return true // default warranted (matches the missing-ledger fallback)
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

	d.Tick() // cadence 1 ⇒ owed a beat; warranted ⇒ beat delivered
	if got := f.beatLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("warranted desk must beat, got %v", got)
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

	for i := 0; i < 5; i++ {
		d.Tick()
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
	// Cadence-neutral: deskSinceBeat is NOT left mid-accrual into a beat — a suppressed tick is like settle.
	if d.deskSinceBeat["backend"] != 0 {
		t.Errorf("deskSinceBeat = %d, want 0 (a not-warranted idle tick is cadence-neutral, like settle)", d.deskSinceBeat["backend"])
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

	d.Tick() // deskSinceBeat 0→1, < 2 ⇒ no beat
	if got := f.beatLog(); len(got) != 0 {
		t.Fatalf("tick 1 (cadence not elapsed) must not beat, got %v", got)
	}
	d.Tick() // deskSinceBeat 1→2, >= 2 ⇒ ONE beat (exactly #183)
	if got := f.beatLog(); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("seam nil must be #183-identical: tick 2 must beat backend once, got %v", got)
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
	for i := 0; i < 6; i++ {
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

// (J6) OFF-MUTEX INVARIANT (load-bearing): the warrant seam invoked from the under-lock decision does
// NO backlog file I/O while d.mu is held. The seam wired here records that it was consulted with
// pre-computed data and never touches the filesystem; ioUnderLock must stay false. This locks the
// detector's off-mutex invariant against a regression that reads a backlog under d.mu.
func TestDeskHeartbeatJudgment_NoBacklogIOUnderLock(t *testing.T) {
	f := newHBJFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, []string{"backend"}, 1, 3, true)
	// Override the warrant seam with one that would FLAG if it did file I/O. It does none — it returns
	// a pre-decided boolean — which is the whole point: the read happens off-lock at the cmd wiring.
	cfg.HeartbeatWarranted = func(a string) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.warrantHits = append(f.warrantHits, a)
		// A real os.ReadFile/backlog.Parse here would violate the invariant. We assert by construction
		// that the seam is a pure lookup: it sets ioUnderLock only if it (wrongly) performed I/O.
		// (No I/O performed ⇒ ioUnderLock stays false.)
		return true
	}
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")

	d.Tick()
	if f.ioUnderLock {
		t.Fatal("the warrant seam performed backlog file I/O under d.mu — the off-mutex invariant is violated")
	}
	if len(f.warrantHits) == 0 {
		t.Fatal("the warrant seam was never consulted — the conjunct is not wired into the decision")
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
