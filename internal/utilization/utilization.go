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
	Working            int     `json:"working"`
	Idle               int     `json:"idle"`
	IdleEmptyQueue     int     `json:"idle_empty_queue"`
	IdleHasQueue       int     `json:"idle_has_queue"`
	IdleQueueUnknown   int     `json:"idle_queue_unknown"`
	Blocked            int     `json:"blocked"`
	AcceptsDispatch    int     `json:"accepts_dispatch"`
	AwaitingAuthority  int     `json:"awaiting_authority"`
	Total              int     `json:"total"`
	UtilizationPercent float64 `json:"utilization_percent"`
	UtilizationWall    bool    `json:"utilization_wall"`
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

// Build derives the shared summary. The detailed queue and posture counters are
// diagnostic wire data; Line deliberately does not expose their internal names.
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
			case QueueHasWork:
				out.IdleHasQueue++
			default:
				out.IdleQueueUnknown++
			}
		}
		// Preserve availability as diagnostic capacity evidence. It is not proof
		// of utilization and is not rendered by Line.
		if posture == "available" {
			out.AcceptsDispatch++
		}
		if state == "blocked" || raw == "blocked" {
			out.Blocked++
		}
		if raw == "awaiting-authority" || raw == "awaiting_authority" {
			out.AwaitingAuthority++
		}
	}
	if out.Total > 0 {
		out.UtilizationPercent = float64(out.Working) * 100 / float64(out.Total)
		out.UtilizationWall = out.Working <= max(1, out.Total/20)
	}
	return out
}

// Line renders one human-readable operator metric. Structured queue and posture
// counters remain available in Summary for diagnostics, but the default prose
// does not make the operator decode internal state-machine vocabulary.
func Line(s Summary) string {
	seatWord := "seats"
	if s.Total == 1 {
		seatWord = "seat"
	}
	line := fmt.Sprintf("%d of %d %s working", s.Working, s.Total, seatWord)
	if s.Blocked > 0 {
		line += fmt.Sprintf(" · %d blocked", s.Blocked)
	}
	if s.AwaitingAuthority > 0 {
		seat := "seats"
		if s.AwaitingAuthority == 1 {
			seat = "seat"
		}
		line += fmt.Sprintf(" · %d %s waiting for authority", s.AwaitingAuthority, seat)
	}
	return line
}

// WallRead gives a plain next action when almost the entire roster is idle.
func WallRead(s Summary) string {
	if !s.UtilizationWall {
		return ""
	}
	return "Almost no one is working — send work or pull the next queue item."
}
