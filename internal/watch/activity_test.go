package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func testActivityConfig() ActivityConfig {
	return ActivityConfig{
		WarmRetention:     10 * time.Minute,
		OperatorRetention: 5 * time.Minute,
	}
}

func TestActivityWorkingDeskElevatesActive(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	at.OnTickIngest(now, "xo", map[string]surface.State{
		"xo":      surface.StateIdle,
		"backend": surface.StateWorking,
	}, true)
	snap := at.Snapshot(now)
	if snap.Level != ActivityActive || snap.WorkingDesks != 1 {
		t.Fatalf("working desk ⇒ Active, got level=%v working=%d", snap.Level, snap.WorkingDesks)
	}
}

func TestActivityXOWorkingElevatesActive(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	at.OnTickIngest(now, "xo", map[string]surface.State{"xo": surface.StateWorking}, true)
	if at.Snapshot(now).Level != ActivityActive {
		t.Fatalf("XO working ⇒ Active, got %v", at.Snapshot(now).Level)
	}
}

func TestActivityXOUnsettledElevatesActive(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	at.OnTickIngest(now, "xo", map[string]surface.State{
		"xo":      surface.StateIdle,
		"backend": surface.StateIdle,
	}, false)
	if at.Snapshot(now).Level != ActivityActive {
		t.Fatalf("XO unsettled ⇒ Active, got %v", at.Snapshot(now).Level)
	}
}

func TestActivitySnapshotExposesStaleIngestAge(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	ingestAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	queryAt := ingestAt.Add(30 * time.Minute)
	at.OnTickIngest(ingestAt, "xo", map[string]surface.State{
		"xo": surface.StateIdle,
	}, true)
	snap := at.Snapshot(queryAt)
	if snap.ObservedAt != queryAt {
		t.Fatalf("ObservedAt = %v, want query time %v", snap.ObservedAt, queryAt)
	}
	if snap.LastIngestAt != ingestAt {
		t.Fatalf("LastIngestAt = %v, want ingest time %v (true signal age)", snap.LastIngestAt, ingestAt)
	}
	if snap.ObservedAt.Sub(snap.LastIngestAt) != 30*time.Minute {
		t.Fatalf("consumers can derive ingest staleness: ObservedAt-LastIngestAt = %v, want 30m",
			snap.ObservedAt.Sub(snap.LastIngestAt))
	}
}

func TestActivityColdStartFailSafeActive(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	snap := at.Snapshot(now)
	if snap.Level != ActivityActive {
		t.Fatalf("cold-start (no ingest yet) must be Active fail-safe, got %v", snap.Level)
	}
	if !snap.LastIngestAt.IsZero() {
		t.Fatalf("cold-start LastIngestAt must be zero, got %v", snap.LastIngestAt)
	}
}

func TestActivityIdleFleetSettled(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	at.OnTickIngest(now, "xo", map[string]surface.State{
		"xo":      surface.StateIdle,
		"backend": surface.StateIdle,
	}, true)
	if at.Snapshot(now).Level != ActivityIdle {
		t.Fatalf("idle settled fleet ⇒ Idle, got %v", at.Snapshot(now).Level)
	}
}

func TestActivityTurnEndExtendsWarmWindow(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	at.OnTickIngest(now, "xo", map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, true)
	at.OnTurnEnd("backend", now)
	later := now.Add(5 * time.Minute)
	if at.Snapshot(later).Level != ActivityWarm {
		t.Fatalf("recent turn-end ⇒ Warm, got %v", at.Snapshot(later).Level)
	}
	expired := now.Add(11 * time.Minute)
	if at.Snapshot(expired).Level != ActivityIdle {
		t.Fatalf("expired turn-end window ⇒ Idle, got %v", at.Snapshot(expired).Level)
	}
}

func TestActivityOperatorExtendsWarmWindow(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	at.OnTickIngest(now, "xo", map[string]surface.State{"xo": surface.StateIdle}, true)
	at.OnOperatorActivity(now)
	later := now.Add(3 * time.Minute)
	if at.Snapshot(later).Level != ActivityWarm {
		t.Fatalf("recent operator activity ⇒ Warm, got %v", at.Snapshot(later).Level)
	}
	expired := now.Add(6 * time.Minute)
	if at.Snapshot(expired).Level != ActivityIdle {
		t.Fatalf("expired operator window ⇒ Idle, got %v", at.Snapshot(expired).Level)
	}
}

func TestActivityActiveOverridesWarm(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	at.OnTurnEnd("backend", now)
	at.OnTickIngest(now, "xo", map[string]surface.State{"backend": surface.StateWorking}, true)
	if at.Snapshot(now).Level != ActivityActive {
		t.Fatalf("working desk must dominate warm turn-end, got %v", at.Snapshot(now).Level)
	}
}

func TestDetectorActivityIngestOffLock(t *testing.T) {
	inner := NewActivityTracker(testActivityConfig())
	var d *Detector
	reentered := false
	probe := &activityIngestProbe{inner: inner, relock: func(det *Detector) {
		det.mu.Lock()
		reentered = true
		det.mu.Unlock()
	}}
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.Activity = probe
	cfg.Now = func() time.Time { return time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC) }
	d = newDet(t, f, cfg)
	probe.d = d
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateIdle}, "h0")
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateIdle)

	done := make(chan struct{})
	go func() { d.Tick(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Tick deadlocked — activity ingest must run OFF d.mu")
	}
	if !reentered {
		t.Fatal("OnTickIngest must run OFF d.mu (re-lock from inside ingest succeeded)")
	}
}

type activityIngestProbe struct {
	inner  ActivityTracker
	d      *Detector
	relock func(*Detector)
}

func (p *activityIngestProbe) OnTickIngest(at time.Time, xo string, states map[string]surface.State, settled bool) {
	if p.relock != nil && p.d != nil {
		p.relock(p.d)
	}
	p.inner.OnTickIngest(at, xo, states, settled)
}
func (p *activityIngestProbe) OnTurnEnd(agent string, at time.Time) { p.inner.OnTurnEnd(agent, at) }
func (p *activityIngestProbe) OnOperatorActivity(at time.Time)      { p.inner.OnOperatorActivity(at) }
func (p *activityIngestProbe) Snapshot(now time.Time) ActivitySnapshot {
	return p.inner.Snapshot(now)
}

func TestDetectorActivityTickDiffTurnEnd(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.Activity = at
	cfg.Now = func() time.Time { return now }
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	d.snap.XOSettled = true
	f.set("backend", surface.StateIdle)
	d.Tick()
	snap := at.Snapshot(now)
	if snap.LastTurnEnd != now {
		t.Fatalf("tick-diff W→I must record turn-end at %v, got %v", now, snap.LastTurnEnd)
	}
	// Material desk transition clears XO settled this tick, so Active dominates Warm.
	if snap.Level != ActivityActive {
		t.Fatalf("material desk finish re-engages XO ⇒ Active, got %v", snap.Level)
	}
	// Once the fleet is idle+settled again, the recorded turn-end sustains Warm.
	at.OnTickIngest(now, "xo", map[string]surface.State{
		"xo": surface.StateIdle, "backend": surface.StateIdle,
	}, true)
	if at.Snapshot(now).Level != ActivityWarm {
		t.Fatalf("recorded turn-end must sustain Warm on idle+settled fleet, got %v", at.Snapshot(now).Level)
	}
}

func TestDetectorActivityOperatorWake(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	cfg.Activity = at
	cfg.Now = func() time.Time { return now }
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	d.snap.XOSettled = true
	d.OperatorWake()
	if at.Snapshot(now).LastOperatorAt != now {
		t.Fatalf("OperatorWake must record operator activity, got %+v", at.Snapshot(now))
	}
}

func TestDetectorActivityNilByteInert(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.Activity = nil
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	f.set("backend", surface.StateIdle)
	d.Tick() // must not panic with nil Activity
	d.OperatorWake()
}

func TestDetectorActivityConcurrentTickOperatorWake(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.Activity = at
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
	wg.Wait()
}

func TestDetectorActivityAgentWakeRecordsOperator(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.Activity = at
	cfg.Now = func() time.Time { return now }
	d := newDet(t, f, cfg)
	d.AgentWake("backend")
	if at.Snapshot(now).LastOperatorAt != now {
		t.Fatalf("AgentWake must record operator activity, got %+v", at.Snapshot(now))
	}
}

func TestDetectorActivityColdStartIngests(t *testing.T) {
	at := NewActivityTracker(testActivityConfig())
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.Activity = at
	cfg.Now = func() time.Time { return now }
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateWorking)
	d.Tick()
	snap := at.Snapshot(now)
	if snap.Level != ActivityActive || snap.WorkingDesks != 1 {
		t.Fatalf("cold-start tick must ingest assess snapshot, got %+v", snap)
	}
}
