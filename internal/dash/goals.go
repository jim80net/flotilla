package dash

// The Goals view — the fleet's PURPOSE hierarchy. Where the fleet board (BuildBoard) shows fleet
// STATE (who is where), the goals DAG shows fleet PURPOSE: goal nodes form an acyclic hierarchy
// (v1: a validated tree), desk/backlog/issue/inline WORK ITEMS attach to nodes, and each node's
// roll-up status + a visual state are computed AT READ TIME from the live board snapshot + the
// backlog the fleet already tracks. This is Stage 1 (static structure from a goals file) + Stage 2
// (live binding of work-item state) of the goals-dashboard build; event choreography (the feed
// pulses) and the edit surface are separate lanes (the dash-next-gen openspec design, #267/#268).
//
// The data model follows the ratified dash-next-gen `goals` spec: a roster-adjacent goals file of
// nodes (id/title/description/scope/parent/owner/status) carrying work_items (issue/backlog/inline/
// desk). This file is the PURE read model — ParseGoalsFile validates the file (acyclic, fail-closed)
// and BuildGoals assembles the rendered document over already-loaded inputs (the goals file, the
// backlog markdown, the live desk states, optional issue states). The HTTP layer does the file I/O
// and supplies the loaded values, mirroring BuildBoard/BuildHistory — so every builder here is
// unit-testable with in-memory inputs and no file, no daemon, no real clock. The dash remains a
// PURE READER; `flotilla watch` stays the single writer (design §2).

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/roster"
)

// GoalScope is a node's altitude in the hierarchy — the column it renders in (fleet → project →
// desk). It is optional in the file; when omitted BuildGoals infers it from the node's depth.
type GoalScope string

const (
	ScopeFleet    GoalScope = "fleet"    // v1 input
	ScopeFlotilla GoalScope = "flotilla" // v2 org top-level
	ScopeProject  GoalScope = "project"  // v1 input
	ScopeOrgDesk  GoalScope = "desk"     // v2 org mid-level (ambiguous with legacy leaf alias)
	ScopeTask     GoalScope = "task"     // canonical leaf altitude
	// ScopeDeskLeaf is the pre-ratification alias for ScopeTask at leaf depth.
	ScopeDeskLeaf GoalScope = "desk"
)

// GoalStatus is a node's declared lifecycle status (distinct from the COMPUTED roll-up). `active`
// is the default; `achieved`/`paused`/`cancelled` are coordinator-set terminal/hold states.
type GoalStatus string

const (
	StatusActive    GoalStatus = "active"
	StatusAchieved  GoalStatus = "achieved"
	StatusPaused    GoalStatus = "paused"
	StatusCancelled GoalStatus = "cancelled"
)

// WorkItemKind is the kind of work attached to a goal node. `desk` binds live (an agent's board
// state); `backlog` resolves against the fleet backlog markdown; `issue` references a GitHub issue
// (`owner/repo#N`); `inline` is a coordinator checklist line carried verbatim.
type WorkItemKind string

const (
	WorkDesk    WorkItemKind = "desk"
	WorkBacklog WorkItemKind = "backlog"
	WorkIssue   WorkItemKind = "issue"
	WorkInline  WorkItemKind = "inline"
)

// WorkItem is one unit of work attached to a goal node (the on-disk shape). Exactly the fields the
// kind needs are set; the rest stay empty. Unknown fields in the file are ignored (forward-compat
// with the yaml-source authoring lane, which may add fields the reader does not yet consume).
type WorkItem struct {
	Kind  WorkItemKind `json:"kind"`
	Agent string       `json:"agent,omitempty"` // kind=desk: the agent whose live state this tracks
	Match string       `json:"match,omitempty"` // kind=backlog: substring identifying the backlog line
	Ref   string       `json:"ref,omitempty"`   // kind=issue: owner/repo#N
	Text  string       `json:"text,omitempty"`  // kind=inline: the checklist text
	Done  bool         `json:"done,omitempty"`  // kind=inline: whether the coordinator marked it done
	Label string       `json:"label,omitempty"` // optional display label overriding the derived one
	// Brief is the DECISION PACKAGE for an operator-gated item (markdown): the
	// recommendation, the value, the tradeoff, the ask — everything the operator needs to
	// decide WITHOUT leaving the respond modal (#347). Optional; empty ⇒ the modal shows an
	// honest "no brief yet — ask the desk" state. The desk attaches it when marking the item
	// operator-blocked (that doctrine is the fleet layer's, not the dash's).
	Brief string `json:"brief,omitempty"`
}

// Goal is one goal node (the on-disk shape).
type Goal struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Description       string     `json:"description,omitempty"`
	Scope             GoalScope  `json:"scope,omitempty"`
	Parent            string     `json:"parent,omitempty"`
	Owner             string     `json:"owner,omitempty"`
	Status            GoalStatus `json:"status,omitempty"`
	ConversationAgent string     `json:"conversation_agent,omitempty"` // Conversations deep-link target
	TopologyChannelID string     `json:"topology_channel_id,omitempty"`
	Priorities        []string   `json:"priorities,omitempty"`
	Milestones        []string   `json:"milestones,omitempty"`
	DependsOn         []string   `json:"depends_on,omitempty"` // cross-dependency ids (not re-parenting)
	WorkItems         []WorkItem `json:"work_items,omitempty"`
	// Brief is a NODE-level decision package (markdown) — for a decision gated on the node
	// itself rather than a single work item (#347). Same modal render + empty-state rules.
	Brief string `json:"brief,omitempty"`
}

// GoalsFile is the roster-adjacent goals file (`fleet-goals.json`, the compiled cache the dash
// reads; the `fleet-goals.yaml` source → json compile is the watch/authoring lane's half).
type GoalsFile struct {
	Version     int    `json:"version,omitempty"`
	DefaultView bool   `json:"default_view,omitempty"` // promote Goals to the default landing tab
	Goals       []Goal `json:"goals"`
}

// ParseGoalsFile decodes and VALIDATES a goals file: unique non-empty ids, every `parent` resolves
// to a known id, and the parent graph is ACYCLIC. Validation fails CLOSED — a cycle or a dangling
// parent returns an error rather than a partially-rendered tree (the spec's acyclicity contract).
// Unknown fields are tolerated (forward-compat); structural violations are not.
func ParseGoalsFile(data []byte) (GoalsFile, error) {
	var gf GoalsFile
	if err := json.Unmarshal(data, &gf); err != nil {
		return GoalsFile{}, fmt.Errorf("goals: parse: %w", err)
	}
	if err := gf.validate(); err != nil {
		return GoalsFile{}, err
	}
	return gf, nil
}

// validate enforces the structural invariants ParseGoalsFile relies on. Kept separate so the rules
// are one place and are unit-testable directly.
func (gf GoalsFile) validate() error {
	ids := make(map[string]bool, len(gf.Goals))
	for _, g := range gf.Goals {
		if strings.TrimSpace(g.ID) == "" {
			return fmt.Errorf("goals: a goal has an empty id (every node needs a unique slug)")
		}
		if ids[g.ID] {
			return fmt.Errorf("goals: duplicate goal id %q", g.ID)
		}
		ids[g.ID] = true
	}
	parent := make(map[string]string, len(gf.Goals))
	for _, g := range gf.Goals {
		if g.Parent != "" && !ids[g.Parent] {
			return fmt.Errorf("goals: goal %q references unknown parent %q", g.ID, g.Parent)
		}
		parent[g.ID] = g.Parent
	}
	// Walk each node's parent chain; a revisited id on one chain is a cycle. O(N·depth), N small.
	for _, g := range gf.Goals {
		seen := map[string]bool{}
		for cur := g.ID; cur != ""; cur = parent[cur] {
			if seen[cur] {
				return fmt.Errorf("goals: cyclic parent chain detected at goal %q (goals must be acyclic)", g.ID)
			}
			seen[cur] = true
		}
	}
	for _, g := range gf.Goals {
		seenDep := make(map[string]bool, len(g.DependsOn))
		for _, dep := range g.DependsOn {
			if strings.TrimSpace(dep) == "" {
				return fmt.Errorf("goals: goal %q has an empty depends_on entry", g.ID)
			}
			if dep == g.ID {
				return fmt.Errorf("goals: goal %q cannot depend_on itself", g.ID)
			}
			if seenDep[dep] {
				return fmt.Errorf("goals: goal %q has duplicate depends_on entry %q", g.ID, dep)
			}
			seenDep[dep] = true
			if !ids[dep] {
				return fmt.Errorf("goals: goal %q references unknown depends_on target %q", g.ID, dep)
			}
		}
	}
	return nil
}

// --- rendered document (the /api/goals shape the frontend consumes) ---

// RenderedWorkItem is a work item with its live status resolved. Class is the settle-relevant
// bucket driving roll-up + the item chip's color; Detail is the operator-facing status word.
type RenderedWorkItem struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Ref    string `json:"ref,omitempty"`
	Agent  string `json:"agent,omitempty"`
	Class  string `json:"class"`           // done | in-flight | awaiting | blocked | active | unknown
	Detail string `json:"detail"`          // live state word (desk state, backlog marker, issue state, …)
	Brief  string `json:"brief,omitempty"` // decision package (markdown) for a gated item — rendered in the respond modal (#347)
}

// GoalHarness is the read-time harness badge (from roster surface, not stored in YAML).
type GoalHarness struct {
	Surface string `json:"surface,omitempty"`
}

// GoalLayout carries hub-spoke layout hints derived from topology + scope (org graph v2).
type GoalLayout struct {
	HubCenter bool `json:"hub_center,omitempty"`
	Spoke     bool `json:"spoke,omitempty"`
}

// RenderedGoal is a goal node with its resolved work items, computed roll-up, and visual state.
// Depth + Scope + Children let the frontend lay the tree out in altitude columns.
type RenderedGoal struct {
	ID                string             `json:"id"`
	Title             string             `json:"title"`
	Description       string             `json:"description,omitempty"`
	Scope             string             `json:"scope"` // v2 vocabulary: flotilla | desk | task
	Parent            string             `json:"parent,omitempty"`
	Owner             string             `json:"owner,omitempty"`
	ConversationAgent string             `json:"conversation_agent,omitempty"`
	TopologyChannelID string             `json:"topology_channel_id,omitempty"`
	Priorities        []string           `json:"priorities,omitempty"`
	Milestones        []string           `json:"milestones,omitempty"`
	Harness           *GoalHarness       `json:"harness,omitempty"`
	Layout            *GoalLayout        `json:"layout,omitempty"`
	Status            string             `json:"status"`         // coordinator-authored lifecycle
	StatusDisplay     string             `json:"status_display"` // computed roll-up (ratified spec): blocked|awaiting|in-flight|achieved|active|paused|cancelled
	Depth             int                `json:"depth"`
	Children          []string           `json:"children"`
	WorkItems         []RenderedWorkItem `json:"work_items"`
	// Source is empty for a goal authored in the goals file; "roster" for a desk card
	// materialized from the roster/topology (a first-class desk not written as a goal —
	// #324 Inc 2). Lets the UI distinguish live-roster desks and group them (Inc 3).
	Source string `json:"source,omitempty"`
	// Brief is a NODE-level decision package (markdown) rendered in the respond modal (#347).
	Brief string `json:"brief,omitempty"`
}

// GoalsCounts is the situation-bar summary — goal counts by scope and by visual state.
type GoalsCounts struct {
	Total        int `json:"total"`
	Flotilla     int `json:"flotilla"`
	Desk         int `json:"desk"` // org-container count (v2 mid-level)
	Task         int `json:"task"`
	Fleet        int `json:"fleet"`        // legacy mirror of flotilla
	Project      int `json:"project"`      // legacy mirror of desk
	Realized     int `json:"realized"`     // status_display achieved
	InFlight     int `json:"in_flight"`    // status_display in-flight
	Awaiting     int `json:"awaiting"`     // awaiting + blocked — the "needs attention" bucket
	Pending      int `json:"pending"`      // dependency-gated — waiting on an unfinished dependency (#349 Inc 3)
	Aspirational int `json:"aspirational"` // active + paused + cancelled — not yet realized
}

// GoalEdge is a cross-dependency link between goals (depends_on — not a parent edge).
type GoalEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // depends_on
}

// GoalsDoc is the /api/goals document: the rendered nodes (roots first, each parent immediately
// before its children — depth-first — so the frontend can stream columns), cross-dependency edges,
// the counts, and honest absent/error messaging (the dash never fabricates a tree). Roster-
// materialized desk cards (#324 Inc 2) are INSERTED right after their hub node, preserving that
// DFS ordering; hub-less desks (a fleet with no hub node) are appended as trailing roots.
type GoalsDoc struct {
	Version     int            `json:"version,omitempty"`
	Found       bool           `json:"found"`
	DefaultView bool           `json:"default_view"`
	SourcePath  string         `json:"source_path,omitempty"`
	GeneratedAt string         `json:"generated_at,omitempty"`
	Error       string         `json:"error,omitempty"`
	Message     string         `json:"message,omitempty"`
	Goals       []RenderedGoal `json:"goals"` // depth-first tree emission (GoalsDoc.tree alias)
	Edges       []GoalEdge     `json:"edges,omitempty"`
	Counts      GoalsCounts    `json:"counts"`
	// Collaborations groups desk NODES that jointly work one lane, drawn as a dotted
	// container on the org map (#324 Inc 3). Empty when no lane binds ≥2 desks.
	Collaborations []Collaboration `json:"collaborations,omitempty"`
}

// Collaboration is a set of desk nodes jointly working one lane (#324 Inc 3). GROUPING
// MECHANISM — PRIMARY: a goal whose work_items name ≥2 desk agents that have nodes on the
// map (the operator's codex-harness-lane example: one lane goal referencing several desks).
// FALLBACK: a NON-flotilla goal with ≥2 desk-scope child nodes (shared parentage) — the
// flotilla hub is excluded so "every desk under the fleet" is never mistaken for a lane.
type Collaboration struct {
	Lane  string   `json:"lane"`  // the binding goal's title (or id)
	Desks []string `json:"desks"` // node ids of the collaborating desks (≥2)
}

// GoalTrailerIssue is an open issue discovered via a `goal-id:` body trailer (coordinator
// convention). The HTTP layer supplies these from the tracker read path; BuildGoals attaches
// them under the referenced goal node without requiring a hand-edited work_items entry.
type GoalTrailerIssue struct {
	GoalID string // parsed slug from the issue body trailer
	Ref    string // owner/repo#N
	State  string // open|closed (only open trailers are attached today)
}

// GoalsInputs are the already-loaded values BuildGoals renders. Keeping the builder pure (no I/O,
// no clock) is what makes the goals read model unit-testable, exactly like BoardInputs.
type GoalsInputs struct {
	File        GoalsFile         // the parsed goals file (zero value when absent)
	FileOK      bool              // a goals file was present and parsed
	LoadErr     string            // non-empty when a file existed but failed to parse/validate (surfaced honestly)
	SourcePath  string            // path the goals file was read from (HTTP layer only)
	GeneratedAt string            // RFC3339 UTC stamp when the doc was built (HTTP layer only)
	Backlog     string            // the fleet backlog markdown (for kind=backlog resolution)
	DeskStates  map[string]string // agent name (lowercased) → live board state label (for kind=desk)
	IssueStates map[string]string // "owner/repo#N" → "open"|"closed" (optional; empty when the tracker is off)
	// TrailerIssues are open issues carrying a goal-id: trailer (tracker read path). Each is
	// merged onto the matching goal node's work_items at render time.
	TrailerIssues []GoalTrailerIssue
	AgentSurfaces map[string]string // agent name (lowercased) → harness surface
	MetaXO        string            // federation hub agent for layout hints
	// Channels is the roster/topology membership used to materialize per-desk cards
	// (#324 Inc 2): every desk that is a channel member but NOT authored as a goal node
	// still gets a first-class card on the map. Empty ⇒ no materialization (authored
	// goals only), so the feature degrades cleanly when the roster has no bindings.
	Channels []DeskChannel
}

// DeskChannel is the minimal channel membership BuildGoals needs to materialize desk
// cards — kept local so BuildGoals stays pure (no roster dependency); loadGoals maps
// roster.Bindings() into it.
type DeskChannel struct {
	ChannelID string
	XOAgent   string
	Members   []string
}

// BuildGoals assembles the goals document. Pure: no I/O, no real time. Absent/error inputs produce
// an honest Found=false document with an operator-facing message, never a fabricated tree.
func BuildGoals(in GoalsInputs) GoalsDoc {
	if in.LoadErr != "" {
		return GoalsDoc{Found: false, Error: in.LoadErr,
			Message: "the goals file could not be loaded — fix it and the Goals view will render (structure is validated fail-closed)"}
	}
	if !in.FileOK {
		return GoalsDoc{Found: false,
			Message: "no goals file yet — create fleet-goals.json (roster-adjacent) to render the fleet's goal hierarchy here"}
	}

	byID := make(map[string]*Goal, len(in.File.Goals))
	children := make(map[string][]string, len(in.File.Goals))
	roots := make([]string, 0, len(in.File.Goals))
	for i := range in.File.Goals {
		g := &in.File.Goals[i]
		byID[g.ID] = g
	}
	for i := range in.File.Goals {
		g := &in.File.Goals[i]
		if g.Parent == "" {
			roots = append(roots, g.ID)
		} else {
			children[g.Parent] = append(children[g.Parent], g.ID)
		}
	}

	// Resolve every node's work items once (live binding happens here).
	resolved := make(map[string][]RenderedWorkItem, len(in.File.Goals))
	for id, g := range byID {
		items := make([]RenderedWorkItem, 0, len(g.WorkItems))
		for _, wi := range g.WorkItems {
			items = append(items, resolveItem(wi, in))
		}
		resolved[id] = items
	}
	mergeTrailerIssues(byID, resolved, in)

	// Roll-up is memoized over the (acyclic) tree — each node computed once.
	rollup := make(map[string]string, len(in.File.Goals))
	var computeRollup func(id string) string
	computeRollup = func(id string) string {
		if r, ok := rollup[id]; ok {
			return r
		}
		r := nodeRollup(byID[id], resolved[id], children[id], computeRollup)
		rollup[id] = r
		return r
	}

	version := in.File.Version
	if version == 0 {
		version = 1
	}
	doc := GoalsDoc{
		Found:       true,
		Version:     version,
		DefaultView: in.File.DefaultView,
		SourcePath:  in.SourcePath,
		GeneratedAt: in.GeneratedAt,
		Edges:       buildDependsOnEdges(in.File.Goals),
		Goals:       make([]RenderedGoal, 0, len(in.File.Goals)),
	}
	// Emit depth-first from roots (file order) so a parent always precedes its children.
	var emit func(id string, depth int)
	emit = func(id string, depth int) {
		g := byID[id]
		r := computeRollup(id)
		items := resolved[id]
		scope := displayScope(g.Scope, depth)
		node := RenderedGoal{
			ID: g.ID, Title: g.Title, Description: g.Description,
			Scope: scope, Parent: g.Parent, Owner: g.Owner,
			ConversationAgent: strings.TrimSpace(g.ConversationAgent),
			TopologyChannelID: strings.TrimSpace(g.TopologyChannelID),
			Priorities:        append([]string(nil), g.Priorities...),
			Milestones:        append([]string(nil), g.Milestones...),
			Harness:           harnessFor(g, in.AgentSurfaces),
			Layout:            layoutFor(g, scope, depth, in.MetaXO),
			Status:            string(statusOrDefault(g.Status)),
			StatusDisplay:     r,
			Depth:             depth,
			Children:          append([]string(nil), children[id]...),
			WorkItems:         items,
			Brief:             g.Brief,
		}
		doc.Goals = append(doc.Goals, node)
		countNode(&doc.Counts, node)
		for _, c := range children[id] {
			emit(c, depth+1)
		}
	}
	for _, id := range roots {
		emit(id, 0)
	}
	relabelPending(&doc, byID)
	materializeRosterDesks(&doc, in)
	doc.Collaborations = buildCollaborations(&doc)
	return doc
}

// buildCollaborations derives the desk collaboration groups (#324 Inc 3). See the
// Collaboration type for the mechanism (work-item refs primary, shared parentage fallback).
func buildCollaborations(doc *GoalsDoc) []Collaboration {
	// desk agent (lowercased) → its node id; node id → scope (for the fallback).
	agentNode := make(map[string]string)
	nodeScope := make(map[string]string, len(doc.Goals))
	for i := range doc.Goals {
		g := &doc.Goals[i]
		nodeScope[g.ID] = g.Scope
		if g.Scope != "desk" {
			continue
		}
		if o := strings.ToLower(strings.TrimSpace(g.Owner)); o != "" {
			agentNode[o] = g.ID
		}
		if c := strings.ToLower(strings.TrimSpace(g.ConversationAgent)); c != "" {
			agentNode[c] = g.ID
		}
	}

	var out []Collaboration
	isLane := make(map[string]bool) // goals already emitted as a work-item lane

	// PRIMARY: a goal whose work_items reference ≥2 distinct desk nodes.
	for i := range doc.Goals {
		g := &doc.Goals[i]
		ids := deskNodesFromWorkItems(g.WorkItems, agentNode)
		if len(ids) >= 2 {
			out = append(out, Collaboration{Lane: laneLabel(g), Desks: ids})
			isLane[g.ID] = true
		}
	}
	// FALLBACK: a non-flotilla goal (the hub is excluded) with ≥2 desk-scope children.
	for i := range doc.Goals {
		g := &doc.Goals[i]
		if isLane[g.ID] || g.Scope == "flotilla" {
			continue
		}
		ids := make([]string, 0, len(g.Children))
		for _, cid := range g.Children {
			if nodeScope[cid] == "desk" {
				ids = append(ids, cid)
			}
		}
		if len(ids) >= 2 {
			out = append(out, Collaboration{Lane: laneLabel(g), Desks: ids})
		}
	}
	return out
}

// deskNodesFromWorkItems returns the node ids for the distinct desk agents referenced by a
// goal's work items (first-seen order), skipping agents with no node on the map.
func deskNodesFromWorkItems(items []RenderedWorkItem, agentNode map[string]string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, wi := range items {
		if wi.Kind != string(WorkDesk) || strings.TrimSpace(wi.Agent) == "" {
			continue
		}
		id, ok := agentNode[strings.ToLower(strings.TrimSpace(wi.Agent))]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func laneLabel(g *RenderedGoal) string {
	if t := strings.TrimSpace(g.Title); t != "" {
		return t
	}
	return g.ID
}

// materializeRosterDesks adds a first-class card for every roster desk that is a channel
// MEMBER but not already represented as an authored goal node (#324 Inc 2). Each becomes
// a scope=desk RenderedGoal parented under its channel's hub, with live harness + status
// from the same board the rest of the view reads — so desks are first-class citizens of
// the command structure, not only goals-file nodes. Degrades to a no-op when the roster
// has no channel bindings.
func materializeRosterDesks(doc *GoalsDoc, in GoalsInputs) {
	if len(in.Channels) == 0 {
		return
	}
	// Agents already on the map (authored as a goal's owner or conversation_agent) must
	// NOT get a duplicate card. The XO is the hub, not a desk card.
	represented := make(map[string]bool)
	for i := range doc.Goals {
		g := &doc.Goals[i]
		if o := strings.ToLower(strings.TrimSpace(g.Owner)); o != "" {
			represented[o] = true
		}
		if c := strings.ToLower(strings.TrimSpace(g.ConversationAgent)); c != "" {
			represented[c] = true
		}
	}
	// Every node id already in use — synthetic desk ids MUST NOT collide with an authored
	// id (a client keys on id; a duplicate would silently clobber). usedID reserves the
	// chosen id and suffixes on collision so the desk still appears with a unique id.
	usedID := make(map[string]bool, len(doc.Goals))
	for i := range doc.Goals {
		usedID[doc.Goals[i].ID] = true
	}
	uniqueID := func(base string) string {
		id := base
		for n := 2; usedID[id]; n++ {
			id = base + "-" + strconv.Itoa(n)
		}
		usedID[id] = true
		return id
	}

	// Collect the desks to add, grouped by their hub, so they can be INSERTED right after
	// their hub node in doc.Goals — keeping the documented DFS ordering (a parent always
	// immediately precedes its children) rather than appended at the end.
	byHub := make(map[string][]RenderedGoal)
	var noHub []RenderedGoal
	seen := make(map[string]bool) // a desk in multiple channels is materialized once
	for _, ch := range in.Channels {
		hubID, hubDepth := deskHubFor(doc, ch, in.MetaXO)
		xo := strings.ToLower(strings.TrimSpace(ch.XOAgent))
		for _, m := range ch.Members {
			name := strings.TrimSpace(m)
			key := strings.ToLower(name)
			if key == "" || key == xo || represented[key] || seen[key] {
				continue
			}
			seen[key] = true
			node := RenderedGoal{
				ID:                uniqueID("desk:" + name),
				Title:             name,
				Scope:             "desk",
				Owner:             name,
				ConversationAgent: name,
				Parent:            hubID,
				Harness:           harnessSurface(in.AgentSurfaces, key),
				Status:            string(StatusActive),
				StatusDisplay:     deskDisplayStatus(in.DeskStates[key]),
				Depth:             hubDepth + 1,
				Children:          []string{},
				WorkItems:         []RenderedWorkItem{},
				Source:            "roster",
			}
			countNode(&doc.Counts, node)
			if hubID == "" {
				noHub = append(noHub, node) // no hub → a root, emitted at the end
			} else {
				byHub[hubID] = append(byHub[hubID], node)
			}
		}
	}
	if len(byHub) == 0 && len(noHub) == 0 {
		return
	}

	// Rebuild the stream: each hub node, then its materialized desks immediately after it
	// (and recorded as its children); hub-less desks become trailing roots.
	out := make([]RenderedGoal, 0, len(doc.Goals)+len(noHub))
	for i := range doc.Goals {
		g := doc.Goals[i]
		if desks := byHub[g.ID]; len(desks) > 0 {
			for _, d := range desks {
				g.Children = append(g.Children, d.ID)
			}
			out = append(out, g)
			out = append(out, desks...)
			continue
		}
		out = append(out, g)
	}
	out = append(out, noHub...)
	doc.Goals = out
}

// deskHubFor picks the node a channel's materialized desks attach under: a flotilla goal
// bound to the channel via topology_channel_id, else the layout hub-center, else the first
// depth-0 root. Returns ("", -1) when the map has no hub — the desks then become roots
// (parent "", depth 0) so they still appear rather than vanish.
func deskHubFor(doc *GoalsDoc, ch DeskChannel, metaXO string) (string, int) {
	for i := range doc.Goals {
		if g := &doc.Goals[i]; g.TopologyChannelID != "" && g.TopologyChannelID == ch.ChannelID {
			return g.ID, g.Depth
		}
	}
	for i := range doc.Goals {
		if g := &doc.Goals[i]; g.Layout != nil && g.Layout.HubCenter {
			return g.ID, g.Depth
		}
	}
	for i := range doc.Goals {
		if doc.Goals[i].Depth == 0 {
			return doc.Goals[i].ID, 0
		}
	}
	return "", -1
}

// harnessSurface builds the harness badge for a materialized desk from the roster surface.
func harnessSurface(surfaces map[string]string, key string) *GoalHarness {
	if s := strings.TrimSpace(surfaces[key]); s != "" {
		return &GoalHarness{Surface: s}
	}
	return nil
}

// deskDisplayStatus maps a live board state to a goal status_display token (reusing the
// work-item deskClass mapping); an unknown/absent state shows as "active" — a desk that
// exists but has no live signal is present, not blocked.
func deskDisplayStatus(state string) string {
	c := deskClass(state)
	if c == "unknown" || c == "" {
		return "active"
	}
	return c
}

// resolveItem binds one work item to its live status. This is the Stage-2 live-binding core: a
// desk item reads the board's current state for that agent; a backlog item resolves against the
// backlog markdown; an issue item reads the (optional) resolved issue state; an inline item carries
// its coordinator-set done flag.
func resolveItem(wi WorkItem, in GoalsInputs) RenderedWorkItem {
	r := RenderedWorkItem{Kind: string(wi.Kind), Label: itemLabel(wi), Brief: wi.Brief}
	switch wi.Kind {
	case WorkDesk:
		r.Agent = wi.Agent
		st := in.DeskStates[strings.ToLower(strings.TrimSpace(wi.Agent))]
		if st == "" {
			st = "unknown"
		}
		r.Detail = st
		r.Class = deskClass(st)
	case WorkBacklog:
		if marker, ok := backlog.MatchInBacklog(in.Backlog, wi.Match); ok {
			r.Detail = marker
			r.Class = backlogClass(marker)
		} else {
			// Ratified spec: a linked backlog item ABSENT from the active backlog is done — the
			// backlog is a live drive-queue that drops completed items, so absence means drained.
			r.Detail, r.Class = "done", "done"
		}
	case WorkIssue:
		r.Ref = wi.Ref
		if s, ok := in.IssueStates[strings.TrimSpace(wi.Ref)]; ok {
			r.Detail = s
			r.Class = issueClass(s) // ratified: open → in-flight, closed → done
		} else {
			// Unresolved (the issue tracker is off, or this PR does not resolve live issue state):
			// shown linked + neutral so it never fabricates an in-flight/done it did not verify.
			r.Detail, r.Class = "linked", "active"
		}
	case WorkInline:
		if wi.Done {
			r.Detail, r.Class = "done", "done"
		} else {
			// Ratified spec: an inline item without done:true participates in roll-up as in-flight.
			r.Detail, r.Class = "in progress", "in-flight"
		}
	default:
		r.Detail, r.Class = "unknown", "unknown"
	}
	return r
}

// itemLabel derives a display label for a work item: the explicit Label if set, else a sensible
// per-kind default (the issue ref, the desk agent, the inline text, or the backlog match).
func itemLabel(wi WorkItem) string {
	if s := strings.TrimSpace(wi.Label); s != "" {
		return s
	}
	switch wi.Kind {
	case WorkIssue:
		return wi.Ref
	case WorkDesk:
		return wi.Agent
	case WorkInline:
		return wi.Text
	case WorkBacklog:
		return wi.Match
	default:
		return string(wi.Kind)
	}
}

// deskClass maps a live board desk-state label (surface.State.String, plus the board's "crashed"/
// "unknown") onto a work-item class, per the ratified goals-spec desk row. `working` is in-flight;
// the two `awaiting-*` states are operator/human-gated (awaiting); `errored`/`crashed` are faults
// (blocked). `idle` is left NEUTRAL (active) rather than the spec's "idle ⇒ done": the spec's done
// rule is conditioned on "no in-flight drive-queue items", and this read model has the board state
// but not per-desk drive-queue data — so it does not assert done it cannot verify (idle ⇒ done with
// the drive-queue check is a tracked follow-on). `unknown` is neutral.
func deskClass(state string) string {
	switch state {
	case "working":
		return "in-flight"
	case "awaiting-input", "awaiting-approval":
		return "awaiting"
	case "errored", "crashed":
		return "blocked"
	case "idle":
		return "active"
	default: // unknown, shell, or any future label
		return "unknown"
	}
}

// backlogClass maps a normalized backlog marker (backlog.MatchInBacklog / ClassifyLine) onto a
// work-item class, per the ratified goals-spec table: `[in-flight]`/`[pending]` → in-flight;
// `[blocked]`/`[needs-attention]` → blocked (a genuine block, red — NOT amber awaiting); only
// `[awaiting-auth]` → awaiting (operator-gated, amber); `[done]` → done; `[next]`/malformed are
// neutral (active).
func backlogClass(marker string) string {
	switch marker {
	case "in-flight": // markerOf normalizes [pending] to in-flight
		return "in-flight"
	case "done":
		return "done"
	case "blocked", "needs-attention":
		return "blocked"
	case "awaiting-auth":
		return "awaiting"
	case "next", "malformed":
		return "active"
	default:
		return "unknown"
	}
}

// issueClass maps a GitHub issue state onto a work-item class, per the ratified spec: open → in-flight
// (open issues are active work), closed → done.
func issueClass(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "closed":
		return "done"
	case "open":
		return "in-flight"
	default:
		return "unknown"
	}
}

// nodeRollup computes a node's operator-facing status_display from its authored status, its
// resolved work items, and its children's status_display, following the RATIFIED goals-spec
// precedence (first match wins):
//
//  1. authored cancelled → cancelled
//  2. any child/item blocked → blocked
//  3. any child/item awaiting → awaiting
//  4. authored paused → paused
//  5. any child/item in-flight → in-flight
//  6. authored achieved AND all non-cancelled children achieved (or none) AND all items done (or none) → achieved
//  7. all non-cancelled children achieved AND all items done AND ≥1 child or item exists → achieved
//     (cancelled children are a dead branch, excluded from the achieved test)
//  8. zero children AND zero items → active (the vacuous-achieved guard)
//  9. otherwise → active
func nodeRollup(g *Goal, items []RenderedWorkItem, kids []string, rollupOf func(string) string) string {
	authored := statusOrDefault(g.Status)
	if authored == StatusCancelled { // step 1
		return "cancelled"
	}

	var hasBlocked, hasAwaiting, hasInflight bool
	consideredChildren, achievedChildren := 0, 0 // non-cancelled children (the achieved test)
	for _, c := range kids {
		switch r := rollupOf(c); r {
		case "blocked":
			hasBlocked = true
			consideredChildren++
		case "awaiting":
			hasAwaiting = true
			consideredChildren++
		case "in-flight":
			hasInflight = true
			consideredChildren++
		case "cancelled":
			// dead branch — excluded from the achieved test entirely
		case "achieved":
			consideredChildren++
			achievedChildren++
		default: // active / paused
			consideredChildren++
		}
	}
	itemsDone := 0
	for _, it := range items {
		switch it.Class {
		case "blocked":
			hasBlocked = true
		case "awaiting":
			hasAwaiting = true
		case "in-flight":
			hasInflight = true
		case "done":
			itemsDone++
		}
	}

	if hasBlocked { // step 2
		return "blocked"
	}
	if hasAwaiting { // step 3
		return "awaiting"
	}
	if authored == StatusPaused { // step 4
		return "paused"
	}
	if hasInflight { // step 5
		return "in-flight"
	}
	allChildrenAchieved := consideredChildren == achievedChildren // vacuously true when none
	allItemsDone := itemsDone == len(items)                       // vacuously true when none
	if allChildrenAchieved && allItemsDone {
		if authored == StatusAchieved { // step 6 (allowed even with no structure)
			return "achieved"
		}
		if len(kids)+len(items) > 0 { // step 7 (needs ≥1 child or item)
			return "achieved"
		}
	}
	return "active" // steps 8-9
}

// relabelPending distinguishes dependency-gated from decision-gated (#349 Inc 3): a goal
// that is otherwise "active" (nothing blocked/awaiting/in-flight in its own subtree) but has
// a depends_on target that is NOT yet achieved is waiting on a DEPENDENCY, not on an operator
// decision or a failure — relabel it "pending" (a calmer, distinct state). This is a post-pass
// over the FINISHED rollups: it reads each target's already-computed status_display, so there
// is no recursion and a depends_on cycle cannot loop it.
func relabelPending(doc *GoalsDoc, byID map[string]*Goal) {
	statusByID := make(map[string]string, len(doc.Goals))
	for i := range doc.Goals {
		statusByID[doc.Goals[i].ID] = doc.Goals[i].StatusDisplay
	}
	for i := range doc.Goals {
		g := &doc.Goals[i]
		if g.StatusDisplay != "active" { // only a would-be-ready goal can become pending
			continue
		}
		authored := byID[g.ID]
		if authored == nil {
			continue
		}
		for _, dep := range authored.DependsOn {
			if s, ok := statusByID[dep]; ok && s != "achieved" {
				g.StatusDisplay = "pending"
				doc.Counts.Aspirational-- // it was counted as active → aspirational during emit
				doc.Counts.Pending++
				break
			}
		}
	}
}

// displayScope emits the v2 API scope string (flotilla | desk | task), dual-reading v1 tokens.
func displayScope(declared GoalScope, depth int) string {
	if declared == "" {
		switch depth {
		case 0:
			return "flotilla"
		case 1:
			return "desk"
		default:
			return "task"
		}
	}
	switch declared {
	case ScopeFleet, ScopeFlotilla:
		return "flotilla"
	case ScopeProject:
		return "desk"
	case ScopeTask:
		return "task"
	case ScopeOrgDesk: // v2 org mid-level at depth 1; legacy leaf alias elsewhere
		if depth == 1 {
			return "desk"
		}
		return "task"
	default:
		return string(declared)
	}
}

func harnessFor(g *Goal, surfaces map[string]string) *GoalHarness {
	if len(surfaces) == 0 {
		return nil
	}
	for _, name := range []string{g.ConversationAgent, g.Owner} {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		if surf, ok := surfaces[key]; ok && surf != "" {
			return &GoalHarness{Surface: surf}
		}
	}
	return nil
}

func layoutFor(g *Goal, scope string, depth int, metaXO string) *GoalLayout {
	meta := strings.ToLower(strings.TrimSpace(metaXO))
	owner := strings.ToLower(strings.TrimSpace(g.Owner))
	if meta != "" && owner == meta && depth == 0 {
		return &GoalLayout{HubCenter: true}
	}
	if scope == "flotilla" && depth > 0 {
		return &GoalLayout{Spoke: true}
	}
	if scope == "flotilla" && depth == 0 && meta == "" {
		return &GoalLayout{HubCenter: true}
	}
	return nil
}

// statusOrDefault treats an empty declared status as active.
func statusOrDefault(s GoalStatus) GoalStatus {
	if s == "" {
		return StatusActive
	}
	return s
}

// countNode accumulates the situation-bar counts for one rendered node.
func countNode(c *GoalsCounts, n RenderedGoal) {
	c.Total++
	switch n.Scope {
	case "flotilla":
		c.Flotilla++
		c.Fleet++
	case "desk":
		c.Desk++
		c.Project++
	case "task":
		c.Task++
	}
	switch n.StatusDisplay {
	case "achieved":
		c.Realized++
	case "in-flight":
		c.InFlight++
	case "awaiting", "blocked":
		c.Awaiting++
	case "pending":
		c.Pending++
	case "active", "paused", "cancelled":
		c.Aspirational++
	}
}

// mergeTrailerIssues appends issue work items from goal-id: body trailers onto the referenced
// goal nodes. Authored work_items win on duplicate refs; unknown goal ids are ignored.
func mergeTrailerIssues(byID map[string]*Goal, resolved map[string][]RenderedWorkItem, in GoalsInputs) {
	if len(in.TrailerIssues) == 0 {
		return
	}
	existing := make(map[string]map[string]bool, len(byID))
	for id, items := range resolved {
		refs := make(map[string]bool, len(items))
		for _, it := range items {
			if ref := strings.TrimSpace(it.Ref); ref != "" {
				refs[ref] = true
			}
		}
		existing[id] = refs
	}
	for _, tr := range in.TrailerIssues {
		if byID[tr.GoalID] == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(tr.State), "open") {
			continue
		}
		ref := strings.TrimSpace(tr.Ref)
		if ref == "" || existing[tr.GoalID][ref] {
			continue
		}
		item := resolveItem(WorkItem{Kind: WorkIssue, Ref: ref}, in)
		// mergeTrailerIssues already trusts tr.State as open; when IssueStates is absent
		// resolveItem falls back to linked/active — derive class from the trailer state instead.
		if item.Class == "active" && item.Detail == "linked" {
			if s := strings.TrimSpace(tr.State); s != "" {
				item.Detail = s
				item.Class = issueClass(s)
			}
		}
		resolved[tr.GoalID] = append(resolved[tr.GoalID], item)
		existing[tr.GoalID][ref] = true
	}
}

// buildDependsOnEdges materializes cross-dependency links for GoalsDoc.edges.
func buildDependsOnEdges(goals []Goal) []GoalEdge {
	var edges []GoalEdge
	for _, g := range goals {
		for _, dep := range g.DependsOn {
			edges = append(edges, GoalEdge{From: g.ID, To: dep, Kind: "depends_on"})
		}
	}
	return edges
}

// agentStates flattens a board document's agents into the lowercased name→state map resolveItem's
// desk binding consumes. Kept next to its consumer so the HTTP layer's loadGoals is a thin adapter
// over the board + goals builders (the desk work-item's live status IS the board's desk state).
func agentStates(board BoardDoc) map[string]string {
	m := make(map[string]string, len(board.Agents))
	for _, a := range board.Agents {
		m[strings.ToLower(a.Name)] = a.State
	}
	return m
}

func agentSurfacesFromRoster(cfg *roster.Config) map[string]string {
	if cfg == nil {
		return nil
	}
	m := make(map[string]string, len(cfg.Agents))
	for _, a := range cfg.Agents {
		if surf := strings.TrimSpace(a.Surface); surf != "" {
			m[strings.ToLower(a.Name)] = surf
		}
	}
	return m
}

// deskChannelsFromRoster maps the roster's channel bindings into the minimal DeskChannel
// shape BuildGoals materializes per-desk cards from (#324 Inc 2). Members are copied
// defensively (Bindings() shares the Config's slice header).
func deskChannelsFromRoster(cfg *roster.Config) []DeskChannel {
	if cfg == nil {
		return nil
	}
	bindings := cfg.Bindings()
	out := make([]DeskChannel, 0, len(bindings))
	for _, ch := range bindings {
		members := make([]string, len(ch.Members))
		copy(members, ch.Members)
		out = append(out, DeskChannel{ChannelID: ch.ChannelID, XOAgent: ch.XOAgent, Members: members})
	}
	return out
}
