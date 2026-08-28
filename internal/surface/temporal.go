package surface

import (
	"crypto/sha256"
	"sync"
	"time"
)

// StaticWorkingWindow is deliberately longer than an ordinary render pause but
// far shorter than the multi-hour false-busy incidents. A moving pane resets the
// observation immediately. Callers may inject a shorter window in tests.
const StaticWorkingWindow = 2 * time.Minute

type temporalObservation struct {
	frameHash  [32]byte
	resultHash [32]byte
	firstSeen  time.Time
}

// TemporalClassifier turns repeated observations into evidence. It never calls a
// pane idle: a disproven benign-working claim becomes StateWedge so delivery stays
// held while the operator gets an actionable classification.
type TemporalClassifier struct {
	mu      sync.Mutex
	now     func() time.Time
	window  time.Duration
	capture func(string) (string, error)
	seen    map[string]temporalObservation
}

func NewTemporalClassifier(capture func(string) (string, error)) *TemporalClassifier {
	return &TemporalClassifier{now: time.Now, window: StaticWorkingWindow, capture: capture, seen: make(map[string]temporalObservation)}
}

// WithTiming is the test seam; production uses NewTemporalClassifier.
func (t *TemporalClassifier) WithTiming(now func() time.Time, window time.Duration) *TemporalClassifier {
	t.now, t.window = now, window
	return t
}

// Assess preserves every non-working verdict. Working becomes Wedge only when
// BOTH the rendered frame and latest completed result are identical for the
// entire interval. Missing result evidence cannot manufacture a wedge.
func (t *TemporalClassifier) Assess(d Driver, pane string) State {
	state := d.Assess(pane)
	if state != StateWorking {
		t.mu.Lock()
		delete(t.seen, pane)
		t.mu.Unlock()
		return state
	}
	frame, err := t.capture(pane)
	if err != nil {
		return state
	}
	rr, ok := d.(ResultReader)
	if !ok {
		return state
	}
	result, err := rr.LatestResult(pane)
	if err != nil || result == "" {
		return state
	}
	now := t.now()
	cur := temporalObservation{frameHash: sha256.Sum256([]byte(frame)), resultHash: sha256.Sum256([]byte(result)), firstSeen: now}
	t.mu.Lock()
	defer t.mu.Unlock()
	prev, exists := t.seen[pane]
	if !exists || prev.frameHash != cur.frameHash || prev.resultHash != cur.resultHash {
		t.seen[pane] = cur
		return state
	}
	if now.Sub(prev.firstSeen) >= t.window {
		return StateWedge
	}
	return state
}

// temporalDriver preserves the driver's action and optional-probe contracts while
// replacing only Assess. All current production TUI drivers implement the composer
// probe; an absent probe remains undetermined and therefore fail-closed.
type temporalDriver struct {
	Driver
	tracker *TemporalClassifier
	pane    string
}

func (d temporalDriver) Assess(pane string) State { return d.tracker.Assess(d.Driver, pane) }

func (d temporalDriver) ComposerState(pane string) ComposerDisposition {
	if p, ok := d.Driver.(ComposerStateProbe); ok {
		return p.ComposerState(pane)
	}
	return ComposerUndetermined
}

func (d temporalDriver) ComposerBlockReason(pane string) string {
	if p, ok := d.Driver.(ComposerBlockReasonProbe); ok {
		return p.ComposerBlockReason(pane)
	}
	return ""
}

func (d temporalDriver) LatestResult(pane string) (string, error) {
	return d.Driver.(ResultReader).LatestResult(pane)
}

// Temporal wraps only drivers with completed-result evidence. Others retain their
// exact interface set and behavior.
func (t *TemporalClassifier) Temporal(d Driver, pane string) Driver {
	if _, ok := d.(ResultReader); !ok {
		return d
	}
	return temporalDriver{Driver: d, tracker: t, pane: pane}
}
