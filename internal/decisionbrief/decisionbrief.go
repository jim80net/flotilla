// Package decisionbrief detects goals work items that are operator-gated
// (awaiting or blocked) but lack a decision_brief, and builds the dispatch
// prompt that instructs the owning desk to author one immediately (#349 item D).
// The detector logic is PURE so the watch daemon can run it each tick without
// coupling to tmux or the filesystem.
package decisionbrief

import (
	"crypto/sha256"
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
	goalByID := make(map[string]dash.Goal, len(in.File.Goals))
	for _, goal := range in.File.Goals {
		goalByID[goal.ID] = goal
	}
	var gaps []Gap
	for _, g := range in.File.Goals {
		rendered, ok := byID[g.ID]
		if !ok {
			continue
		}
		owner := ResolveOwnerInTree(g, goalByID)
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
		// Briefs live at work_items[].brief (#365): do not fire a node-level trigger when any
		// work item already carries a decision package (desk execution items may be in-flight
		// while the node roll-up is still operator-gated).
		if anyWorkItemBriefPresent(g) {
			continue
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

func anyWorkItemBriefPresent(g dash.Goal) bool {
	for _, wi := range g.WorkItems {
		if strings.TrimSpace(wi.Brief) != "" {
			return true
		}
	}
	return false
}

// GapStillOpen reports whether target is still among FindGaps results (dispatch-time re-verify).
func GapStillOpen(in Inputs, target Gap) bool {
	want := GapKey(target)
	for _, g := range FindGaps(in) {
		if GapKey(g) == want {
			return true
		}
	}
	return false
}

// GapKey is the stable tracker id for a gap (re-arm when brief appears or class clears).
func GapKey(g Gap) string {
	if g.ItemKey == "" {
		return g.GoalID
	}
	return g.GoalID + ":" + g.ItemKey
}

// ResolveOwner picks the desk that authors the brief on this goal only: conversation_agent,
// else the first kind=desk work item's agent. Empty when no owner can be determined.
func ResolveOwner(g dash.Goal) string {
	return resolveOwnerDirect(g)
}

// ResolveOwnerInTree walks up the parent chain to the nearest ancestor with an owner (#482).
func ResolveOwnerInTree(g dash.Goal, byID map[string]dash.Goal) string {
	for cur := g; ; {
		if o := resolveOwnerDirect(cur); o != "" {
			return o
		}
		parentID := strings.TrimSpace(cur.Parent)
		if parentID == "" {
			return ""
		}
		parent, ok := byID[parentID]
		if !ok {
			return ""
		}
		cur = parent
	}
}

func resolveOwnerDirect(g dash.Goal) string {
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

// UnownedSkipLatch hash-latches no-owning-desk skip logs per gap shape (#482).
// Production invokes DecisionBriefOnTick via MirrorDispatch (go run), so overlapping
// ticks may access the latch concurrently — the mutex matches Tracker's posture.
type UnownedSkipLatch struct {
	mu     sync.Mutex
	shapes map[string]string // GapKey → shape hash
}

// NewUnownedSkipLatch builds an empty skip-log latch.
func NewUnownedSkipLatch() *UnownedSkipLatch {
	return &UnownedSkipLatch{shapes: make(map[string]string)}
}

// ShouldLog reports whether the no-owning-desk skip line should be logged for g.
func (l *UnownedSkipLatch) ShouldLog(g Gap) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := GapKey(g)
	h := unownedSkipShapeHash(g)
	if l.shapes[key] == h {
		return false
	}
	l.shapes[key] = h
	return true
}

// Clear drops the latch entry when a gap clears or gains an owner.
func (l *UnownedSkipLatch) Clear(g Gap) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.shapes, GapKey(g))
}

// Reconcile drops latch entries for gaps no longer active this tick.
func (l *UnownedSkipLatch) Reconcile(active map[string]bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for key := range l.shapes {
		if !active[key] {
			delete(l.shapes, key)
		}
	}
}

func unownedSkipShapeHash(g Gap) string {
	sum := sha256.Sum256([]byte(g.GoalID + "\x00" + g.ItemKey + "\x00" + g.Class))
	return fmt.Sprintf("%x", sum)
}

// Tracker suppresses re-dispatch for the same gap until it clears or gains a brief.
// pending holds in-flight enqueues (not yet confirmed); dispatched is persisted only
// after confirmed delivery — a busy-dropped detector tick must not leave a durable claim (#365 P1).
type Tracker struct {
	mu         sync.Mutex
	pending    map[string]bool
	dispatched map[string]bool
}

// NewTracker builds an empty gap tracker.
func NewTracker() *Tracker {
	return &Tracker{
		pending:    make(map[string]bool),
		dispatched: make(map[string]bool),
	}
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

// TryBeginDispatch atomically checks whether gap key is not already pending or
// confirmed-dispatched and marks it pending. Returns true only for the caller that
// wins the race — overlapping async tick scans must use this instead of separate
// ShouldDispatch/MarkDispatched (#352 P2). Pending is in-memory only; Confirm
// promotes to dispatched after the injector confirms delivery.
func (t *Tracker) TryBeginDispatch(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dispatched[key] || t.pending[key] {
		return false
	}
	t.pending[key] = true
	return true
}

// Confirm promotes a pending gap key to dispatched after confirmed delivery.
func (t *Tracker) Confirm(key string) {
	if key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pending, key)
	t.dispatched[key] = true
}

// Abort clears a pending gap key without recording a dispatch (busy drop or delivery failure).
func (t *Tracker) Abort(key string) {
	if key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pending, key)
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
