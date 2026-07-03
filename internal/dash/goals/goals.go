// Package goals is the dash's MINIMAL, read-only goals read model: it parses the
// coordinator-authored fleet-goals.yaml into the §6.1 GoalsDoc JSON contract the
// Goals-map view (#267) renders. It is deliberately minimal — no CLI, no compile
// cache, no gh round-trips, nothing beyond what the UI consumes — because
// flotilla-dev's fuller internal/goals core (validate/compile/roll-ups/issue
// trailer) supersedes it later BEHIND THE SAME JSON CONTRACT (COS ruling
// 2026-07-03, Option B: drop-in swap, no UI change). The types here are the wire
// contract; keep them faithful to design.md §4.2 / §4.4 / §6.1.
//
// Pure over files (design §6.3): Load reads + parses; Build* are pure functions,
// unit-tested without HTTP.
package goals

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// GoalNode is one node in the goals tree. `Status` is the coordinator-AUTHORED
// state (drives aspirational/achieved styling); `StatusDisplay` is the COMPUTED
// roll-up (drives the live blocked/awaiting/in-flight coloring). The UI reads
// both. JSON tags are the contract the Goals view binds to.
type GoalNode struct {
	ID     string `json:"id" yaml:"id"`
	Title  string `json:"title" yaml:"title"`
	Scope  string `json:"scope,omitempty" yaml:"scope"` // fleet | project | task
	Parent string `json:"parent,omitempty" yaml:"parent"`
	Owner  string `json:"owner,omitempty" yaml:"owner"` // coordinator/desk role (generic)
	// ConversationAgent is the deep-link ref: clicking the node opens this agent's
	// Conversations thread (#267 tri-surface mirroring). Falls back to Owner in the
	// UI when unset.
	ConversationAgent string      `json:"conversation_agent,omitempty" yaml:"conversation_agent"`
	Status            string      `json:"status" yaml:"status"`    // active | achieved | paused | cancelled (authored)
	StatusDisplay     string      `json:"status_display" yaml:"-"` // computed roll-up (never from yaml)
	DependsOn         []string    `json:"depends_on,omitempty" yaml:"depends_on"`
	WorkItems         []WorkItem  `json:"work_items,omitempty" yaml:"work_items"`
	Children          []*GoalNode `json:"children,omitempty" yaml:"children"`
}

// Edge is a cross-dependency edge (depends_on), exposed flat in GoalsDoc.edges[] so
// the UI can draw the faint dependency lines / gantt-style ID labels without walking
// the tree (operator feedback #2). Structural parent/child (serves/realizes) stay in
// the tree; edges[] carries only the cross-links.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // "depends_on" (ratified spec goals/spec.md line 43)
}

// WorkItem binds a node to concrete work. Kind is issue|backlog|inline|desk; the
// binding field depends on the kind (ref for issue/desk, marker for backlog, text
// + done for inline). Status is the minimal derived state the UI shows without
// itself calling gh: the minimal parser resolves the two YAML-DETERMINISTIC kinds
// (backlog markers, inline done) fully, and leaves the two LIVE-RESOLVED kinds
// (issue open/closed, desk snapshot state) NEUTRAL — the fuller internal/goals core
// resolves those. Neutral is honest, not fabricated: an unresolvable issue is never
// asserted done or in-flight (never-fabricate); it simply does not promote a node to
// achieved on its own (see compute).
type WorkItem struct {
	Kind   string `json:"kind" yaml:"kind"`
	Ref    string `json:"ref,omitempty" yaml:"ref"`
	Marker string `json:"marker,omitempty" yaml:"marker"`
	Text   string `json:"text,omitempty" yaml:"text"`
	Done   bool   `json:"done,omitempty" yaml:"done"` // inline checklist completion
	Status string `json:"status,omitempty" yaml:"-"`  // derived (never from yaml): blocked|awaiting|in-flight|done|""
}

// GoalsDoc is the /api/goals response: the goal tree, a flat id→status_display
// roll-up map (for cheap lookups + the dependency-line coloring), and the "as of"
// stamp. Matches design §6.1.
type GoalsDoc struct {
	Tree        []*GoalNode       `json:"tree"`
	Edges       []Edge            `json:"edges"`
	Rollups     map[string]string `json:"rollups"`
	GeneratedAt string            `json:"generated_at"`
}

// GoalDetailDoc is the /api/goals/{id} response (design §6.1). Minimal: the node,
// its work items, and the owner desk(s) as a hint the UI enriches from /api/status.
type GoalDetailDoc struct {
	Node       *GoalNode   `json:"node"`
	WorkItems  []WorkItem  `json:"work_items"`
	DeskStates []DeskState `json:"desk_states"`
}

// DeskState is a minimal owner-desk hint (the UI joins live state from /api/status).
type DeskState struct {
	Agent string `json:"agent"`
}

// yamlFile is the fleet-goals.yaml top-level shape.
type yamlFile struct {
	Version int         `yaml:"version"`
	Goals   []*GoalNode `yaml:"goals"`
}

// Load reads + parses fleet-goals.yaml. A missing file is NOT an error — it yields
// an empty tree (the dash shows an honest "no goals yet" state, like an absent
// snapshot). A malformed/ cyclic file IS a typed error (never a silent mis-read).
func Load(path string) (*GoalsDoc, error) {
	if path == "" {
		return emptyDoc(), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyDoc(), nil
		}
		return nil, fmt.Errorf("read goals file %q: %w", path, err)
	}
	return Parse(raw)
}

// Parse builds a GoalsDoc from raw yaml bytes (pure; the testable core).
func Parse(raw []byte) (*GoalsDoc, error) {
	var f yamlFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse goals yaml: %w", err)
	}
	roots := f.Goals
	if roots == nil {
		roots = []*GoalNode{} // "no goals yet" → tree:[], never JSON null (contract shape)
	}
	ids := map[string]bool{}
	if err := validateAcyclic(roots, ids); err != nil {
		return nil, err
	}
	edges, err := buildEdges(roots, ids)
	if err != nil {
		return nil, err
	}
	rollups := map[string]string{}
	for _, n := range roots {
		compute(n, rollups)
	}
	return &GoalsDoc{Tree: roots, Edges: edges, Rollups: rollups, GeneratedAt: nowRFC3339()}, nil
}

// buildEdges flattens every node's depends_on into cross-dependency edges. A
// depends_on referencing an unknown id is config drift (a typo) — a typed error,
// not a silently-dropped edge. (A full depends_on cycle check is deferred to
// flotilla-dev's core; the primary hierarchy is already tree-acyclic.)
func buildEdges(roots []*GoalNode, ids map[string]bool) ([]Edge, error) {
	edges := []Edge{}
	var walk func(nodes []*GoalNode) error
	walk = func(nodes []*GoalNode) error {
		for _, n := range nodes {
			for _, dep := range n.DependsOn {
				if !ids[dep] {
					return fmt.Errorf("goals: node %q depends_on unknown id %q", n.ID, dep)
				}
				edges = append(edges, Edge{From: n.ID, To: dep, Kind: "depends_on"})
			}
			if err := walk(n.Children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(roots); err != nil {
		return nil, err
	}
	return edges, nil
}

// Detail returns the node + its work items + owner-desk hints for /api/goals/{id},
// or (nil, false) if the id is absent.
func (d *GoalsDoc) Detail(id string) (*GoalDetailDoc, bool) {
	n := d.find(id)
	if n == nil {
		return nil, false
	}
	desks := []DeskState{}
	if n.Owner != "" {
		desks = append(desks, DeskState{Agent: n.Owner})
	}
	items := n.WorkItems
	if items == nil {
		items = []WorkItem{}
	}
	return &GoalDetailDoc{Node: n, WorkItems: items, DeskStates: desks}, true
}

// --- helpers ---

func (d *GoalsDoc) find(id string) *GoalNode {
	var walk func(nodes []*GoalNode) *GoalNode
	walk = func(nodes []*GoalNode) *GoalNode {
		for _, n := range nodes {
			if n.ID == id {
				return n
			}
			if hit := walk(n.Children); hit != nil {
				return hit
			}
		}
		return nil
	}
	return walk(d.Tree)
}

// validateAcyclic enforces the v1 tree invariant: every ID unique, and no cycle
// via the parent references (children are structural, but a malformed file could
// declare a child whose parent points elsewhere, or repeat an ID). Same fail-loud
// discipline as roster.assertSynthesisAcyclic.
func validateAcyclic(roots []*GoalNode, seen map[string]bool) error {
	var walk func(n *GoalNode, ancestors map[string]bool) error
	walk = func(n *GoalNode, ancestors map[string]bool) error {
		if n == nil {
			// A null yaml sequence entry (`- ` / an empty `children:` item) decodes to
			// a nil node — a TYPED error, never a nil-deref panic (trio HIGH).
			return fmt.Errorf("goals: a node is null (malformed yaml list entry)")
		}
		if n.ID == "" {
			return fmt.Errorf("goals: a node has an empty id")
		}
		if seen[n.ID] {
			return fmt.Errorf("goals: duplicate node id %q", n.ID)
		}
		seen[n.ID] = true
		if ancestors[n.ID] {
			return fmt.Errorf("goals: cycle at node %q", n.ID)
		}
		ancestors[n.ID] = true
		for _, c := range n.Children {
			if err := walk(c, ancestors); err != nil {
				return err
			}
		}
		delete(ancestors, n.ID)
		return nil
	}
	for _, r := range roots {
		if err := walk(r, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

// compute fills StatusDisplay for a node and its subtree (post-order) + records it
// in rollups, following the RATIFIED precedence (openspec/changes/dash-next-gen/
// specs/goals/spec.md lines 89-103, "Roll-up status combines authored and computed
// state"). First match wins:
//
//  1. authored cancelled                          → cancelled
//  2. any child/item blocked                      → blocked
//  3. any child/item awaiting                     → awaiting
//  4. authored paused                             → paused
//  5. any child/item in-flight                    → in-flight
//  6. authored achieved + all non-cancelled kids achieved + items done → achieved
//  7. all non-cancelled kids achieved + items done + ≥1 child/item      → achieved
//  8. zero children AND zero items                → active (vacuous-achieved guard)
//  9. otherwise                                   → active
//
// Two deltas from a naive reading: (a) a PAUSE does NOT hide a live blocker — only
// authored cancelled (rule 1, a dead branch) outranks blocked; a paused node with a
// blocked descendant renders blocked. (b) CANCELLED children are EXCLUDED from the
// achieved test (rules 6/7) — a cancelled sub-goal is a dead branch and never holds
// the parent out of achieved. Children propagate blocked/awaiting/in-flight upward;
// they count toward achieved only if themselves achieved.
//
// Children are computed post-order first so their rollups exist before the parent
// evaluates its precedence.
func compute(n *GoalNode, rollups map[string]string) string {
	for i := range n.WorkItems {
		n.WorkItems[i].Status = itemStatus(n.WorkItems[i])
	}

	childBlocked, childAwaiting, childInFlight := false, false, false
	nonCancelledKids, nonCancelledAchievedKids := 0, 0
	for _, c := range n.Children {
		cd := compute(c, rollups) // compute each child EXACTLY once (post-order)
		switch cd {
		case "blocked":
			childBlocked = true
		case "awaiting":
			childAwaiting = true
		case "in-flight":
			childInFlight = true
		}
		if cd != "cancelled" { // cancelled kids are dead branches, excluded from the achieved test
			nonCancelledKids++
			if cd == "achieved" {
				nonCancelledAchievedKids++
			}
		}
	}

	// Item signals. A neutral item (unresolved issue/desk) is NOT "done" — it holds a
	// node out of achieved (we can't confirm completion) but is never asserted
	// in-flight/blocked (never-fabricate). The full core resolves those live.
	itemBlocked, itemAwaiting, itemInFlight, itemsDone := false, false, false, true
	for _, wi := range n.WorkItems {
		switch wi.Status {
		case "blocked":
			itemBlocked, itemsDone = true, false
		case "awaiting":
			itemAwaiting, itemsDone = true, false
		case "in-flight":
			itemInFlight, itemsDone = true, false
		case "done":
			// counts as done
		default:
			itemsDone = false // neutral/unresolved — not confirmed done
		}
	}

	allKidsAchieved := nonCancelledKids == nonCancelledAchievedKids // vacuously true with 0 non-cancelled kids
	hasStructure := len(n.Children) > 0 || len(n.WorkItems) > 0

	var display string
	switch {
	case n.Status == "cancelled": // 1
		display = "cancelled"
	case childBlocked || itemBlocked: // 2 — a pause never hides a live blocker
		display = "blocked"
	case childAwaiting || itemAwaiting: // 3
		display = "awaiting"
	case n.Status == "paused": // 4 — outranks in-flight, yields to blocked/awaiting
		display = "paused"
	case childInFlight || itemInFlight: // 5
		display = "in-flight"
	case n.Status == "achieved" && allKidsAchieved && itemsDone: // 6 (authored; leaf ok)
		display = "achieved"
	case hasStructure && allKidsAchieved && itemsDone: // 7 (computed; ≥1 child/item)
		display = "achieved"
	default: // 8 (zero/zero) + 9 → active; the vacuous leaf is never achieved by computation
		display = "active"
	}
	n.StatusDisplay = display
	rollups[n.ID] = display
	return display
}

// itemStatus derives a minimal work-item status from its binding, resolving only the
// two YAML-DETERMINISTIC kinds — WITHOUT calling gh or reading the watch snapshot
// (that live resolution is flotilla-dev's core):
//   - backlog: the marker tokens (reuses the backlog.Parse vocabulary).
//   - inline:  done → "done", otherwise "in-flight" (a bare checklist line is open work).
//   - issue/desk: NEUTRAL ("") — live-resolved kinds the minimal parser does not fabricate.
func itemStatus(wi WorkItem) string {
	switch wi.Kind {
	case "backlog":
		m := strings.ToLower(wi.Marker)
		switch {
		case strings.Contains(m, "[blocked]") || strings.Contains(m, "[needs-attention]"):
			return "blocked"
		case strings.Contains(m, "[awaiting-auth]"):
			return "awaiting"
		case strings.Contains(m, "[in-flight]") || strings.Contains(m, "[pending]"):
			return "in-flight"
		case strings.Contains(m, "[done]"):
			return "done"
		default:
			return ""
		}
	case "inline":
		if wi.Done {
			return "done"
		}
		return "in-flight"
	default: // issue, desk — live-resolved, left neutral
		return ""
	}
}

func emptyDoc() *GoalsDoc {
	// All slices/maps non-nil so the JSON is a consistent shape (tree:[], edges:[])
	// on the "no goals file yet" path — a UI iterating edges/tree never hits null.
	return &GoalsDoc{Tree: []*GoalNode{}, Edges: []Edge{}, Rollups: map[string]string{}, GeneratedAt: nowRFC3339()}
}

// nowRFC3339 is overridable in tests for a deterministic generated_at.
var nowRFC3339 = func() string { return time.Now().UTC().Format(time.RFC3339) }

// SortedRollupIDs is a small helper for deterministic test output.
func SortedRollupIDs(m map[string]string) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
