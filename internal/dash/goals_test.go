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

// --- BuildGoals: per-desk card materialization (#324 Inc 2) ---

func TestBuildGoals_MaterializeRosterDesks(t *testing.T) {
	// A flotilla hub (owner == the metaXO ⇒ hub-center) + one AUTHORED desk (owner alpha).
	file := GoalsFile{Goals: []Goal{
		{ID: "flot", Title: "Fleet", Scope: "flotilla", Owner: "xo"},
		{ID: "alpha-goal", Title: "Alpha work", Scope: "desk", Parent: "flot", Owner: "alpha", ConversationAgent: "alpha"},
	}}
	in := GoalsInputs{
		File: file, FileOK: true, MetaXO: "xo",
		AgentSurfaces: map[string]string{"beta": "grok"},
		DeskStates:    map[string]string{"beta": "working"},
		// members: the xo (the hub), alpha (authored), beta (NOT authored → materialize).
		Channels: []DeskChannel{{ChannelID: "C1", XOAgent: "xo", Members: []string{"xo", "alpha", "beta"}}},
	}
	doc := BuildGoals(in)
	byID := make(map[string]RenderedGoal, len(doc.Goals))
	for _, g := range doc.Goals {
		byID[g.ID] = g
	}

	beta, ok := byID["desk:beta"]
	if !ok {
		t.Fatalf("beta (a roster member absent from the goals file) must be materialized; goals=%+v", doc.Goals)
	}
	if beta.Scope != "desk" || beta.Source != "roster" {
		t.Errorf("materialized desk = scope %q source %q, want desk/roster", beta.Scope, beta.Source)
	}
	if beta.Parent != "flot" {
		t.Errorf("materialized desk must parent under the hub (flot), got %q", beta.Parent)
	}
	if beta.Depth != 1 {
		t.Errorf("materialized desk depth = %d, want hubDepth+1 = 1", beta.Depth)
	}
	if beta.Harness == nil || beta.Harness.Surface != "grok" {
		t.Errorf("materialized desk harness = %+v, want surface grok from the roster", beta.Harness)
	}
	if beta.StatusDisplay != "in-flight" {
		t.Errorf("beta is 'working' → status_display in-flight, got %q", beta.StatusDisplay)
	}
	// the hub is not a desk card; an authored desk is not duplicated.
	if _, ok := byID["desk:xo"]; ok {
		t.Error("the xo hub must not be materialized as a desk card")
	}
	if _, ok := byID["desk:alpha"]; ok {
		t.Error("an authored desk (alpha) must not be duplicated as a roster card")
	}
	// the hub gained beta as a child (so the org layout parents it on the ring).
	if flot := byID["flot"]; !goalIDsContain(flot.Children, "desk:beta") {
		t.Errorf("hub children must include the materialized desk, got %v", flot.Children)
	}
	// DFS ordering contract: the materialized desk is INSERTED right after its hub node,
	// not appended at the end (GoalsDoc: parent immediately precedes its children).
	var order []string
	for _, g := range doc.Goals {
		order = append(order, g.ID)
	}
	for i, id := range order {
		if id == "flot" && (i+1 >= len(order) || order[i+1] != "desk:beta") {
			t.Errorf("materialized desk must immediately follow its hub (DFS contract); order=%v", order)
		}
	}
}

func TestBuildGoals_CoordinatorNeverMaterializesAsDesk(t *testing.T) {
	// The CoS / meta-XO (cos) OWNS the fleet-command channel and is a MEMBER (the parent, for
	// awareness roll-up) of every flotilla channel. Without the coordinator guard it would
	// materialize as a spoke desk under EVERY flotilla — the operator's "CoS wired into every
	// flotilla" bug. It must appear ONLY as its hub node, never as a roster desk. Real leaf
	// desks (alpha-be, beta-be) still materialize.
	file := GoalsFile{Goals: []Goal{
		{ID: "fleet", Title: "Fleet", Scope: "flotilla", Owner: "cos"},
		{ID: "alpha", Title: "Alpha flotilla", Scope: "flotilla", Parent: "fleet", Owner: "alpha-xo", ConversationAgent: "alpha-xo"},
		{ID: "beta", Title: "Beta flotilla", Scope: "flotilla", Parent: "fleet", Owner: "beta-xo", ConversationAgent: "beta-xo"},
	}}
	in := GoalsInputs{
		File: file, FileOK: true, MetaXO: "cos",
		Channels: []DeskChannel{
			{ChannelID: "C_CMD", XOAgent: "cos", Members: []string{"alpha-xo", "beta-xo"}},
			{ChannelID: "C_ALPHA", XOAgent: "alpha-xo", Members: []string{"cos", "alpha-be"}},
			{ChannelID: "C_BETA", XOAgent: "beta-xo", Members: []string{"cos", "beta-be"}},
		},
	}
	doc := BuildGoals(in)
	for _, g := range doc.Goals {
		if g.Source == "roster" && strings.EqualFold(strings.TrimSpace(g.Owner), "cos") {
			t.Errorf("coordinator cos must NEVER materialize as a desk, found id=%q owner=%q source=%q", g.ID, g.Owner, g.Source)
		}
		if strings.HasPrefix(strings.ToLower(g.ID), "desk:cos") {
			t.Errorf("coordinator cos must NEVER get a desk node, found id=%q", g.ID)
		}
	}
	byID := indexByID(doc.Goals)
	if _, ok := byID["desk:alpha-be"]; !ok {
		t.Error("a real leaf desk (alpha-be) must still materialize")
	}
	if _, ok := byID["desk:beta-be"]; !ok {
		t.Error("a real leaf desk (beta-be) must still materialize")
	}
}

func TestBuildGoals_Collaborations(t *testing.T) {
	// A lane goal whose work_items reference two desks (codex-dev, codex-review) — both
	// authored desk nodes — must produce a collaboration group over their node ids.
	file := GoalsFile{Goals: []Goal{
		{ID: "flot", Title: "Fleet", Scope: "flotilla", Owner: "xo"},
		{ID: "cdev", Title: "Codex dev", Scope: "desk", Parent: "flot", Owner: "codex-dev", ConversationAgent: "codex-dev"},
		{ID: "crev", Title: "Codex review", Scope: "desk", Parent: "flot", Owner: "codex-review", ConversationAgent: "codex-review"},
		{ID: "lane", Title: "Codex harness lane", Scope: "task", Parent: "flot", WorkItems: []WorkItem{
			{Kind: WorkDesk, Agent: "codex-dev"},
			{Kind: WorkDesk, Agent: "codex-review"},
		}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, MetaXO: "xo"})
	if len(doc.Collaborations) != 1 {
		t.Fatalf("expected 1 collaboration, got %d: %+v", len(doc.Collaborations), doc.Collaborations)
	}
	cb := doc.Collaborations[0]
	if cb.Lane != "Codex harness lane" {
		t.Errorf("lane label = %q, want the goal title", cb.Lane)
	}
	if len(cb.Desks) != 2 || !goalIDsContain(cb.Desks, "cdev") || !goalIDsContain(cb.Desks, "crev") {
		t.Errorf("collaboration desks = %v, want [cdev crev]", cb.Desks)
	}
	// A single-desk lane must NOT form a collaboration (needs ≥2).
	file2 := GoalsFile{Goals: []Goal{
		{ID: "flot", Title: "Fleet", Scope: "flotilla", Owner: "xo"},
		{ID: "cdev", Title: "Codex dev", Scope: "desk", Parent: "flot", Owner: "codex-dev"},
		{ID: "lane", Title: "Solo lane", Scope: "task", Parent: "flot", WorkItems: []WorkItem{{Kind: WorkDesk, Agent: "codex-dev"}}},
	}}
	if doc2 := BuildGoals(GoalsInputs{File: file2, FileOK: true}); len(doc2.Collaborations) != 0 {
		t.Errorf("a single-desk lane must not form a collaboration, got %+v", doc2.Collaborations)
	}
}

// TestBuildGoals_DecisionBrief locks the #347 decision-package passthrough: a work item's
// (and a node's) optional Brief reaches the /api/goals shape so the respond modal can render
// it; an item without a brief stays empty (the modal shows the honest empty state).
func TestBuildGoals_DecisionBrief(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "g", Title: "G", Brief: "node-level decision package", WorkItems: []WorkItem{
			{Kind: WorkInline, Text: "Kelly loss-cap — value sign-off", Brief: "## Recommendation\ncap at 0.25\n\n- value: capital protected\n- tradeoff: fewer entries"},
			{Kind: WorkInline, Text: "no-brief item"},
		}},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if len(doc.Goals) != 1 {
		t.Fatalf("goals = %d", len(doc.Goals))
	}
	node := doc.Goals[0]
	if node.Brief != "node-level decision package" {
		t.Errorf("node-level brief passthrough = %q", node.Brief)
	}
	if len(node.WorkItems) != 2 {
		t.Fatalf("work items = %d", len(node.WorkItems))
	}
	if !strings.Contains(node.WorkItems[0].Brief, "Recommendation") {
		t.Errorf("work-item brief must pass through, got %q", node.WorkItems[0].Brief)
	}
	if node.WorkItems[1].Brief != "" {
		t.Errorf("an item without a brief must stay empty, got %q", node.WorkItems[1].Brief)
	}
}

func TestBuildGoals_DeskIDCollision(t *testing.T) {
	// An authored goal LITERALLY named "desk:beta" must not be clobbered by the card
	// synthesized for member beta — the synthetic id is suffixed to stay unique.
	file := GoalsFile{Goals: []Goal{
		{ID: "flot", Title: "Fleet", Scope: "flotilla", Owner: "xo"},
		{ID: "desk:beta", Title: "A goal named desk:beta", Scope: "task", Parent: "flot", Owner: "someone-else"},
	}}
	in := GoalsInputs{
		File: file, FileOK: true, MetaXO: "xo",
		Channels: []DeskChannel{{ChannelID: "C1", XOAgent: "xo", Members: []string{"xo", "beta"}}},
	}
	doc := BuildGoals(in)

	authored, betaCard := 0, (*RenderedGoal)(nil)
	for i := range doc.Goals {
		g := &doc.Goals[i]
		if g.ID == "desk:beta" {
			authored++
			if g.Source == "roster" {
				t.Error("the authored desk:beta must be preserved, not replaced by the materialized card")
			}
		}
		if g.Source == "roster" && strings.EqualFold(g.Owner, "beta") {
			betaCard = g
		}
	}
	if authored != 1 {
		t.Errorf("the authored desk:beta must remain exactly once, found %d", authored)
	}
	if betaCard == nil {
		t.Fatalf("member beta must still be materialized under a unique id; goals=%+v", doc.Goals)
	}
	if betaCard.ID == "desk:beta" {
		t.Errorf("materialized beta must not collide with the authored id, got %q", betaCard.ID)
	}
}

func goalIDsContain(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
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

func TestBuildGoals_Pending(t *testing.T) {
	// #349 Inc 3 taxonomy: a would-be-active goal waiting on an unfinished depends_on target
	// is dependency-gated → "pending" (a calm state distinct from blocked-red / awaiting-amber).
	// Once every dependency is achieved it reverts to plain active.
	t.Run("unfinished dependency → pending", func(t *testing.T) {
		file := GoalsFile{Goals: []Goal{
			{ID: "dep", Title: "Prereq"}, // empty → active (not achieved)
			{ID: "waiter", Title: "Waits", DependsOn: []string{"dep"}},
		}}
		doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
		byID := indexByID(doc.Goals)
		if byID["waiter"].StatusDisplay != "pending" {
			t.Errorf("goal with an unfinished depends_on → pending, got %q", byID["waiter"].StatusDisplay)
		}
		if doc.Counts.Pending != 1 {
			t.Errorf("counts.pending = %d, want 1", doc.Counts.Pending)
		}
		if byID["dep"].StatusDisplay != "active" {
			t.Errorf("the dependency itself is untouched (active), got %q", byID["dep"].StatusDisplay)
		}
	})
	t.Run("achieved dependency → stays active", func(t *testing.T) {
		file := GoalsFile{Goals: []Goal{
			{ID: "dep", Title: "Prereq", Status: StatusAchieved},
			{ID: "waiter", Title: "Waits", DependsOn: []string{"dep"}},
		}}
		doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
		byID := indexByID(doc.Goals)
		if byID["waiter"].StatusDisplay != "active" {
			t.Errorf("goal whose only depends_on is achieved → active, got %q", byID["waiter"].StatusDisplay)
		}
		if doc.Counts.Pending != 0 {
			t.Errorf("counts.pending = %d, want 0", doc.Counts.Pending)
		}
	})
	t.Run("only would-be-active goals become pending", func(t *testing.T) {
		// A goal with live in-flight work stays in-flight even if a dependency is unfinished —
		// pending is strictly the calm "nothing to do but wait" state, not an override.
		file := GoalsFile{Goals: []Goal{
			{ID: "dep", Title: "Prereq"},
			{ID: "busy", Title: "Busy", DependsOn: []string{"dep"},
				WorkItems: []WorkItem{{Kind: WorkInline, Text: "todo"}}},
		}}
		doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
		byID := indexByID(doc.Goals)
		if byID["busy"].StatusDisplay != "in-flight" {
			t.Errorf("a goal with active work stays in-flight, not pending, got %q", byID["busy"].StatusDisplay)
		}
	})
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
	// Scope inferred from depth when unset — v2 API vocabulary is flotilla/desk/task.
	if byID["a-fleet"].Scope != "flotilla" || byID["b-project"].Scope != "desk" || byID["c-task"].Scope != "task" {
		t.Errorf("scope inference wrong: %q/%q/%q", byID["a-fleet"].Scope, byID["b-project"].Scope, byID["c-task"].Scope)
	}
}

func TestBuildGoals_LegacyDeskScopeNormalizedToTask(t *testing.T) {
	file := GoalsFile{Goals: []Goal{{ID: "g", Title: "G", Scope: ScopeDeskLeaf}}}
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
	if c.Total != 3 || c.Flotilla != 1 || c.Desk != 2 || c.Fleet != 1 || c.Project != 2 {
		t.Errorf("scope counts wrong: %+v", c)
	}
	if c.Realized != 1 || c.InFlight != 1 || c.Aspirational != 1 {
		t.Errorf("state counts wrong: %+v", c)
	}
}

func TestBuildGoals_TrailerIssueMergedOntoGoal(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "dash-next-gen", Title: "Dash next gen"},
		{ID: "other", Title: "Other"},
	}}
	doc := BuildGoals(GoalsInputs{
		File: file, FileOK: true,
		IssueStates: map[string]string{"jim80net/flotilla#267": "open"},
		TrailerIssues: []GoalTrailerIssue{{
			GoalID: "dash-next-gen",
			Ref:    "jim80net/flotilla#267",
			State:  "open",
		}},
	})
	g := indexByID(doc.Goals)["dash-next-gen"]
	if len(g.WorkItems) != 1 {
		t.Fatalf("expected one trailer issue on goal, got %+v", g.WorkItems)
	}
	if g.WorkItems[0].Ref != "jim80net/flotilla#267" || g.WorkItems[0].Class != "in-flight" {
		t.Errorf("trailer issue render wrong: %+v", g.WorkItems[0])
	}
	if other := indexByID(doc.Goals)["other"]; len(other.WorkItems) != 0 {
		t.Errorf("unreferenced goal should have no trailer items, got %+v", other.WorkItems)
	}
}

func TestBuildGoals_TrailerIssueWithoutIssueStates(t *testing.T) {
	file := GoalsFile{Goals: []Goal{{ID: "g", Title: "G"}}}
	doc := BuildGoals(GoalsInputs{
		File: file, FileOK: true,
		TrailerIssues: []GoalTrailerIssue{{
			GoalID: "g", Ref: "owner/repo#7", State: "open",
		}},
	})
	items := doc.Goals[0].WorkItems
	if len(items) != 1 || items[0].Class != "in-flight" || items[0].Detail != "open" {
		t.Errorf("trailer state without IssueStates → in-flight/open, got %+v", items)
	}
}

func TestBuildGoals_TrailerIssueSkipsDuplicateRef(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "g", Title: "G", WorkItems: []WorkItem{{Kind: WorkIssue, Ref: "owner/repo#1"}}},
	}}
	doc := BuildGoals(GoalsInputs{
		File: file, FileOK: true,
		IssueStates: map[string]string{"owner/repo#1": "open"},
		TrailerIssues: []GoalTrailerIssue{{
			GoalID: "g", Ref: "owner/repo#1", State: "open",
		}},
	})
	if len(doc.Goals[0].WorkItems) != 1 {
		t.Fatalf("duplicate trailer must not append, got %d items", len(doc.Goals[0].WorkItems))
	}
}

func TestBuildGoals_TrailerIssueIgnoresClosed(t *testing.T) {
	file := GoalsFile{Goals: []Goal{{ID: "g", Title: "G"}}}
	doc := BuildGoals(GoalsInputs{
		File: file, FileOK: true,
		TrailerIssues: []GoalTrailerIssue{{
			GoalID: "g", Ref: "owner/repo#9", State: "closed",
		}},
	})
	if len(doc.Goals[0].WorkItems) != 0 {
		t.Fatalf("closed trailer issue must not attach, got %+v", doc.Goals[0].WorkItems)
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

func TestParseGoalsFile_RejectsSelfAndDuplicateDependsOn(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"goals":[{"id":"a","title":"A","depends_on":["a"]}]}`),
		[]byte(`{"goals":[{"id":"a","title":"A"},{"id":"b","title":"B","depends_on":["a","a"]}]}`),
	} {
		if _, err := ParseGoalsFile(data); err == nil {
			t.Fatalf("malformed depends_on must error for %s", data)
		}
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

func TestBuildGoals_V2FieldsAndHarness(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "hub", Title: "Hub", Scope: ScopeFlotilla, Owner: "cos",
			Priorities: []string{"Ship schema-v2"}, TopologyChannelID: " ch-1 "},
		{ID: "desk", Title: "Desk", Scope: ScopeOrgDesk, Parent: "hub", Owner: "builder",
			ConversationAgent: "builder", Milestones: []string{"PR merge"}},
	}}
	doc := BuildGoals(GoalsInputs{
		File: file, FileOK: true,
		AgentSurfaces: map[string]string{"builder": "grok"},
		MetaXO:        "cos",
	})
	hub := indexByID(doc.Goals)["hub"]
	if hub.Scope != "flotilla" || hub.TopologyChannelID != "ch-1" || len(hub.Priorities) != 1 {
		t.Fatalf("hub v2 fields wrong: %+v", hub)
	}
	if hub.Layout == nil || !hub.Layout.HubCenter {
		t.Fatalf("hub should be layout hub_center, got %+v", hub.Layout)
	}
	desk := indexByID(doc.Goals)["desk"]
	if desk.Scope != "desk" || len(desk.Milestones) != 1 {
		t.Fatalf("desk v2 fields wrong: %+v", desk)
	}
	if desk.Harness == nil || desk.Harness.Surface != "grok" {
		t.Fatalf("desk harness wrong: %+v", desk.Harness)
	}
}

func TestBuildGoals_V1ScopesDisplayAsV2(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "root", Title: "R", Scope: ScopeFleet},
		{ID: "mid", Title: "M", Scope: ScopeProject, Parent: "root"},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true})
	if got := indexByID(doc.Goals)["root"].Scope; got != "flotilla" {
		t.Errorf("fleet → flotilla, got %q", got)
	}
	if got := indexByID(doc.Goals)["mid"].Scope; got != "desk" {
		t.Errorf("project → desk, got %q", got)
	}
}

func TestBuildGoals_FlotillaSpokeLayout(t *testing.T) {
	file := GoalsFile{Goals: []Goal{
		{ID: "cos", Title: "COS", Scope: ScopeFlotilla},
		{ID: "xo", Title: "XO", Scope: ScopeFlotilla, Parent: "cos"},
	}}
	doc := BuildGoals(GoalsInputs{File: file, FileOK: true, MetaXO: "cos"})
	xo := indexByID(doc.Goals)["xo"]
	if xo.Layout == nil || !xo.Layout.Spoke {
		t.Fatalf("child flotilla node should be spoke, got %+v", xo.Layout)
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
