package watch

import (
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// DefaultEventPollInterval is how often TurnEndPoller re-assesses desk panes for
// Working→Idle transitions when event-driven coordination is enabled.
const DefaultEventPollInterval = 5 * time.Second

// DefaultPokeDebounce coalesces a burst of desk finishes into one detector Tick.
const DefaultPokeDebounce = 3 * time.Second

// TurnEndPoller fast-polls desk pane states and pokes the detector when a
// non-clock-XO desk finishes a turn (Working→Idle). It reuses the same Assess
// seam the interval tick uses — no per-desk Stop-hook or signal-file required.
type TurnEndPoller struct {
	xoAgent  string
	desks    []string
	assess   func(agent string) surface.State
	poke     func()
	interval time.Duration

	stop      chan struct{}
	done      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once

	last map[string]surface.State
}

// NewTurnEndPoller builds a poller. interval <= 0 disables it (Start is a no-op).
func NewTurnEndPoller(xoAgent string, desks []string, assess func(string) surface.State, poke func(), interval time.Duration) *TurnEndPoller {
	return &TurnEndPoller{
		xoAgent:  xoAgent,
		desks:    desks,
		assess:   assess,
		poke:     poke,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		last:     make(map[string]surface.State, len(desks)),
	}
}

// Start launches the poll loop. Idempotent when interval <= 0.
func (p *TurnEndPoller) Start() {
	p.startOnce.Do(func() {
		if p.interval <= 0 {
			close(p.done)
			return
		}
		go p.loop()
	})
}

// Stop ends the poll loop and waits for exit. Idempotent.
func (p *TurnEndPoller) Stop() {
	p.stopOnce.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}
		close(p.stop)
		<-p.done
	})
}

func (p *TurnEndPoller) loop() {
	defer close(p.done)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	seeded := false
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			if p.pollOnce(!seeded) {
				seeded = true
			}
		}
	}
}

// pollOnce returns true after the cache is seeded (first pass never pokes).
func (p *TurnEndPoller) pollOnce(seedOnly bool) bool {
	for _, name := range p.desks {
		if name == p.xoAgent {
			continue
		}
		cur := p.assess(name)
		prev, seen := p.last[name]
		if seen && !seedOnly && prev == surface.StateWorking && cur == surface.StateIdle {
			p.poke()
		}
		p.last[name] = cur
	}
	return true
}
