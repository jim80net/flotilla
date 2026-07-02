package watch

import "time"

// quietPingFSM implements max-quiet liveness ping with wall-time semantics.
// Zero quietSince means the quiet clock has not started (not "due immediately").
type quietPingFSM struct {
	pingPeriod time.Duration
	quietSince time.Time
}

func (q *quietPingFSM) OnWake() {
	q.quietSince = time.Time{}
}

func (q *quietPingFSM) OnColdStart() {
	q.quietSince = time.Time{}
}

func (q *quietPingFSM) OnQuietTick(now time.Time) (fire bool) {
	if q.pingPeriod <= 0 {
		return false
	}
	if q.quietSince.IsZero() {
		q.quietSince = now
		return false
	}
	return now.Sub(q.quietSince) >= q.pingPeriod
}

func (q *quietPingFSM) OnPingFired(now time.Time) {
	q.quietSince = now
}
