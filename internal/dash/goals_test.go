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
	// Forward-compat with the ratified spec's newer fields (conversation_agent, depends_on) and the
	// yaml-source authoring lane — unknown fields are ignored, not rejected.
	data := []byte(`{"goals":[{"id":"a","title":"A","conversation_agent":"x","depends_on":["b"],"future":1},{"id":"b","title":"B"}]}`)
	if _, err := ParseGoalsFile(data); err != nil {
		t.Fatalf("unknown/newer fields must be tolerated, got %v", err)
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
	// A crashed desk is the most salient → the node's status_display is blocked.
	if doc.Goals[0].StatusDisplay != "blocked" {
		t.Errorf("node with a crashed desk should be blocked, got %q", doc.Goals[0].StatusDisplay)
	}
}

// --- BuildGoals: backlog binding (ratified marker mapping) ---

func TestBuildGoals_BacklogBinding(t *testing.T) {
	md := "## Backlog\n- [in-flight] wire the goals view\n- [blocked] operator sign-off\n- [awaiting-auth] go/no-go\n"
	file := GoalsFile{Goals: []Goal{
		{ID: "flight", Title: "Flight", WorkItems: []WorkItem{{Kind: WorkBacklog, Match: "goals view"}}},
		{ID: "blk", Title: "Blocked", WorkItems: []WorkItem{{Kind: WorkBacklog, Match: "sign-off"}}},
		{ID: "gate", Title: "Gate", WorkItems: []WorkItem{{Kind: WorkBacklog, Match: "go/no-go"}}},
		{ID: "absent", Title: "Absent", WorkItems: []WorkItem{{Kind: WorkBacklog, Match: "not present"}}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, Backlog: md})
	byID := indexByID(doc.Goals)
	if byID["flight"].StatusDisplay != "in-flight" {
		t.Errorf("[in-flight] backlog → in-flight, got %q", byID["flight"].StatusDisplay)
	}
	if byID["blk"].StatusDisplay != "blocked" {
		t.Errorf("[blocked] backlog → blocked (red, NOT awaiting), got %q", byID["blk"].StatusDisplay)
	}
	if byID["gate"].StatusDisplay != "awaiting" {
		t.Errorf("[awaiting-auth] backlog → awaiting (amber), got %q", byID["gate"].StatusDisplay)
	}
	// Ratified spec: a linked backlog item ABSENT from the active backlog is done (drained).
	if byID["absent"].WorkItems[0].Class != "done" || byID["absent"].StatusDisplay != "achieved" {
		t.Errorf("absent backlog item → done/achieved, got item=%q node=%q",
			byID["absent"].WorkItems[0].Class, byID["absent"].StatusDisplay)
	}
}

// --- BuildGoals: roll-up precedence (ratified 9-step order) ---

func TestBuildGoals_BlockedBeatsInflight(t *testing.T) {
	// One blocked child, one in-flight child, one achieved child → blocked wins.
	file := GoalsFile{Goals: []Goal{
		{ID: "root", Title: "Root"},
		{ID: "c1", Title: "C1", Parent: "root", WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "x"}}}, // crashed → blocked
		{ID: "c2", Title: "C2", Parent: "root", WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "y"}}}, // working → in-flight
		{ID: "c3", Title: "C3", Parent: "root", Status: StatusAchieved},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, DeskStates: map[string]string{"x": "crashed", "y": "working"}})
	byID := indexByID(doc.Goals)
	if byID["root"].StatusDisplay != "blocked" {
		t.Errorf("root should be blocked (most salient child), got %q", byID["root"].StatusDisplay)
	}
	if byID["c3"].StatusDisplay != "achieved" {
		t.Errorf("declared-achieved child → achieved, got %q", byID["c3"].StatusDisplay)
	}
}

func TestBuildGoals_BlockedBeatsAuthoredPaused(t *testing.T) {
	// Ratified precedence: blocked (step 2) beats authored paused (step 4).
	file := GoalsFile{Goals: []Goal{
		{ID: "root", Title: "Root", Status: StatusPaused},
		{ID: "c", Title: "C", Parent: "root", WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "x"}}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, DeskStates: map[string]string{"x": "crashed"}})
	if r := indexByID(doc.Goals)["root"].StatusDisplay; r != "blocked" {
		t.Errorf("a paused parent with a blocked child must show blocked, got %q", r)
	}
}

func TestBuildGoals_AuthoredPausedBeatsInflight(t *testing.T) {
	// Ratified precedence: authored paused (step 4) beats in-flight (step 5).
	file := GoalsFile{Goals: []Goal{
		{ID: "root", Title: "Root", Status: StatusPaused},
		{ID: "c", Title: "C", Parent: "root", WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "x"}}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, DeskStates: map[string]string{"x": "working"}})
	if r := indexByID(doc.Goals)["root"].StatusDisplay; r != "paused" {
		t.Errorf("a paused parent with only an in-flight child should show paused, got %q", r)
	}
}

func TestBuildGoals_CancelledShortCircuits(t *testing.T) {
	// Step 1: authored cancelled wins over everything, even a blocked child.
	file := GoalsFile{Goals: []Goal{
		{ID: "root", Title: "Root", Status: StatusCancelled},
		{ID: "c", Title: "C", Parent: "root", WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "x"}}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, DeskStates: map[string]string{"x": "crashed"}})
	if r := indexByID(doc.Goals)["root"].StatusDisplay; r != "cancelled" {
		t.Errorf("authored cancelled short-circuits, got %q", r)
	}
}

func TestBuildGoals_CancelledChildExcludedFromAchieved(t *testing.T) {
	// A cancelled sub-goal is a dead branch and does not hold the parent out of achieved.
	file := GoalsFile{Goals: []Goal{
		{ID: "root", Title: "Root"},
		{ID: "done1", Title: "Done", Parent: "root", Status: StatusAchieved},
		{ID: "dead", Title: "Dead", Parent: "root", Status: StatusCancelled},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if r := indexByID(doc.Goals)["root"].StatusDisplay; r != "achieved" {
		t.Errorf("cancelled child must not hold parent out of achieved, got %q", r)
	}
}

func TestBuildGoals_AchievedWhenAllDone(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "root", Title: "Root"},
		{ID: "c1", Title: "C1", Parent: "root", Status: StatusAchieved},
		{ID: "c2", Title: "C2", Parent: "root", WorkItems: []WorkItem{{Kind: WorkInline, Text: "done thing", Done: true}}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if r := indexByID(doc.Goals)["root"].StatusDisplay; r != "achieved" {
		t.Errorf("root with all children/items done → achieved, got %q", r)
	}
}

func TestBuildGoals_EmptyNodeIsActiveNotAchieved(t *testing.T) {
	// Ratified vacuous-achieved guard: an empty node is active, never achieved.
	file := GoalsFile{Goals: []Goal{{ID: "dream", Title: "Someday"}}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if doc.Goals[0].StatusDisplay != "active" {
		t.Errorf("empty node → active (not achieved), got %q", doc.Goals[0].StatusDisplay)
	}
}

func TestBuildGoals_InlineNotDoneIsInflight(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "g", Title: "G", WorkItems: []WorkItem{{Kind: WorkInline, Text: "todo"}}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if doc.Goals[0].WorkItems[0].Class != "in-flight" || doc.Goals[0].StatusDisplay != "in-flight" {
		t.Errorf("inline without done:true → in-flight, got item=%q node=%q",
			doc.Goals[0].WorkItems[0].Class, doc.Goals[0].StatusDisplay)
	}
}

func TestBuildGoals_DeclaredPausedSurvivesIdle(t *testing.T) {
	file := GoalsFile{Goals: []Goal{{ID: "p", Title: "P", Status: StatusPaused}}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if doc.Goals[0].StatusDisplay != "paused" {
		t.Errorf("authored paused with no active work → paused, got %q", doc.Goals[0].StatusDisplay)
	}
}

// --- BuildGoals: emission order, depth, scope inference (task), counts ---

func TestBuildGoals_OrderDepthAndScope(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "b-project", Title: "B", Parent: "a-fleet"},
		{ID: "a-fleet", Title: "A"},
		{ID: "c-task", Title: "C", Parent: "b-project"},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	gotIDs := []string{doc.Goals[0].ID, doc.Goals[1].ID, doc.Goals[2].ID}
	wantIDs := []string{"a-fleet", "b-project", "c-task"}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("emission order = %v, want %v (parent must precede children)", gotIDs, wantIDs)
		}
	}
	byID := indexByID(doc.Goals)
	if byID["a-fleet"].Depth != 0 || byID["b-project"].Depth != 1 || byID["c-task"].Depth != 2 {
		t.Errorf("depths wrong: %d/%d/%d", byID["a-fleet"].Depth, byID["b-project"].Depth, byID["c-task"].Depth)
	}
	// Scope inferred from depth when unset — the ratified enum is fleet/project/task.
	if byID["a-fleet"].Scope != "fleet" || byID["b-project"].Scope != "project" || byID["c-task"].Scope != "task" {
		t.Errorf("scope inference wrong: %q/%q/%q", byID["a-fleet"].Scope, byID["b-project"].Scope, byID["c-task"].Scope)
	}
}

func TestBuildGoals_LegacyDeskScopeNormalizedToTask(t *testing.T) {
	file := GoalsFile{Goals: []Goal{{ID: "g", Title: "G", Scope: ScopeDesk}}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if doc.Goals[0].Scope != "task" {
		t.Errorf("legacy scope 'desk' should normalize to 'task', got %q", doc.Goals[0].Scope)
	}
	if doc.Counts.Task != 1 {
		t.Errorf("a desk/task node must be counted under Task, got %+v", doc.Counts)
	}
}

func TestBuildGoals_Counts(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "f", Title: "F", Scope: ScopeFleet, Status: StatusAchieved},
		{ID: "p1", Title: "P1", Scope: ScopeProject, WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "w"}}},
		{ID: "p2", Title: "P2", Scope: ScopeProject}, // empty active
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
			{Kind: WorkIssue, Ref: "owner/repo#3"},
		}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, IssueStates: map[string]string{
		"owner/repo#2": "closed", "owner/repo#3": "open"}})
	items := doc.Goals[0].WorkItems
	if items[0].Detail != "linked" || items[0].Class != "active" {
		t.Errorf("unresolved issue → linked/active, got %+v", items[0])
	}
	if items[1].Detail != "closed" || items[1].Class != "done" {
		t.Errorf("closed issue → done, got %+v", items[1])
	}
	if items[2].Detail != "open" || items[2].Class != "in-flight" {
		t.Errorf("open issue → in-flight (ratified), got %+v", items[2])
	}
}

func TestParseGoalsFile_RejectsUnknownDependsOn(t *testing.T) {
	data := []byte(`{"goals":[{"id":"a","title":"A","depends_on":["missing"]}]}`)
	if _, err := ParseGoalsFile(data); err == nil || !strings.Contains(err.Error(), "depends_on") {
		t.Fatalf("expected depends_on validation failure, got %v", err)
	}
}

func TestBuildGoals_DependsOnEdgesAndConversationAgent(t *testing.T) {
	file := GoalsFile{Version: 1, Goals: []Goal{
		{ID: "parent", Title: "Parent"},
		{ID: "child", Title: "Child", Parent: "parent", ConversationAgent: "builder",
			DependsOn: []string{"parent"}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, SourcePath: "/roster/fleet-goals.json",
		GeneratedAt: "2026-07-03T12:00:00Z"})
	if doc.Version != 1 {
		t.Fatalf("version = %d, want 1", doc.Version)
	}
	if len(doc.Edges) != 1 || doc.Edges[0].From != "child" || doc.Edges[0].To != "parent" || doc.Edges[0].Kind != "depends_on" {
		t.Fatalf("edges = %+v, want child→parent depends_on", doc.Edges)
	}
	child := indexByID(doc.Goals)["child"]
	if child.ConversationAgent != "builder" {
		t.Fatalf("conversation_agent = %q, want builder", child.ConversationAgent)
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
