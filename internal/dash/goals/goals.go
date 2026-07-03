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
	ID            string      `json:"id" yaml:"id"`
	Title         string      `json:"title" yaml:"title"`
	Scope         string      `json:"scope,omitempty" yaml:"scope"` // fleet | project | task
	Parent        string      `json:"parent,omitempty" yaml:"parent"`
	Owner         string      `json:"owner,omitempty" yaml:"owner"` // coordinator/desk role (generic)
	Status        string      `json:"status" yaml:"status"`         // active | achieved | paused | cancelled (authored)
	StatusDisplay string      `json:"status_display" yaml:"-"`      // computed roll-up (never from yaml)
	DependsOn     []string    `json:"depends_on,omitempty" yaml:"depends_on"`
	WorkItems     []WorkItem  `json:"work_items,omitempty" yaml:"work_items"`
	Children      []*GoalNode `json:"children,omitempty" yaml:"children"`
}

// WorkItem binds a node to concrete work. Kind is issue|backlog|inline|desk; the
// binding field depends on the kind (ref for issue/desk, marker for backlog, text
// for inline). Status is the minimal derived state (from the backlog marker
// tokens) the UI shows without itself calling gh.
type WorkItem struct {
	Kind   string `json:"kind" yaml:"kind"`
	Ref    string `json:"ref,omitempty" yaml:"ref"`
	Marker string `json:"marker,omitempty" yaml:"marker"`
	Text   string `json:"text,omitempty" yaml:"text"`
	Status string `json:"status,omitempty" yaml:"-"` // derived (never from yaml): blocked|awaiting|in-flight|done|""
}

// GoalsDoc is the /api/goals response: the goal tree, a flat id→status_display
// roll-up map (for cheap lookups + the dependency-line coloring), and the "as of"
// stamp. Matches design §6.1.
type GoalsDoc struct {
	Tree        []*GoalNode       `json:"tree"`
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
	if err := validateAcyclic(roots); err != nil {
		return nil, err
	}
	rollups := map[string]string{}
	for _, n := range roots {
		compute(n, rollups)
	}
	return &GoalsDoc{Tree: roots, Rollups: rollups, GeneratedAt: nowRFC3339()}, nil
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
func validateAcyclic(roots []*GoalNode) error {
	seen := map[string]bool{}
	var walk func(n *GoalNode, ancestors map[string]bool) error
	walk = func(n *GoalNode, ancestors map[string]bool) error {
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
// in rollups. Implements design §4.4: blocked wins, then awaiting (operator-gated),
// then in-flight, then achieved, else active. Reuses the backlog marker tokens.
func compute(n *GoalNode, rollups map[string]string) string {
	for i := range n.WorkItems {
		n.WorkItems[i].Status = itemStatus(n.WorkItems[i])
	}
	childBlocked, childAwaiting, childInFlight := false, false, false
	allChildrenAchieved := len(n.Children) > 0
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
		if cd != "achieved" {
			allChildrenAchieved = false
		}
	}
	itemBlocked, itemAwaiting, itemInFlight, itemsDone := false, false, false, true
	for _, wi := range n.WorkItems {
		switch wi.Status {
		case "blocked":
			itemBlocked = true
			itemsDone = false
		case "awaiting":
			itemAwaiting = true
			itemsDone = false
		case "in-flight":
			itemInFlight = true
			itemsDone = false
		case "done":
			// counts as done
		default:
			itemsDone = false
		}
	}

	var display string
	switch {
	case childBlocked || itemBlocked:
		display = "blocked"
	case childAwaiting || itemAwaiting:
		display = "awaiting"
	case childInFlight || itemInFlight:
		display = "in-flight"
	case n.Status == "achieved" || (allChildrenAchieved && (len(n.WorkItems) == 0 || itemsDone)):
		display = "achieved"
	default:
		display = "active"
	}
	n.StatusDisplay = display
	rollups[n.ID] = display
	return display
}

// itemStatus derives a minimal work-item status from its binding — WITHOUT calling
// gh (that richer resolution is flotilla-dev's core). Backlog markers carry the
// tokens (reuses the backlog.Parse vocabulary); other kinds are neutral.
func itemStatus(wi WorkItem) string {
	m := strings.ToLower(wi.Marker)
	switch {
	case strings.Contains(m, "[blocked]") || strings.Contains(m, "[needs-attention]"):
		return "blocked"
	case strings.Contains(m, "[awaiting-auth]"):
		return "awaiting"
	case strings.Contains(m, "[in-flight]"):
		return "in-flight"
	case strings.Contains(m, "[done]"):
		return "done"
	default:
		return ""
	}
}

func emptyDoc() *GoalsDoc {
	return &GoalsDoc{Tree: []*GoalNode{}, Rollups: map[string]string{}, GeneratedAt: nowRFC3339()}
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
