// Package status compresses flotilla status --json into operator-facing fleet
// posture blocks for coordinator Discord notifies (#625).
package status

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jim80net/flotilla/internal/loopposture"
	"github.com/jim80net/flotilla/internal/utilization"
)

// Headers that mean the body already carries a fleet-status block (idempotent skip).
var fleetStatusHeaders = []string{
	"**Status of the fleet**",
	"**Fleet status**",
}

// Doc is the subset of `flotilla status --json` needed to compress a notify block.
type Doc struct {
	GeneratedAt string  `json:"generated_at"`
	XO          string  `json:"xo,omitempty"`
	Agents      []Agent `json:"agents"`
}

// Agent is one seat row from status --json.
type Agent struct {
	Name           string `json:"name"`
	Role           string `json:"role,omitempty"`
	State          string `json:"state"`
	LoopPosture    string `json:"loop_posture,omitempty"`
	RawLoopPosture string `json:"raw_loop_posture,omitempty"`
	QueueState     string `json:"queue_state"`
}

// ParseDoc unmarshals status --json bytes into Doc.
func ParseDoc(raw []byte) (Doc, error) {
	var d Doc
	if err := json.Unmarshal(raw, &d); err != nil {
		return Doc{}, fmt.Errorf("status json: %w", err)
	}
	return d, nil
}

// HasFleetStatusHeader reports whether body already includes a fleet-status section.
func HasFleetStatusHeader(body string) bool {
	for _, h := range fleetStatusHeaders {
		if strings.Contains(body, h) {
			return true
		}
	}
	return false
}

// UnavailableBlock is the fail-closed footer when status cannot be read (#625).
func UnavailableBlock() string {
	return "**Status of the fleet**\n(unavailable)"
}

// CompressOptions controls skip-noise and formatting.
type CompressOptions struct {
	// Skip names (typically --from agent + its adjutant) are omitted from lists
	// and from seat counts / histograms so self-noise does not dominate.
	Skip map[string]struct{}
}

// CompressBlock builds the multi-line **Status of the fleet** markdown block
// from a statusDoc-shaped JSON document. Lists working / blocked / awaiting
// seats; idle and other states appear only in the histogram line.
func CompressBlock(doc Doc, opt CompressOptions) string {
	if len(doc.Agents) == 0 {
		return UnavailableBlock()
	}
	skip := opt.Skip
	if skip == nil {
		skip = map[string]struct{}{}
	}

	type bucket struct {
		label string
		names []string
	}
	// Notable lists (operator-facing).
	var working, blocked, awaiting []string
	// Shared utilization input over all non-skipped seats.
	utilAgents := make([]utilization.Agent, 0, len(doc.Agents))

	for _, a := range doc.Agents {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		if _, omit := skip[name]; omit {
			continue
		}
		cat := classifyState(a.State, a.LoopPosture, a.RawLoopPosture)
		utilAgents = append(utilAgents, utilization.Agent{
			State: a.State, LoopPosture: a.LoopPosture,
			RawLoopPosture: a.RawLoopPosture, QueueState: a.QueueState,
		})
		switch cat {
		case "working":
			working = append(working, name)
		case "blocked":
			blocked = append(blocked, name)
		case "awaiting":
			awaiting = append(awaiting, name)
		}
	}

	sort.Strings(working)
	sort.Strings(blocked)
	sort.Strings(awaiting)

	var b strings.Builder
	b.WriteString("**Status of the fleet**\n")
	// Summary line: utilization first. "Accepts work" remains a secondary
	// capacity signal; it never substitutes for working/idle truth (#797).
	if doc.GeneratedAt != "" {
		b.WriteString("as of ")
		b.WriteString(doc.GeneratedAt)
		b.WriteString(" · ")
	}
	summary := utilization.Build(utilAgents)
	b.WriteString(utilization.Line(summary))
	b.WriteByte('\n')

	writeList := func(label string, names []string) {
		if len(names) == 0 {
			return
		}
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(strings.Join(names, ", "))
		b.WriteByte('\n')
	}
	writeList("working", working)
	writeList("blocked", blocked)
	writeList("awaiting", awaiting)

	return strings.TrimRight(b.String(), "\n")
}

// classifyState maps pane state + loop_posture to a coarse operator bucket.
func classifyState(state, loopPosture, rawLoopPosture string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	raw := strings.ToLower(strings.TrimSpace(rawLoopPosture))
	if raw == "" {
		raw = strings.ToLower(strings.TrimSpace(loopPosture))
	}
	if raw == "awaiting_authority" {
		raw = string(loopposture.PostureAwaitingAuthority)
	}
	lp := loopposture.OperatorDisplay(loopposture.Posture(raw))
	if raw == string(loopposture.PostureAwaitingAuthority) && lp == loopposture.PostureAvailable {
		return "available"
	}
	// A real blocked posture stays strong even when the pane happens to be idle.
	// Awaiting-authority normalizes to available and therefore does not enter here.
	if lp == loopposture.PostureBlocked {
		return "blocked"
	}
	switch s {
	case "working":
		return "working"
	case "idle":
		return "idle"
	case "crashed", "shell":
		return "crashed"
	case "errored", "error":
		return "errored"
	case "unknown", "":
		return "unknown"
	case "blocked":
		return "blocked"
	default:
		// awaiting-input, awaiting-approval, awaiting, …
		if strings.HasPrefix(s, "awaiting") {
			return "awaiting"
		}
		return "other"
	}
}

// AppendFleetStatus appends block to body unless body already has a fleet-status
// header. Empty body still receives the block (attachment-only notify).
func AppendFleetStatus(body, block string) string {
	if HasFleetStatusHeader(body) {
		return body
	}
	block = strings.TrimSpace(block)
	if block == "" {
		block = UnavailableBlock()
	}
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return block
	}
	return body + "\n\n" + block
}

// SkipSet builds the self+adjutant noise set for --from agent.
func SkipSet(from, adjutant string) map[string]struct{} {
	m := map[string]struct{}{}
	if from != "" {
		m[from] = struct{}{}
	}
	if adjutant != "" {
		m[adjutant] = struct{}{}
	}
	return m
}
