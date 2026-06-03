package watch

// Watchdog determines XO liveness from acknowledgements, not process existence,
// so it catches the alive-but-context-exhausted XO (process up, pane title still
// matches) that a process scan misses. Each heartbeat cycle calls Observe with
// the cycle's signals; the watchdog emits AT MOST ONE alert on the
// down-transition (debounced) and clears on recovery.
type Watchdog struct {
	maxMissed int
	alert     func(string)

	missed int
	down   bool
}

// NewWatchdog builds a watchdog that alerts after maxMissed consecutive missed
// acks. maxMissed < 1 is treated as 1.
func NewWatchdog(maxMissed int, alert func(string)) *Watchdog {
	if maxMissed < 1 {
		maxMissed = 1
	}
	return &Watchdog{maxMissed: maxMissed, alert: alert}
}

// Observe records one cycle's liveness signals:
//   - crashed: the XO pane fell back to a shell (or vanished) — an immediate
//     crash fast-path.
//   - acked: the XO produced a liveness ack since the previous Observe.
//
// It alerts at most once per down-transition and clears the down state on
// recovery, so a persistently-down XO does not spam the channel.
func (w *Watchdog) Observe(acked, crashed bool) {
	if crashed {
		w.trip("XO pane is not a live session (shell fallback) — restart needed")
		return
	}
	if acked {
		w.missed = 0
		w.down = false // recovered
		return
	}
	w.missed++
	if w.missed >= w.maxMissed {
		w.trip("XO unresponsive — no liveness ack for the alert threshold; likely context-exhausted or wedged — restart needed")
	}
}

// Down reports whether the watchdog currently considers the XO down (the clock
// should not keep winding a dead XO).
func (w *Watchdog) Down() bool { return w.down }

// trip fires the alert only on the down-transition (debounce).
func (w *Watchdog) trip(msg string) {
	if w.down {
		return
	}
	w.down = true
	if w.alert != nil {
		w.alert(msg)
	}
}
