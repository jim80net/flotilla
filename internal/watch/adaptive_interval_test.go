package watch

import (
	"testing"
	"time"
)

func testAdaptiveConfig() AdaptiveConfig {
	return AdaptiveConfig{
		Enabled:          true,
		Floor:            2 * time.Minute,
		Warm:             8 * time.Minute,
		Ceiling:          20 * time.Minute,
		ReleaseStepEvery: 5 * time.Minute,
		IdleStableFor:    10 * time.Minute,
	}
}

func snapAt(level ActivityLevel, t time.Time) ActivitySnapshot {
	return ActivitySnapshot{Level: level, ObservedAt: t}
}

func TestAdaptiveIntervalAttackActiveImmediate(t *testing.T) {
	ai := NewAdaptiveInterval(testAdaptiveConfig())
	t0 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if got := ai.Current(); got != 20*time.Minute {
		t.Fatalf("cold start current = %v, want ceiling", got)
	}
	interval, changed := ai.Update(snapAt(ActivityActive, t0))
	if !changed || interval != 2*time.Minute {
		t.Fatalf("Active attack = (%v, %v), want (2m, true)", interval, changed)
	}
}

func TestAdaptiveIntervalReleaseOneStepPerWindow(t *testing.T) {
	ai := NewAdaptiveInterval(testAdaptiveConfig())
	t0 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	ai.Update(snapAt(ActivityActive, t0)) // floor

	interval, changed := ai.Update(snapAt(ActivityWarm, t0.Add(time.Minute)))
	if changed {
		t.Fatalf("release within ReleaseStepEvery must not step early, got %v", interval)
	}

	interval, changed = ai.Update(snapAt(ActivityWarm, t0.Add(5*time.Minute)))
	if !changed || interval != 8*time.Minute {
		t.Fatalf("release after step window = (%v, %v), want (8m, true)", interval, changed)
	}
}

func TestAdaptiveIntervalIdleHysteresisBeforeCeiling(t *testing.T) {
	ai := NewAdaptiveInterval(testAdaptiveConfig())
	t0 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	ai.Update(snapAt(ActivityActive, t0)) // floor

	// Idle but not yet stable — desired Warm, release floor→warm after step window.
	_, _ = ai.Update(snapAt(ActivityIdle, t0.Add(5*time.Minute)))
	if got := ai.Current(); got != 8*time.Minute {
		t.Fatalf("idle before stable should step to warm only, got %v", got)
	}

	// Still within IdleStableFor — must not reach ceiling yet.
	_, changed := ai.Update(snapAt(ActivityIdle, t0.Add(9*time.Minute)))
	if changed {
		t.Fatalf("idle < IdleStableFor must not release toward ceiling yet, current=%v", ai.Current())
	}

	// Idle stable elapsed — release warm→ceiling after step window.
	_, changed = ai.Update(snapAt(ActivityIdle, t0.Add(15*time.Minute)))
	if !changed || ai.Current() != 20*time.Minute {
		t.Fatalf("idle stable ⇒ ceiling, got (%v, %v)", ai.Current(), changed)
	}
}

func TestAdaptiveIntervalDisabledByteInert(t *testing.T) {
	cfg := testAdaptiveConfig()
	cfg.Enabled = false
	ai := NewAdaptiveInterval(cfg)
	t0 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	interval, changed := ai.Update(snapAt(ActivityActive, t0))
	if changed || interval != 20*time.Minute {
		t.Fatalf("disabled adaptive must not change interval, got (%v, %v)", interval, changed)
	}
}

func TestDrainTickerDropsPendingTick(t *testing.T) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	<-ticker.C
	drainTicker(ticker)
	select {
	case <-ticker.C:
		t.Fatal("drainTicker must discard a pending tick before Stop/Reset")
	default:
	}
}

func TestAdaptiveIntervalReAttackDuringRelease(t *testing.T) {
	ai := NewAdaptiveInterval(testAdaptiveConfig())
	t0 := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	ai.Update(snapAt(ActivityActive, t0))
	ai.Update(snapAt(ActivityWarm, t0.Add(5*time.Minute))) // warm

	interval, changed := ai.Update(snapAt(ActivityActive, t0.Add(6*time.Minute)))
	if !changed || interval != 2*time.Minute {
		t.Fatalf("re-attack during release = (%v, %v), want (2m, true)", interval, changed)
	}
}
