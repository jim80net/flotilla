package watch

import (
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// ActivityLevel is the fleet coordination activity tier derived from tick assess
// snapshots and turn-end / operator signals. Consumed by AdaptiveInterval (PR 3).
type ActivityLevel int

const (
	ActivityIdle ActivityLevel = iota
	ActivityWarm
	ActivityActive
)

// ActivitySnapshot is a point-in-time view of fleet activity for adaptive policy.
type ActivitySnapshot struct {
	Level          ActivityLevel
	WorkingDesks   int
	XOWorking      bool
	XOSettled      bool
	LastTurnEnd    time.Time
	LastOperatorAt time.Time
	LastIngestAt   time.Time // wall time of the most recent OnTickIngest (zero = never)
	ObservedAt     time.Time // query time passed to Snapshot(now)
}

// ActivityTracker ingests detector observations. NO pane I/O.
type ActivityTracker interface {
	// OnTickIngest is pure; called OFF d.mu with a copy of debounced DeskStates.
	OnTickIngest(observedAt time.Time, xoAgent string, states map[string]surface.State, xoSettled bool)
	OnTurnEnd(agent string, at time.Time) // poller poke + tick-diff W→I
	OnOperatorActivity(at time.Time)
	Snapshot(now time.Time) ActivitySnapshot
}

// ActivityConfig tunes warm/operator retention windows for Snapshot level derivation.
type ActivityConfig struct {
	WarmRetention     time.Duration // default 10m
	OperatorRetention time.Duration // default 5m
}

// DefaultActivityConfig returns design-default retention windows.
func DefaultActivityConfig() ActivityConfig {
	return ActivityConfig{
		WarmRetention:     10 * time.Minute,
		OperatorRetention: 5 * time.Minute,
	}
}

type activityTracker struct {
	mu  sync.Mutex
	cfg ActivityConfig
	obs activityObs
}

type activityObs struct {
	workingDesks   int
	xoWorking      bool
	xoSettled      bool
	lastTurnEnd    time.Time
	lastOperatorAt time.Time
	lastIngestAt   time.Time
}

// NewActivityTracker builds an in-memory activity tracker (no pane I/O).
func NewActivityTracker(cfg ActivityConfig) ActivityTracker {
	if cfg.WarmRetention <= 0 {
		cfg.WarmRetention = DefaultActivityConfig().WarmRetention
	}
	if cfg.OperatorRetention <= 0 {
		cfg.OperatorRetention = DefaultActivityConfig().OperatorRetention
	}
	return &activityTracker{cfg: cfg}
}

func (t *activityTracker) OnTickIngest(observedAt time.Time, xoAgent string, states map[string]surface.State, xoSettled bool) {
	var working int
	var xoWorking bool
	for name, st := range states {
		if name == xoAgent {
			xoWorking = st == surface.StateWorking
			continue
		}
		if st == surface.StateWorking {
			working++
		}
	}
	t.mu.Lock()
	t.obs.workingDesks = working
	t.obs.xoWorking = xoWorking
	t.obs.xoSettled = xoSettled
	t.obs.lastIngestAt = observedAt
	t.mu.Unlock()
}

func (t *activityTracker) OnTurnEnd(_ string, at time.Time) {
	t.mu.Lock()
	if at.After(t.obs.lastTurnEnd) {
		t.obs.lastTurnEnd = at
	}
	t.mu.Unlock()
}

func (t *activityTracker) OnOperatorActivity(at time.Time) {
	t.mu.Lock()
	if at.After(t.obs.lastOperatorAt) {
		t.obs.lastOperatorAt = at
	}
	t.mu.Unlock()
}

func (t *activityTracker) Snapshot(now time.Time) ActivitySnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	snap := ActivitySnapshot{
		Level:          t.levelLocked(now),
		WorkingDesks:   t.obs.workingDesks,
		XOWorking:      t.obs.xoWorking,
		XOSettled:      t.obs.xoSettled,
		LastTurnEnd:    t.obs.lastTurnEnd,
		LastOperatorAt: t.obs.lastOperatorAt,
		LastIngestAt:   t.obs.lastIngestAt,
		ObservedAt:     now,
	}
	return snap
}

// levelLocked derives the activity tier from ingested state.
//
// Cold-start fail-safe: before the first OnTickIngest, xoSettled is false (zero
// value), so level is ActivityActive. A coordination daemon must bias toward fast
// polling until a tick proves the fleet is quiet — starting at Idle would mean a
// slow ceiling tick on boot, which is the unsafe direction.
func (t *activityTracker) levelLocked(now time.Time) ActivityLevel {
	if t.obs.workingDesks > 0 || t.obs.xoWorking || !t.obs.xoSettled {
		return ActivityActive
	}
	if !t.obs.lastTurnEnd.IsZero() && now.Sub(t.obs.lastTurnEnd) <= t.cfg.WarmRetention {
		return ActivityWarm
	}
	if !t.obs.lastOperatorAt.IsZero() && now.Sub(t.obs.lastOperatorAt) <= t.cfg.OperatorRetention {
		return ActivityWarm
	}
	return ActivityIdle
}
