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

// Build derives the shared summary. AcceptsDispatch preserves the existing
// operator-facing available signal under a more honest name; it is deliberately
// independent from working/idle utilization and queue evidence.
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
		// Preserve the existing operator-facing availability signal, but name it
		// honestly: it accepts a dispatch; it is not proof of utilization.
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

// Line renders the operator-first metric line. Fraction only, no jargon:
// "3 of 12 seats working · 1 blocked · 2 held for a decision".
func Line(s Summary) string {
	var parts []string
	switch s.Working {
	case 0:
		parts = append(parts, "No seats working")
	case s.Total:
		parts = append(parts, fmt.Sprintf("All %d seats working", s.Total))
	default:
		parts = append(parts, fmt.Sprintf("%d of %d seats working", s.Working, s.Total))
	}
	if s.Blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", s.Blocked))
	}
	if s.AwaitingAuthority > 0 {
		parts = append(parts, fmt.Sprintf("%d held for a decision", s.AwaitingAuthority))
	}
	return strings.Join(parts, " · ")
}

// WallRead is the explicit diagnosis shown when almost the entire roster is
// idle. It directs the product response toward action rather than celebrating
// nominal availability.
func WallRead(s Summary) string {
	if !s.UtilizationWall {
		return ""
	}
	return "Almost no one is working — send work or pull the next queue item"
}
