// Package decisionbrief detects goals work items that are operator-gated
// (awaiting or blocked) but lack a decision_brief, and builds the dispatch
// prompt that instructs the owning desk to author one immediately (#349 item D).
// The detector logic is PURE so the watch daemon can run it each tick without
// coupling to tmux or the filesystem.
package decisionbrief

import (
	"fmt"
	"strings"
	"sync"

	"github.com/jim80net/flotilla/internal/dash"
)

// Gap is one operator-gated goals item missing a decision brief.
type Gap struct {
	GoalID    string
	GoalTitle string
	ItemKey   string // empty = goal-level rollup; else work-item discriminator
	Class     string // awaiting | blocked
	Owner     string // resolved owning desk (empty when unowned — skipped by caller)
}

// Inputs are the already-loaded values FindGaps needs (no I/O).
type Inputs struct {
	File       dash.GoalsFile
	FileOK     bool
	Backlog    string
	DeskStates map[string]string // agent name (lowercased) → state label
}

// operatorGated reports whether a work-item class or roll-up status_display is
// waiting on the operator (awaiting-auth, blocked, needs-attention, desk gates).
func operatorGated(class string) bool {
	switch class {
	case "awaiting", "blocked":
		return true
	default:
		return false
	}
}

// FindGaps returns every goal-level or work-item gap in the goals file. Malformed
// or absent files yield nil. Dependency-only items that are not operator-gated
// (in-flight, active, done) are excluded by the class filter.
func FindGaps(in Inputs) []Gap {
	if !in.FileOK || len(in.File.Goals) == 0 {
		return nil
	}
	doc := dash.BuildGoals(dash.GoalsInputs{
		File: in.File, FileOK: true,
		Backlog: in.Backlog, DeskStates: in.DeskStates,
	})
	byID := make(map[string]dash.RenderedGoal, len(doc.Goals))
	for _, g := range doc.Goals {
		byID[g.ID] = g
	}
	var gaps []Gap
	for _, g := range in.File.Goals {
		rendered, ok := byID[g.ID]
		if !ok {
			continue
		}
		owner := ResolveOwner(g)
		var itemGaps []Gap
		for i, wi := range g.WorkItems {
			if i >= len(rendered.WorkItems) {
				break
			}
			ric := rendered.WorkItems[i]
			if !operatorGated(ric.Class) || strings.TrimSpace(wi.Brief) != "" {
				continue
			}
			itemGaps = append(itemGaps, Gap{
				GoalID: g.ID, GoalTitle: g.Title,
				ItemKey: itemKey(wi, ric),
				Class:   ric.Class,
				Owner:   owner,
			})
		}
		if len(itemGaps) > 0 {
			gaps = append(gaps, itemGaps...)
			continue
		}
		hasGatedItem := false
		for i := range g.WorkItems {
			if i < len(rendered.WorkItems) && operatorGated(rendered.WorkItems[i].Class) {
				hasGatedItem = true
				break
			}
		}
		if hasGatedItem {
			continue // gated work items carry briefs — roll-up blocked is satisfied
		}
		if operatorGated(rendered.StatusDisplay) && strings.TrimSpace(g.Brief) == "" {
			gaps = append(gaps, Gap{
				GoalID: g.ID, GoalTitle: g.Title,
				Class: rendered.StatusDisplay, Owner: owner,
			})
		}
	}
	return gaps
}

func itemKey(wi dash.WorkItem, ric dash.RenderedWorkItem) string {
	if s := strings.TrimSpace(wi.Label); s != "" {
		return s
	}
	switch wi.Kind {
	case dash.WorkIssue:
		return wi.Ref
	case dash.WorkDesk:
		return wi.Agent
	case dash.WorkInline:
		return wi.Text
	case dash.WorkBacklog:
		return wi.Match
	default:
		return ric.Label
	}
}

// GapKey is the stable tracker id for a gap (re-arm when brief appears or class clears).
func GapKey(g Gap) string {
	if g.ItemKey == "" {
		return g.GoalID
	}
	return g.GoalID + ":" + g.ItemKey
}

// ResolveOwner picks the desk that authors the brief: conversation_agent, else the
// first kind=desk work item's agent. Empty when no owner can be determined.
func ResolveOwner(g dash.Goal) string {
	if ca := strings.TrimSpace(g.ConversationAgent); ca != "" {
		return ca
	}
	for _, wi := range g.WorkItems {
		if wi.Kind == dash.WorkDesk {
			if a := strings.TrimSpace(wi.Agent); a != "" {
				return a
			}
		}
	}
	return ""
}

// Tracker suppresses re-dispatch for the same gap until it clears or gains a brief.
type Tracker struct {
	mu         sync.Mutex
	dispatched map[string]bool
}

// NewTracker builds an empty gap tracker.
func NewTracker() *Tracker {
	return &Tracker{dispatched: make(map[string]bool)}
}

// Reconcile drops tracker entries for gaps that are no longer active.
func (t *Tracker) Reconcile(active map[string]bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.dispatched {
		if !active[k] {
			delete(t.dispatched, k)
		}
	}
}

// TryClaim atomically checks whether gap key is undispatched and marks it claimed.
// Returns true only for the caller that wins the race — overlapping async tick
// scans must use this instead of separate ShouldDispatch/MarkDispatched (#352 P2).
func (t *Tracker) TryClaim(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dispatched[key] {
		return false
	}
	t.dispatched[key] = true
	return true
}

// DispatchPrompt is injected into the owning desk when a gap is first detected.
func DispatchPrompt(g Gap) string {
	var b strings.Builder
	b.WriteString("[flotilla decision-brief trigger] A goals item waiting on the operator lacks a brief. Author it NOW — the operator must never have to ask the desk.\n\n")
	b.WriteString("Goal: ")
	b.WriteString(g.GoalID)
	if g.GoalTitle != "" {
		b.WriteString(" (")
		b.WriteString(g.GoalTitle)
		b.WriteString(")")
	}
	b.WriteString("\n")
	if g.ItemKey != "" {
		b.WriteString("Work item: ")
		b.WriteString(g.ItemKey)
		b.WriteString("\n")
	}
	b.WriteString("Operator gate class: ")
	b.WriteString(g.Class)
	b.WriteString("\n\n")
	b.WriteString("Write the brief using this six-element template (every field required; use labeled provenance when a dollar value is not yet measurable):\n")
	b.WriteString("1. What it is — plain language, no codenames\n")
	b.WriteString("2. Concrete value in dollars (or labeled provenance + committed measurement date when unmeasurable)\n")
	b.WriteString("3. Mechanics on approval — what happens the moment the operator says yes\n")
	b.WriteString("4. Alternatives + one-line tradeoffs each\n")
	b.WriteString("5. Recommendation + safe default\n")
	b.WriteString("6. Reversibility — how hard to undo\n\n")
	b.WriteString("Attach the brief on the goals work item as brief (markdown) in fleet-goals.yaml (or the goal node when goal-level), then run: flotilla goals compile\n\n")
	b.WriteString("The dash decision modal renders this field; an empty brief while operator-blocked is a defect you must close on this turn.")
	if g.Owner != "" {
		b.WriteString(fmt.Sprintf("\n\nYou (%s) are the owning desk for this goal.", g.Owner))
	}
	return b.String()
}
