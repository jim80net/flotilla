package watch

import "sync"

// PaneMutexes serializes the daemon's IN-PROCESS pane writers BEYOND the single Injector
// worker. The Injector serializes deliveries among themselves, but the change-detector's
// context rotate (the `/clear` injection) is a SEPARATE writer running on the detector
// goroutine. A confirmed delivery (surface.Confirm.Submit) is a multi-step sequence —
// submit → poll Assess → re-send Enter — that RELEASES the per-pane flock between its tmux
// calls, opening a window for a concurrent `/clear` to land between the submit and the
// retry and corrupt the composer (it would type `/clear` after an unsubmitted body). Holding
// a per-pane mutex across the WHOLE confirmed-delivery sequence, and acquiring the same mutex
// in the rotate, closes that window: the two in-daemon writers can never interleave keystrokes
// into one composer. The cross-process flock still guards external writers (`flotilla send`,
// voice); this mutex is the in-daemon layer the flock's per-call scope does not cover.
//
// Keyed by AGENT name (1:1 with a pane; both the Injector send closure and the rotate closure
// have the agent name without resolving the pane). Lock order is safe: the detector acquires
// this mutex UNDER its own detector.mu (Tick → continueXO → rotate), and the Injector worker
// acquires it ALONE (its delivery path never touches detector.mu) — a single, never-inverted
// ordering, so no deadlock.
type PaneMutexes struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

// NewPaneMutexes builds an empty per-agent mutex registry. One instance is shared by the
// daemon's Injector send closure and its detector rotate closure (cmd/flotilla/watch.go).
func NewPaneMutexes() *PaneMutexes { return &PaneMutexes{m: map[string]*sync.Mutex{}} }

// Lock acquires the per-agent mutex and returns its unlock. Hold it across a confirmed
// delivery (or a rotate) to that agent's pane so the two never interleave.
func (p *PaneMutexes) Lock(agent string) (unlock func()) {
	p.mu.Lock()
	mu, ok := p.m[agent]
	if !ok {
		mu = &sync.Mutex{}
		p.m[agent] = mu
	}
	p.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}
