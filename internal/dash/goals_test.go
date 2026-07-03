package dash

import (
	"strings"
	"testing"
)

// --- ParseGoalsFile: structural validation (fail-closed) ---

func TestParseGoalsFile_Valid(t *testing.T) {
	data := []byte(`{
	  "version": 1,
	  "default_view": true,
	  "goals": [
	    {"id": "trading", "title": "Trading", "scope": "fleet"},
	    {"id": "eqo", "title": "Equities", "scope": "project", "parent": "trading",
	     "work_items": [{"kind": "desk", "agent": "tactical-head"}]}
	  ]
	}`)
	gf, err := ParseGoalsFile(data)
	if err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}
	if !gf.DefaultView || len(gf.Goals) != 2 || gf.Goals[1].Parent != "trading" {
		t.Fatalf("parsed shape wrong: %+v", gf)
	}
	if gf.Goals[1].WorkItems[0].Agent != "tactical-head" {
		t.Fatalf("work item not parsed: %+v", gf.Goals[1].WorkItems)
	}
}

func TestParseGoalsFile_RejectsCycle(t *testing.T) {
	data := []byte(`{"goals":[
	  {"id":"a","title":"A","parent":"b"},
	  {"id":"b","title":"B","parent":"a"}
	]}`)
	if _, err := ParseGoalsFile(data); err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected acyclicity failure, got %v", err)
	}
}

func TestParseGoalsFile_RejectsSelfParent(t *testing.T) {
	data := []byte(`{"goals":[{"id":"a","title":"A","parent":"a"}]}`)
	if _, err := ParseGoalsFile(data); err == nil {
		t.Fatal("a self-parent is a cycle and must be rejected")
	}
}

func TestParseGoalsFile_RejectsDuplicateID(t *testing.T) {
	data := []byte(`{"goals":[{"id":"a","title":"A"},{"id":"a","title":"A2"}]}`)
	if _, err := ParseGoalsFile(data); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-id failure, got %v", err)
	}
}

func TestParseGoalsFile_RejectsUnknownParent(t *testing.T) {
	data := []byte(`{"goals":[{"id":"a","title":"A","parent":"ghost"}]}`)
	if _, err := ParseGoalsFile(data); err == nil || !strings.Contains(err.Error(), "unknown parent") {
		t.Fatalf("expected unknown-parent failure, got %v", err)
	}
}

func TestParseGoalsFile_RejectsEmptyID(t *testing.T) {
	data := []byte(`{"goals":[{"id":"","title":"A"}]}`)
	if _, err := ParseGoalsFile(data); err == nil {
		t.Fatal("an empty id must be rejected")
	}
}

func TestParseGoalsFile_RejectsBadJSON(t *testing.T) {
	if _, err := ParseGoalsFile([]byte(`{not json`)); err == nil {
		t.Fatal("malformed JSON must error")
	}
}

func TestParseGoalsFile_ToleratesUnknownFields(t *testing.T) {
	// Forward-compat with the yaml-source authoring lane (may add fields the reader ignores).
	data := []byte(`{"goals":[{"id":"a","title":"A","future_field":123}],"unknown_top":true}`)
	if _, err := ParseGoalsFile(data); err != nil {
		t.Fatalf("unknown fields must be tolerated, got %v", err)
	}
}

// --- BuildGoals: absent / error inputs are honest, never fabricated ---

func TestBuildGoals_Absent(t *testing.T) {
	doc := BuildGoals(GoalsInputs{FileOK: false})
	if doc.Found || doc.Message == "" || len(doc.Goals) != 0 {
		t.Fatalf("absent goals should be Found=false with a message, got %+v", doc)
	}
}

func TestBuildGoals_LoadError(t *testing.T) {
	doc := BuildGoals(GoalsInputs{LoadErr: "goals: cyclic parent chain detected at goal \"a\""})
	if doc.Found || doc.Error == "" {
		t.Fatalf("a load error must surface honestly, got %+v", doc)
	}
}

// --- BuildGoals: live desk binding (the Stage-2 core) ---

func TestBuildGoals_DeskLiveBinding(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "g", Title: "G", WorkItems: []WorkItem{
			{Kind: WorkDesk, Agent: "builder"},
			{Kind: WorkDesk, Agent: "asker"},
			{Kind: WorkDesk, Agent: "faulted"},
			{Kind: WorkDesk, Agent: "resting"},
			{Kind: WorkDesk, Agent: "ghost"},
		}},
	}}
	desks := map[string]string{
		"builder": "working",
		"asker":   "awaiting-input",
		"faulted": "crashed",
		"resting": "idle",
		// "ghost" absent → unknown
	}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, DeskStates: desks})
	items := doc.Goals[0].WorkItems
	want := []struct{ detail, class string }{
		{"working", "in-flight"},
		{"awaiting-input", "awaiting"},
		{"crashed", "blocked"},
		{"idle", "active"},
		{"unknown", "unknown"},
	}
	for i, w := range want {
		if items[i].Detail != w.detail || items[i].Class != w.class {
			t.Errorf("desk item %d = (%q,%q), want (%q,%q)", i, items[i].Detail, items[i].Class, w.detail, w.class)
		}
	}
	// A crashed desk is the most salient → the node rolls up blocked.
	if doc.Goals[0].Rollup != "blocked" || doc.Goals[0].State != "blocked" {
		t.Errorf("node with a crashed desk should be blocked, got rollup=%q state=%q", doc.Goals[0].Rollup, doc.Goals[0].State)
	}
}

// --- BuildGoals: backlog binding ---

func TestBuildGoals_BacklogBinding(t *testing.T) {
	md := "## Backlog\n- [in-flight] wire the goals view\n- [blocked] operator sign-off\n"
	file := GoalsFile{Goals: []Goal{
		{ID: "flight", Title: "Flight", WorkItems: []WorkItem{{Kind: WorkBacklog, Match: "goals view"}}},
		{ID: "gate", Title: "Gate", WorkItems: []WorkItem{{Kind: WorkBacklog, Match: "sign-off"}}},
		{ID: "miss", Title: "Miss", WorkItems: []WorkItem{{Kind: WorkBacklog, Match: "not present"}}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, Backlog: md})
	byID := indexByID(doc.Goals)
	if byID["flight"].Rollup != "in-flight" {
		t.Errorf("in-flight backlog item should roll up in-flight, got %q", byID["flight"].Rollup)
	}
	if byID["gate"].Rollup != "awaiting" || byID["gate"].State != "awaiting" {
		t.Errorf("blocked backlog item should be awaiting, got rollup=%q", byID["gate"].Rollup)
	}
	if byID["miss"].WorkItems[0].Class != "unknown" {
		t.Errorf("an unmatched backlog item should be unknown, got %q", byID["miss"].WorkItems[0].Class)
	}
}

// --- BuildGoals: roll-up salience + declared status ---

func TestBuildGoals_RollupSalience(t *testing.T) {
	// A parent with three children: one blocked, one in-flight, one achieved → blocked wins.
	file := GoalsFile{Goals: []Goal{
		{ID: "root", Title: "Root"},
		{ID: "c1", Title: "C1", Parent: "root", WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "x"}}}, // crashed → blocked
		{ID: "c2", Title: "C2", Parent: "root", WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "y"}}}, // working → in-flight
		{ID: "c3", Title: "C3", Parent: "root", Status: StatusAchieved},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, DeskStates: map[string]string{"x": "crashed", "y": "working"}})
	byID := indexByID(doc.Goals)
	if byID["root"].Rollup != "blocked" {
		t.Errorf("root should roll up blocked (most salient child), got %q", byID["root"].Rollup)
	}
	if byID["c3"].Rollup != "achieved" || byID["c3"].State != "realized" {
		t.Errorf("declared-achieved child should be realized, got rollup=%q state=%q", byID["c3"].Rollup, byID["c3"].State)
	}
}

func TestBuildGoals_AchievedWhenAllChildrenAchieved(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "root", Title: "Root"},
		{ID: "c1", Title: "C1", Parent: "root", Status: StatusAchieved},
		{ID: "c2", Title: "C2", Parent: "root", WorkItems: []WorkItem{{Kind: WorkInline, Text: "done thing", Done: true}}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if r := indexByID(doc.Goals)["root"].Rollup; r != "achieved" {
		t.Errorf("root with all children/items done should be achieved, got %q", r)
	}
}

func TestBuildGoals_DeclaredPausedAndCancelled(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "p", Title: "P", Status: StatusPaused},
		{ID: "x", Title: "X", Status: StatusCancelled},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	byID := indexByID(doc.Goals)
	if byID["p"].State != "paused" || byID["x"].State != "cancelled" {
		t.Errorf("declared paused/cancelled should map to their states, got %q / %q", byID["p"].State, byID["x"].State)
	}
}

func TestBuildGoals_AspirationalWhenEmptyActive(t *testing.T) {
	file := GoalsFile{Goals: []Goal{{ID: "dream", Title: "Someday"}}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if doc.Goals[0].State != "aspirational" {
		t.Errorf("an active node with no items/children is aspirational, got %q", doc.Goals[0].State)
	}
}

// --- BuildGoals: emission order, depth, scope inference, counts ---

func TestBuildGoals_OrderDepthAndScope(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "b-project", Title: "B", Parent: "a-fleet"},
		{ID: "a-fleet", Title: "A"},
		{ID: "c-desk", Title: "C", Parent: "b-project"},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	// Depth-first from roots (file order of roots): a-fleet, then its subtree b-project, c-desk.
	gotIDs := []string{doc.Goals[0].ID, doc.Goals[1].ID, doc.Goals[2].ID}
	wantIDs := []string{"a-fleet", "b-project", "c-desk"}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("emission order = %v, want %v (parent must precede children)", gotIDs, wantIDs)
		}
	}
	byID := indexByID(doc.Goals)
	if byID["a-fleet"].Depth != 0 || byID["b-project"].Depth != 1 || byID["c-desk"].Depth != 2 {
		t.Errorf("depths wrong: %d/%d/%d", byID["a-fleet"].Depth, byID["b-project"].Depth, byID["c-desk"].Depth)
	}
	// Scope inferred from depth when unset.
	if byID["a-fleet"].Scope != "fleet" || byID["b-project"].Scope != "project" || byID["c-desk"].Scope != "desk" {
		t.Errorf("scope inference wrong: %q/%q/%q", byID["a-fleet"].Scope, byID["b-project"].Scope, byID["c-desk"].Scope)
	}
	if byID["a-fleet"].Children[0] != "b-project" {
		t.Errorf("children not wired: %+v", byID["a-fleet"].Children)
	}
}

func TestBuildGoals_Counts(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "f", Title: "F", Scope: ScopeFleet, Status: StatusAchieved},
		{ID: "p1", Title: "P1", Scope: ScopeProject, WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "w"}}},
		{ID: "p2", Title: "P2", Scope: ScopeProject}, // empty active → aspirational
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, DeskStates: map[string]string{"w": "working"}})
	c := doc.Counts
	if c.Total != 3 || c.Fleet != 1 || c.Project != 2 {
		t.Errorf("scope counts wrong: %+v", c)
	}
	if c.Realized != 1 || c.InFlight != 1 || c.Aspirational != 1 {
		t.Errorf("state counts wrong: %+v", c)
	}
}

func TestBuildGoals_IssueLinkedAndResolved(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "g", Title: "G", WorkItems: []WorkItem{
			{Kind: WorkIssue, Ref: "owner/repo#1"},
			{Kind: WorkIssue, Ref: "owner/repo#2"},
		}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, IssueStates: map[string]string{"owner/repo#2": "closed"}})
	items := doc.Goals[0].WorkItems
	if items[0].Detail != "linked" || items[0].Class != "active" {
		t.Errorf("unresolved issue should be linked/active, got %+v", items[0])
	}
	if items[1].Detail != "closed" || items[1].Class != "done" {
		t.Errorf("resolved closed issue should be done, got %+v", items[1])
	}
}

// indexByID is a test helper mapping a rendered goals slice by id.
func indexByID(goals []RenderedGoal) map[string]RenderedGoal {
	m := make(map[string]RenderedGoal, len(goals))
	for _, g := range goals {
		m[g.ID] = g
	}
	return m
}
