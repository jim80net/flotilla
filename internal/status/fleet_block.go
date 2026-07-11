// Package status compresses flotilla status --json into operator-facing fleet
// posture blocks for coordinator Discord notifies (#625).
package status

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	Name        string `json:"name"`
	Role        string `json:"role,omitempty"`
	State       string `json:"state"`
	LoopPosture string `json:"loop_posture,omitempty"`
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
	// Histogram over all non-skipped seats.
	hist := map[string]int{}
	n := 0

	for _, a := range doc.Agents {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		if _, omit := skip[name]; omit {
			continue
		}
		n++
		cat := classifyState(a.State, a.LoopPosture)
		hist[cat]++
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
	// Summary line: as-of + seat count + compact histogram.
	if doc.GeneratedAt != "" {
		b.WriteString("as of ")
		b.WriteString(doc.GeneratedAt)
		b.WriteString(" · ")
	}
	b.WriteString(fmt.Sprintf("%d seats", n))
	for _, key := range histOrder {
		if c := hist[key]; c > 0 {
			b.WriteString(fmt.Sprintf(" · %s:%d", key, c))
		}
	}
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

// histOrder is stable histogram key order for the summary line.
var histOrder = []string{"working", "blocked", "awaiting", "idle", "crashed", "errored", "unknown", "other"}

// classifyState maps pane state + loop_posture to a coarse operator bucket.
func classifyState(state, loopPosture string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	lp := strings.ToLower(strings.TrimSpace(loopPosture))
	// Loop posture can reclassify idle/working seats into blocked-ish authority waits.
	if lp == "awaiting-authority" || lp == "awaiting_authority" {
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
