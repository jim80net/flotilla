package watch

import "testing"

func TestWatchdogAlertsAfterKMissedAcks(t *testing.T) {
	var alerts []string
	w := NewWatchdog(3, func(m string) { alerts = append(alerts, m) })

	w.Observe(false, false) // miss 1
	w.Observe(false, false) // miss 2
	if len(alerts) != 0 {
		t.Fatalf("alerted early after %d misses", len(alerts))
	}
	w.Observe(false, false) // miss 3 -> trip
	if len(alerts) != 1 {
		t.Fatalf("alerts after 3 misses = %d, want 1", len(alerts))
	}
	if !w.Down() {
		t.Error("watchdog should be Down after tripping")
	}
}

func TestWatchdogDebouncesWhilePersistentlyDown(t *testing.T) {
	var alerts int
	w := NewWatchdog(1, func(string) { alerts++ })
	for i := 0; i < 5; i++ {
		w.Observe(false, false) // each is a miss; should alert only on the transition
	}
	if alerts != 1 {
		t.Errorf("alerts while persistently down = %d, want 1 (debounced)", alerts)
	}
}

func TestWatchdogRecoveryClearsAndCanReTrip(t *testing.T) {
	var alerts int
	w := NewWatchdog(2, func(string) { alerts++ })
	w.Observe(false, false)
	w.Observe(false, false) // trip (1)
	w.Observe(true, false)  // recover
	if w.Down() {
		t.Error("watchdog should clear Down on ack")
	}
	w.Observe(false, false)
	w.Observe(false, false) // trip again (2)
	if alerts != 2 {
		t.Errorf("alerts across down/recover/down = %d, want 2", alerts)
	}
}

func TestWatchdogCrashFastPath(t *testing.T) {
	var alerts int
	w := NewWatchdog(99, func(string) { alerts++ }) // high miss threshold...
	w.Observe(false, true)                          // ...but a crash trips immediately
	if alerts != 1 || !w.Down() {
		t.Errorf("crash fast-path: alerts=%d down=%v, want 1/true", alerts, w.Down())
	}
}
