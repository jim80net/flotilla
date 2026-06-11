package voice

import (
	"sync"
	"sync/atomic"
	"testing"
)

// The XO's explicit ask: prove the HARD-STOP path — cap hit → the session ends cleanly
// (every subsequent reserve also hard-stops), it does NOT silently continue spending,
// and spend never overshoots the cap.
func TestMeterHardStop(t *testing.T) {
	caps := Caps{TTSUSDPerM: 1_000_000} // $1 per character → easy arithmetic
	m := NewMeter(2.50, caps)

	if err := m.ReserveTTS(1); err != nil { // $1
		t.Fatal(err)
	}
	if err := m.ReserveTTS(1); err != nil { // $2
		t.Fatal(err)
	}
	if m.Stopped() || m.SpentUSD() != 2.0 {
		t.Fatalf("under cap: stopped=%v spent=%v want false/2.0", m.Stopped(), m.SpentUSD())
	}

	// A 3rd $1 op would hit $3 > $2.50 → HARD STOP: rejected, not spent, session stopped.
	if err := m.ReserveTTS(1); err != ErrCapReached {
		t.Fatalf("over-cap reserve = %v, want ErrCapReached", err)
	}
	if !m.Stopped() {
		t.Fatal("session must be stopped once the cap is hit")
	}
	if m.SpentUSD() != 2.0 {
		t.Errorf("spend overshot the cap: %v (must stay 2.0 — the over-cap op was rejected)", m.SpentUSD())
	}

	// The session ENDS: every later reserve hard-stops (no silent continuation), even a
	// would-be-affordable or zero-cost one.
	if err := m.ReserveSTT(0.0001); err != ErrCapReached {
		t.Errorf("post-cap STT reserve = %v, want a hard stop", err)
	}
	if err := m.ReserveTTS(0); err != ErrCapReached {
		t.Errorf("post-cap zero-cost reserve = %v, want a hard stop (session ended)", err)
	}
}

// reserve is atomic (check+commit under one mutex) so concurrent synthesis cannot
// overshoot the cap via a check-then-spend race.
func TestMeterAtomicUnderConcurrency(t *testing.T) {
	caps := Caps{TTSUSDPerM: 1_000_000} // $1/char
	m := NewMeter(5.0, caps)            // at most 5 successful $1 reserves

	var ok int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m.ReserveTTS(1) == nil {
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	wg.Wait()

	if ok != 5 {
		t.Errorf("%d concurrent reserves succeeded, want exactly 5 (cap not atomic?)", ok)
	}
	if m.SpentUSD() != 5.0 {
		t.Errorf("spent = %v, must never exceed the $5 cap", m.SpentUSD())
	}
}
