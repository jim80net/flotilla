package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// synthFixture extends the detector test rig with the synthesis seams (WakeAgent,
// SynthParents, SynthRead, SynthEveryTicks + a disk sidecar path). It deliberately
// mirrors detFixture's minimal-collaborator style so every synthesis test runs
// without tmux, a clock, or transcript files. The base Wake stays a plain no-op so
// the shipped primary-XO path is exercised byte-identically alongside synthesis.
type synthFixture struct {
	mu          sync.Mutex
	states      map[string]surface.State
	parents     map[string][]string // agent → AgentsAbove(agent)
	subText     map[string]string   // subordinate agent → its latest turn text
	subReadable map[string]bool     // subordinate agent → pane resolves (default true)
	agentWakes  []agentWakeRec      // WakeAgent invocations
	sidecarPath string
}

type agentWakeRec struct {
	agent   string
	kind    WakeKind
	reasons []string
}

func newSynthFixture(t *testing.T) *synthFixture {
	t.Helper()
	return &synthFixture{
		states:      map[string]surface.State{},
		parents:     map[string][]string{},
		subText:     map[string]string{},
		subReadable: map[string]bool{},
		sidecarPath: filepath.Join(t.TempDir(), "flotilla-synthesis-state.json"),
	}
}

func (f *synthFixture) set(agent string, s surface.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[agent] = s
}

func (f *synthFixture) setSub(agent, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subText[agent] = text
	f.subReadable[agent] = true
}

func (f *synthFixture) setUnreadable(agent string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subReadable[agent] = false
}

func (f *synthFixture) config(xo string, desks []string) DetectorConfig {
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
		AckAge:          func() time.Duration { return 0 },
		Wake:            func(WakeKind, []string) {},
		Persist:         func(Snapshot) error { return nil },
		SynthEveryTicks: 1, // tests pin a tight cadence unless they override
		SynthParents: func(a string) []string {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.parents[a]
		},
		SynthRead: func(a string) (string, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if readable, ok := f.subReadable[a]; ok && !readable {
				return "", false
			}
			return f.subText[a], true
		},
		WakeAgent: func(agent string, kind WakeKind, reasons []string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.agentWakes = append(f.agentWakes, agentWakeRec{agent, kind, reasons})
		},
	}
	return cfg
}

func (f *synthFixture) newDet(t *testing.T, cfg DetectorConfig) *Detector {
	t.Helper()
	return NewDetectorWithSynthSidecar(cfg, filepath.Join(t.TempDir(), "missing.json"), f.sidecarPath)
}

func (f *synthFixture) synthWakes() []agentWakeRec {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []agentWakeRec
	for _, w := range f.agentWakes {
		if w.kind == WakeSynthesis {
			out = append(out, w)
		}
	}
	return out
}

// §5.1 — a boat-finish (Working→Idle, non-XO) marks synthesis owed for its parent(s), keyed
// per synthesizing agent, and fires WakeSynthesis to that parent.
func TestSynthesisOwedOnDeskFinish(t *testing.T) {
	f := newSynthFixture(t)
	cfg := f.config("hydra-ops", []string{"hydra-ops", "family-office", "v12-dev"})
	f.parents["v12-dev"] = []string{"family-office"}
	f.setSub("v12-dev", "built the thing")
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")

	d.Tick()

	got := f.synthWakes()
	if len(got) != 1 || got[0].agent != "family-office" {
		t.Fatalf("expected one synthesis wake to family-office, got %+v", got)
	}
}

// §5.6 — a boat that is a member of TWO channels marks BOTH parents owed.
func TestSynthesisOwedMarksBothParents(t *testing.T) {
	f := newSynthFixture(t)
	cfg := f.config("hydra-ops", []string{"hydra-ops", "p1", "p2", "boat"})
	f.parents["boat"] = []string{"p1", "p2"}
	f.setSub("boat", "did work")
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "p1": surface.StateIdle, "p2": surface.StateIdle, "boat": surface.StateWorking}, "h0")

	d.Tick()

	got := f.synthWakes()
	if len(got) != 2 {
		t.Fatalf("expected two synthesis wakes (both parents), got %+v", got)
	}
	seen := map[string]bool{}
	for _, w := range got {
		seen[w.agent] = true
	}
	if !seen["p1"] || !seen["p2"] {
		t.Errorf("both p1 and p2 must be woken, got %+v", got)
	}
}

// §5.2 — debounce-up: a burst of finishes within the digest sub-cadence coalesces to ONE wake
// per synthesizing agent.
func TestSynthesisDebouncesBurstToOneWake(t *testing.T) {
	f := newSynthFixture(t)
	cfg := f.config("hydra-ops", []string{"hydra-ops", "family-office", "a", "b"})
	cfg.SynthEveryTicks = 3 // a wake at most once per 3 ticks per agent
	f.parents["a"] = []string{"family-office"}
	f.parents["b"] = []string{"family-office"}
	f.setSub("a", "ta")
	f.setSub("b", "tb")
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "a": surface.StateWorking, "b": surface.StateWorking}, "h0")

	// Tick 1: a finishes → owed, fires.
	f.set("hydra-ops", surface.StateIdle)
	f.set("family-office", surface.StateIdle)
	f.set("a", surface.StateIdle)
	f.set("b", surface.StateWorking)
	d.Tick()
	// Tick 2: b finishes (still inside the 3-tick window, and a's text unchanged) → coalesced, no new wake.
	f.set("b", surface.StateIdle)
	f.setSub("b", "tb2") // b changed so materiality would allow, but cadence suppresses
	d.Tick()

	if got := f.synthWakes(); len(got) != 1 {
		t.Fatalf("burst within the cadence window must coalesce to ONE wake, got %d: %+v", len(got), got)
	}
}

// §5.3 — with no synthesis owed, no WakeSynthesis fires (idle $0; the primary-XO Wake path
// stays byte-identical).
func TestSynthesisIdleFleetFiresNothing(t *testing.T) {
	f := newSynthFixture(t)
	cfg := f.config("hydra-ops", []string{"hydra-ops", "family-office", "v12-dev"})
	f.parents["v12-dev"] = []string{"family-office"}
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "v12-dev": surface.StateIdle}, "h0")

	d.Tick()

	if got := f.synthWakes(); len(got) != 0 {
		t.Errorf("idle fleet (nothing owed) must fire no synthesis wake, got %+v", got)
	}
}

// §5.4 (P1 regression) — the materiality READ (SynthRead = blocking tmux + transcript I/O in
// production) runs OUTSIDE d.mu: OperatorWake returns while a synthesis read is parked. This is the
// regression lock for the implement-gate P1 — against the pre-fix code (the read ran under d.mu in
// decideSynthesis) this test would HANG, blocking the relay goroutine. It exercises the READ, where
// the older TestSynthesisWakeTargetsArbitraryAgentOutsideMutex only exercised the DELIVERY.
func TestSynthesisMaterialityReadRunsOutsideMutex(t *testing.T) {
	f := newSynthFixture(t)
	cfg := f.config("hydra-ops", []string{"hydra-ops", "family-office", "v12-dev"})
	f.parents["v12-dev"] = []string{"family-office"}
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }

	// A blocking SynthRead parks the materiality read; if it ran under d.mu, OperatorWake would block.
	reading := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	cfg.SynthRead = func(a string) (string, bool) {
		once.Do(func() { close(reading) })
		<-release
		return "text", true
	}
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")

	tickDone := make(chan struct{})
	go func() { d.Tick(); close(tickDone) }()
	<-reading // parked inside the blocked materiality read (off d.mu, in runSynthesis)

	woke := make(chan struct{})
	go func() { d.OperatorWake(); close(woke) }()
	select {
	case <-woke:
	case <-time.After(2 * time.Second):
		t.Fatal("OperatorWake blocked behind a synthesis materiality READ — it is being held under d.mu (P1)")
	}
	close(release)
	<-tickDone
}

// §5.4 — the synthesis wake DELIVERY runs OUTSIDE d.mu (in runSynthesis), and targets the
// SYNTHESIZING agent which differs from d.cfg.XOAgent.
func TestSynthesisWakeTargetsArbitraryAgentOutsideMutex(t *testing.T) {
	f := newSynthFixture(t)
	cfg := f.config("hydra-ops", []string{"hydra-ops", "family-office", "v12-dev"})
	f.parents["v12-dev"] = []string{"family-office"} // family-office != hydra-ops (the primary XO)
	f.setSub("v12-dev", "x")
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }

	// A blocking WakeAgent proves the side-effect runs outside d.mu (OperatorWake returns while parked).
	waking := make(chan struct{})
	release := make(chan struct{})
	cfg.WakeAgent = func(agent string, kind WakeKind, reasons []string) {
		if kind == WakeSynthesis {
			if agent != "family-office" {
				t.Errorf("synthesis wake must target the synthesizing agent family-office, got %q", agent)
			}
			close(waking)
			<-release
		}
	}
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")

	tickDone := make(chan struct{})
	go func() { d.Tick(); close(tickDone) }()
	<-waking // parked inside the blocked synthesis wake, in runTail

	woke := make(chan struct{})
	go func() { d.OperatorWake(); close(woke) }()
	select {
	case <-woke:
	case <-time.After(2 * time.Second):
		t.Fatal("OperatorWake blocked behind a synthesis wake — it is being held under d.mu")
	}
	close(release)
	<-tickDone
}

// §5.5 default-inert: with no synthesis seams configured (the shipped detector), no synthesis
// wake EVER fires and behavior is byte-identical. A boat finish still mirrors / wakes as before.
func TestSynthesisDefaultInert(t *testing.T) {
	f := newFixture()
	cfg := f.config("hydra-ops", []string{"hydra-ops", "v12-dev"}, 3, "none")
	// No WakeAgent / SynthParents / SynthRead set → inert.
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")
	f.set("hydra-ops", surface.StateIdle)
	f.set("v12-dev", surface.StateIdle)
	f.signal = "h0"

	d.Tick() // must not panic; the WakeMaterial path is unchanged
	if f.wakeCount() != 1 || f.lastWake().kind != WakeMaterial {
		t.Fatalf("inert synthesis must not change the shipped desk-finish wake, got %+v", f.wakes)
	}
}

// §5.5 separation guard — with the synthesis seams WIRED, a primary-XO wake (e.g. a desk-finish
// WakeMaterial, which targets the XO) still routes through the shipped Wake seam (agent=""), NEVER
// through WakeAgent. The two paths are parallel: synthesis never hijacks the primary-XO clock.
func TestSynthesisDoesNotHijackPrimaryWake(t *testing.T) {
	f := newSynthFixture(t)
	var primaryWakes int
	var pmu sync.Mutex
	cfg := f.config("hydra-ops", []string{"hydra-ops", "family-office", "v12-dev"})
	cfg.Wake = func(WakeKind, []string) { pmu.Lock(); primaryWakes++; pmu.Unlock() }
	// v12-dev finishing is BOTH a desk-finish (WakeMaterial → primary XO via Wake) AND a synthesis
	// owe (→ family-office via WakeAgent). Both must fire on their OWN seam.
	f.parents["v12-dev"] = []string{"family-office"}
	f.setSub("v12-dev", "x")
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")

	d.Tick()

	pmu.Lock()
	defer pmu.Unlock()
	if primaryWakes != 1 {
		t.Errorf("the desk-finish material wake must still fire through the primary Wake seam exactly once, got %d", primaryWakes)
	}
	if got := f.synthWakes(); len(got) != 1 || got[0].agent != "family-office" {
		t.Errorf("the synthesis wake must fire through WakeAgent to family-office, got %+v", got)
	}
}

// §6.1 — materiality: a fired wake whose subordinates have NOT changed since last synthesis is
// suppressed (no re-post / no wake).
func TestSynthesisMaterialitySuppressesUnchanged(t *testing.T) {
	f := newSynthFixture(t)
	cfg := f.config("hydra-ops", []string{"hydra-ops", "family-office", "v12-dev"})
	cfg.SynthEveryTicks = 1
	f.parents["v12-dev"] = []string{"family-office"}
	f.setSub("v12-dev", "stable text")
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")

	// Tick 1: v12-dev finishes with "stable text" → first synthesis (everything new) fires.
	f.set("hydra-ops", surface.StateIdle)
	f.set("family-office", surface.StateIdle)
	f.set("v12-dev", surface.StateIdle)
	d.Tick()
	if got := f.synthWakes(); len(got) != 1 {
		t.Fatalf("first synthesis should fire, got %+v", got)
	}

	// Tick 2: v12-dev finishes AGAIN but its latest text is UNCHANGED → materiality suppresses.
	f.set("v12-dev", surface.StateWorking)
	d.Tick()
	f.set("v12-dev", surface.StateIdle)
	d.Tick()
	if got := f.synthWakes(); len(got) != 1 {
		t.Fatalf("unchanged subordinate must not re-fire synthesis, got %d: %+v", len(got), got)
	}

	// Tick 3: v12-dev's text CHANGES → materiality allows a new synthesis.
	f.set("v12-dev", surface.StateWorking)
	d.Tick()
	f.setSub("v12-dev", "new text")
	f.set("v12-dev", surface.StateIdle)
	d.Tick()
	if got := f.synthWakes(); len(got) != 2 {
		t.Fatalf("a changed subordinate must re-fire synthesis, got %d: %+v", len(got), got)
	}
}

// §6.4 — the last-seen materiality state is a DISK SIDECAR surviving a daemon restart: after a
// restart with unchanged subordinates, NO synthesis re-fires (no restart-storm). A fresh detector
// over the SAME sidecar path simulates the restart.
func TestSynthesisMaterialitySurvivesRestart(t *testing.T) {
	f := newSynthFixture(t)
	f.parents["v12-dev"] = []string{"family-office"}
	f.setSub("v12-dev", "stable text")

	// Detector A: first synthesis persists the last-seen sidecar.
	cfgA := f.config("hydra-ops", []string{"hydra-ops", "family-office", "v12-dev"})
	dA := f.newDet(t, cfgA)
	seed(dA, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")
	f.set("hydra-ops", surface.StateIdle)
	f.set("family-office", surface.StateIdle)
	f.set("v12-dev", surface.StateIdle)
	dA.Tick()
	if got := f.synthWakes(); len(got) != 1 {
		t.Fatalf("first synthesis should fire, got %+v", got)
	}

	// Detector B: a fresh detector over the SAME sidecar path (the restart). Subordinate unchanged.
	cfgB := f.config("hydra-ops", []string{"hydra-ops", "family-office", "v12-dev"})
	dB := f.newDet(t, cfgB)
	seed(dB, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")
	f.set("v12-dev", surface.StateWorking)
	dB.Tick()
	f.set("v12-dev", surface.StateIdle)
	dB.Tick()
	if got := f.synthWakes(); len(got) != 1 {
		t.Fatalf("after restart, an unchanged subordinate must NOT re-fire synthesis (restart-storm), got %d: %+v", len(got), got)
	}
}

// §6.5 — an UNREADABLE subordinate (pane won't resolve) is EXCLUDED from the materiality hash for
// that wake (never hashed as empty), so a transient resolve failure neither flaps nor suppresses.
func TestSynthesisMaterialityExcludesUnreadableSubordinate(t *testing.T) {
	f := newSynthFixture(t)
	cfg := f.config("hydra-ops", []string{"hydra-ops", "family-office", "sub1", "sub2"})
	cfg.SynthEveryTicks = 1
	f.parents["sub1"] = []string{"family-office"}
	f.parents["sub2"] = []string{"family-office"}
	f.setSub("sub1", "sub1 stable")
	f.setSub("sub2", "sub2 stable")
	d := f.newDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "sub1": surface.StateWorking, "sub2": surface.StateIdle}, "h0")

	// Tick 1: sub1 finishes → first synthesis (both readable) fires + records both hashes.
	f.set("hydra-ops", surface.StateIdle)
	f.set("family-office", surface.StateIdle)
	f.set("sub1", surface.StateIdle)
	d.Tick()
	if got := f.synthWakes(); len(got) != 1 {
		t.Fatalf("first synthesis should fire, got %+v", got)
	}

	// Tick 2: sub2 becomes UNREADABLE and sub1 unchanged. A finish re-owes family-office. The
	// unreadable sub2 must be EXCLUDED (not hashed as "" → "changed to empty"), and sub1 is
	// unchanged → no material change → no re-fire (no flap).
	f.setUnreadable("sub2")
	f.set("sub1", surface.StateWorking)
	d.Tick()
	f.set("sub1", surface.StateIdle)
	d.Tick()
	if got := f.synthWakes(); len(got) != 1 {
		t.Fatalf("an unreadable subordinate must not flap the wake (change-to-empty), got %d: %+v", len(got), got)
	}
}

// §6.2 / §6.4 — a missing/corrupt sidecar fails SAFE toward "all changed" (synthesize once),
// never silent-never-fire. A brand-new detector (no sidecar) fires on the first owed wake.
func TestSynthesisMissingSidecarFiresOnce(t *testing.T) {
	f := newSynthFixture(t)
	cfg := f.config("hydra-ops", []string{"hydra-ops", "family-office", "v12-dev"})
	f.parents["v12-dev"] = []string{"family-office"}
	f.setSub("v12-dev", "anything")
	d := f.newDet(t, cfg) // sidecar path does not exist yet
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "family-office": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")
	f.set("hydra-ops", surface.StateIdle)
	f.set("family-office", surface.StateIdle)
	f.set("v12-dev", surface.StateIdle)

	d.Tick()
	if got := f.synthWakes(); len(got) != 1 {
		t.Fatalf("missing sidecar must fail safe to synthesize once, got %+v", got)
	}
}
