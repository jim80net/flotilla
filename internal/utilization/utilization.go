// Package utilization derives one utilization-first fleet summary for status,
// notifications, and the dashboard. It deliberately keeps pane activity and
// loop posture separate: blocked is an operator-relevant overlay, while working
// and idle describe what the harness is doing right now.
package utilization

import (
	"fmt"
	"strings"
)

const (
	QueueEmpty   = "empty"
	QueueHasWork = "has-work"
	QueueUnknown = "unknown"
)

// Agent is the minimal per-seat evidence needed for a fleet summary.
type Agent struct {
	State          string
	LoopPosture    string
	RawLoopPosture string
	QueueState     string
}

// Summary is the utilization-first status contract shared by every operator
// surface. Blocked is intentionally an overlay and may overlap Idle.
type Summary struct {
	Working          int `json:"working"`
	Idle             int `json:"idle"`
	IdleEmptyQueue   int `json:"idle_empty_queue"`
	IdleHasQueue     int `json:"idle_has_queue"`
	IdleQueueUnknown int `json:"idle_queue_unknown"`
	Blocked          int `json:"blocked"`
	AcceptsWork      int `json:"accepts_work"`
	Total            int `json:"total"`
}

// QueueState converts backlog read evidence into the stable wire vocabulary.
func QueueState(known bool, unblocked int) string {
	if !known {
		return QueueUnknown
	}
	if unblocked > 0 {
		return QueueHasWork
	}
	return QueueEmpty
}

// Build derives the shared summary. An idle empty-queue seat accepts work only
// when its operator-facing posture is available or parked; blocked, drifted,
// stale, and unknown seats are never advertised as capacity.
func Build(agents []Agent) Summary {
	var out Summary
	for _, a := range agents {
		out.Total++
		state := strings.ToLower(strings.TrimSpace(a.State))
		posture := strings.ToLower(strings.TrimSpace(a.LoopPosture))
		raw := strings.ToLower(strings.TrimSpace(a.RawLoopPosture))
		if raw == "" {
			raw = posture
		}
		if state == "working" {
			out.Working++
		}
		if state == "idle" {
			out.Idle++
			switch a.QueueState {
			case QueueEmpty:
				out.IdleEmptyQueue++
				if posture == "available" || posture == "parked" {
					out.AcceptsWork++
				}
			case QueueHasWork:
				out.IdleHasQueue++
			default:
				out.IdleQueueUnknown++
			}
		}
		if state == "blocked" || raw == "blocked" {
			out.Blocked++
		}
	}
	return out
}

// Line renders the operator-first metric line. It leads with utilization and
// demotes accepts-work capacity to the final, secondary clause.
func Line(s Summary) string {
	queue := fmt.Sprintf("empty-queue:%d · has-queue:%d", s.IdleEmptyQueue, s.IdleHasQueue)
	if s.IdleQueueUnknown > 0 {
		queue += fmt.Sprintf(" · queue-unknown:%d", s.IdleQueueUnknown)
	}
	return fmt.Sprintf("working:%d / idle:%d (%s) / blocked:%d · total:%d · accepts-work:%d",
		s.Working, s.Idle, queue, s.Blocked, s.Total, s.AcceptsWork)
}
