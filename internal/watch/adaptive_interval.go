package watch

import (
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// AdaptiveIntervalEnabled reports whether the fleet-wide adaptive detector tick is on.
// DEFAULT ON at GA; disable explicitly with FLOTILLA_ADAPTIVE_INTERVAL=0/false/no/off.
// ONE definition shared by watch CLI + deploy installer docs.
func AdaptiveIntervalEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FLOTILLA_ADAPTIVE_INTERVAL"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// AdaptiveConfig tunes the fleet-wide adaptive tick policy (PR 3).
type AdaptiveConfig struct {
	Enabled          bool
	Floor            time.Duration // default 2m — Active tier
	Ceiling          time.Duration // default roster heartbeat_interval
	Warm             time.Duration // default 8m — Warm tier
	ReleaseStepEvery time.Duration // default 5m — max one release step per window
	IdleStableFor    time.Duration // default 10m — hysteresis before ceiling
}

// DefaultAdaptiveConfig returns design-default adaptive policy values.
func DefaultAdaptiveConfig(ceiling time.Duration) AdaptiveConfig {
	return AdaptiveConfig{
		Enabled:          true,
		Floor:            2 * time.Minute,
		Ceiling:          ceiling,
		Warm:             8 * time.Minute,
		ReleaseStepEvery: 5 * time.Minute,
		IdleStableFor:    10 * time.Minute,
	}
}

// AdaptiveInterval varies the detector tick period from ActivityTracker output.
type AdaptiveInterval interface {
	Current() time.Duration
	Update(snap ActivitySnapshot) (interval time.Duration, changed bool)
}

type adaptiveInterval struct {
	mu              sync.Mutex
	cfg             AdaptiveConfig
	current         time.Duration
	lastReleaseStep time.Time
	idleSince       time.Time
}

func normalizeAdaptiveConfig(cfg AdaptiveConfig) AdaptiveConfig {
	if cfg.Floor <= 0 {
		cfg.Floor = 2 * time.Minute
	}
	if cfg.Warm <= 0 {
		cfg.Warm = 8 * time.Minute
	}
	if cfg.Ceiling <= 0 {
		cfg.Ceiling = 20 * time.Minute
	}
	tiers := []time.Duration{cfg.Floor, cfg.Warm, cfg.Ceiling}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i] < tiers[j] })
	cfg.Floor, cfg.Warm, cfg.Ceiling = tiers[0], tiers[1], tiers[2]
	if cfg.ReleaseStepEvery <= 0 {
		cfg.ReleaseStepEvery = 5 * time.Minute
	}
	if cfg.IdleStableFor <= 0 {
		cfg.IdleStableFor = 10 * time.Minute
	}
	return cfg
}

func clampAdaptiveCurrent(d time.Duration, cfg AdaptiveConfig) time.Duration {
	if d <= 0 {
		return cfg.Ceiling
	}
	return d
}

// NewAdaptiveInterval builds the adaptive tick policy engine. When Enabled is false,
// Update is a no-op and Current returns ceiling.
func NewAdaptiveInterval(cfg AdaptiveConfig) AdaptiveInterval {
	cfg = normalizeAdaptiveConfig(cfg)
	start := clampAdaptiveCurrent(cfg.Ceiling, cfg)
	return &adaptiveInterval{cfg: cfg, current: start}
}

func (a *adaptiveInterval) Current() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.cfg.Enabled {
		return a.cfg.Ceiling
	}
	return a.current
}

func (a *adaptiveInterval) Update(snap ActivitySnapshot) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.cfg.Enabled {
		return a.current, false
	}
	now := snap.ObservedAt
	if now.IsZero() {
		now = time.Now()
	}

	a.trackIdle(snap.Level, now)
	desired := a.desiredInterval(snap.Level, now)
	changed := false

	if desired < a.current {
		// Attack: tighten immediately; start the release cooldown from this instant.
		a.current = clampAdaptiveCurrent(desired, a.cfg)
		a.lastReleaseStep = now
		changed = true
	} else if desired > a.current {
		// Release: at most one tier per ReleaseStepEvery.
		if a.lastReleaseStep.IsZero() || now.Sub(a.lastReleaseStep) >= a.cfg.ReleaseStepEvery {
			next := clampAdaptiveCurrent(a.stepUp(a.current), a.cfg)
			if next > desired {
				next = desired
			}
			if next > a.current {
				a.current = next
				a.lastReleaseStep = now
				changed = true
			}
		}
	}
	return clampAdaptiveCurrent(a.current, a.cfg), changed
}

func (a *adaptiveInterval) trackIdle(level ActivityLevel, now time.Time) {
	if level == ActivityIdle {
		if a.idleSince.IsZero() {
			a.idleSince = now
		}
		return
	}
	a.idleSince = time.Time{}
}

func (a *adaptiveInterval) desiredInterval(level ActivityLevel, now time.Time) time.Duration {
	switch level {
	case ActivityActive:
		return a.cfg.Floor
	case ActivityWarm:
		return a.cfg.Warm
	default: // ActivityIdle
		if !a.idleSince.IsZero() && now.Sub(a.idleSince) >= a.cfg.IdleStableFor {
			return a.cfg.Ceiling
		}
		return a.cfg.Warm
	}
}

func (a *adaptiveInterval) stepUp(cur time.Duration) time.Duration {
	switch a.tierOf(cur) {
	case 0:
		return a.cfg.Warm
	case 1:
		return a.cfg.Ceiling
	default:
		return a.cfg.Ceiling
	}
}

func (a *adaptiveInterval) tierOf(d time.Duration) int {
	if d <= a.cfg.Floor {
		return 0
	}
	if d <= a.cfg.Warm {
		return 1
	}
	return 2
}

// drainTimeChan discards one pending value from a tick channel when present.
func drainTimeChan(c <-chan time.Time) {
	select {
	case <-c:
	default:
	}
}

// drainTicker discards a pending tick before Stop/Reset (poke-debounce discipline).
func drainTicker(t *time.Ticker) {
	if t == nil {
		return
	}
	drainTimeChan(t.C)
}
