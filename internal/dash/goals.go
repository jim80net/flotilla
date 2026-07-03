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
	Class  string `json:"class"`  // done | in-flight | awaiting | blocked | active | unknown
	Detail string `json:"detail"` // live state word (desk state, backlog marker, issue state, …)
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
// the counts, and honest absent/error messaging (the dash never fabricates a tree).
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
	return doc
}

// resolveItem binds one work item to its live status. This is the Stage-2 live-binding core: a
// desk item reads the board's current state for that agent; a backlog item resolves against the
// backlog markdown; an issue item reads the (optional) resolved issue state; an inline item carries
// its coordinator-set done flag.
func resolveItem(wi WorkItem, in GoalsInputs) RenderedWorkItem {
	r := RenderedWorkItem{Kind: string(wi.Kind), Label: itemLabel(wi)}
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

// scopeOf returns the declared scope (normalized to the canonical enum), or infers one from depth
// when the file omits it (0 → fleet, 1 → project, ≥2 → task).
func scopeOf(declared GoalScope, depth int) GoalScope {
	if declared != "" {
		return normalizeScope(declared)
	}
	switch depth {
	case 0:
		return ScopeFleet
	case 1:
		return ScopeProject
	default:
		return ScopeTask
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

// normalizeScope maps scopes onto roll-up buckets (legacy desk leaf alias → task).
func normalizeScope(s GoalScope) GoalScope {
	switch s {
	case ScopeFleet, ScopeFlotilla:
		return ScopeFlotilla
	case ScopeProject:
		return ScopeOrgDesk
	case ScopeOrgDesk: // legacy leaf alias; v2 org desk disambiguated in displayScope
		return ScopeTask
	default:
		return s
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
