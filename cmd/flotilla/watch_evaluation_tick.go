package main

import (
	"time"

	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
)

// defaultBufferSeamMaxWait is the anti-starvation threshold for evaluation-tick leader digest
// inject when the leader stays Working on a goal loop (#523 Phase 3).
const defaultBufferSeamMaxWait = 30 * time.Minute

func bufferSeamMaxWaitExceeded(bufferPath string, maxWait time.Duration, now time.Time) bool {
	age, ok := adjutantbuffer.OldestItemAge(bufferPath, now)
	return ok && age >= maxWait
}

// evaluationTickAntiStarvationDrain enqueues a leader seam brief at an evaluation tick when
// buffered items have aged past maxWait. Protected-window gating lives inside drain.
func evaluationTickAntiStarvationDrain(
	bufferPath, owner string,
	maxWait time.Duration,
	now time.Time,
	drain func(owner string),
) {
	if bufferSeamMaxWaitExceeded(bufferPath, maxWait, now) {
		drain(owner)
	}
}
