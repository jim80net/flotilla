// Package harnessquality records versioned completion and gate outcomes without
// reading transcript bodies or spending provider capacity.
package harnessquality

import (
	"fmt"
	"strings"
	"time"
)

const (
	EventSchema   = "flotilla.harness_quality_event/v1"
	ContextSchema = "flotilla.harness_quality_context/v1"
	LedgerName    = "harness-quality.jsonl"
	ContextDir    = "harness-quality-context"
)

type WorkClass string

const (
	WorkStrategic    WorkClass = "strategic"
	WorkMaintenance  WorkClass = "maintenance"
	WorkKTLO         WorkClass = "ktlo"
	WorkUnclassified WorkClass = "unclassified"
)

type EventKind string

const (
	KindCompletion EventKind = "completion"
	KindGate       EventKind = "gate"
)

type Outcome string

const (
	OutcomeCompleted Outcome = "completed"
	OutcomeMerged    Outcome = "merged"
	OutcomeAbandoned Outcome = "abandoned"
	OutcomePassed    Outcome = "passed"
	OutcomeBounced   Outcome = "bounced"
)

// Event is one append-only quality fact. Unknown metadata is explicit rather
// than inferred: Model uses "unknown", WorkClass uses "unclassified", and an
// unavailable harness version is omitted.
type Event struct {
	Schema           string    `json:"schema"`
	ID               string    `json:"id"`
	TS               string    `json:"ts"`
	Seat             string    `json:"seat"`
	Kind             EventKind `json:"event_kind"`
	Outcome          Outcome   `json:"outcome"`
	WorkClass        WorkClass `json:"work_class"`
	WorkRef          string    `json:"work_ref,omitempty"`
	Surface          string    `json:"surface"`
	Model            string    `json:"model"`
	HarnessVersion   string    `json:"harness_version,omitempty"`
	FlotillaVersion  string    `json:"flotilla_version"`
	BounceCount      int       `json:"bounce_count"`
	ReworkCount      int       `json:"rework_count"`
	SessionMirrorPtr string    `json:"session_mirror_ptr,omitempty"`
}

type Context struct {
	Schema         string    `json:"schema"`
	Seat           string    `json:"seat"`
	WorkClass      WorkClass `json:"work_class"`
	WorkRef        string    `json:"work_ref,omitempty"`
	HarnessVersion string    `json:"harness_version,omitempty"`
	UpdatedAt      string    `json:"updated_at"`
}

func ValidWorkClass(v WorkClass, allowUnclassified bool) bool {
	switch v {
	case WorkStrategic, WorkMaintenance, WorkKTLO:
		return true
	case WorkUnclassified:
		return allowUnclassified
	default:
		return false
	}
}

func (e Event) Validate() error {
	if e.Schema != EventSchema {
		return fmt.Errorf("unsupported schema %q", e.Schema)
	}
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Seat) == "" {
		return fmt.Errorf("id and seat are required")
	}
	if _, err := time.Parse(time.RFC3339, e.TS); err != nil {
		return fmt.Errorf("invalid ts: %w", err)
	}
	if !ValidWorkClass(e.WorkClass, true) {
		return fmt.Errorf("invalid work_class %q", e.WorkClass)
	}
	if strings.TrimSpace(e.Surface) == "" || strings.TrimSpace(e.Model) == "" || strings.TrimSpace(e.FlotillaVersion) == "" {
		return fmt.Errorf("surface, model, and flotilla_version are required")
	}
	if e.BounceCount < 0 || e.ReworkCount < 0 {
		return fmt.Errorf("bounce_count and rework_count cannot be negative")
	}
	switch e.Kind {
	case KindCompletion:
		if e.Outcome != OutcomeCompleted && e.Outcome != OutcomeMerged && e.Outcome != OutcomeAbandoned {
			return fmt.Errorf("completion event has invalid outcome %q", e.Outcome)
		}
	case KindGate:
		if e.Outcome != OutcomePassed && e.Outcome != OutcomeBounced {
			return fmt.Errorf("gate event has invalid outcome %q", e.Outcome)
		}
		if e.Outcome == OutcomeBounced && e.BounceCount == 0 {
			return fmt.Errorf("bounced gate requires bounce_count > 0")
		}
	default:
		return fmt.Errorf("invalid event_kind %q", e.Kind)
	}
	return nil
}

func (c Context) Validate() error {
	if c.Schema != ContextSchema {
		return fmt.Errorf("unsupported schema %q", c.Schema)
	}
	if strings.TrimSpace(c.Seat) == "" {
		return fmt.Errorf("seat is required")
	}
	if !ValidWorkClass(c.WorkClass, false) {
		return fmt.Errorf("work_class must be strategic, maintenance, or ktlo")
	}
	if _, err := time.Parse(time.RFC3339, c.UpdatedAt); err != nil {
		return fmt.Errorf("invalid updated_at: %w", err)
	}
	return nil
}
