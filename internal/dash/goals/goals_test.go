package goals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func init() { nowRFC3339 = func() string { return "2026-07-03T00:00:00Z" } }

const sampleYAML = `
version: 1
goals:
  - id: g-root
    title: "Root goal"
    scope: fleet
    status: active
    children:
      - id: ws-active
        title: "Active workstream"
        scope: project
        status: active
        depends_on: [ws-done]
        work_items:
          - kind: backlog
            marker: "[in-flight] shipping it"
          - kind: issue
            ref: "owner/repo#1"
      - id: ws-done
        title: "Done workstream"
        scope: project
        status: achieved
        work_items:
          - kind: backlog
            marker: "[done] shipped"
  - id: g-gated
    title: "Gated goal"
    scope: fleet
    status: active
    work_items:
      - kind: backlog
        marker: "[awaiting-auth] operator call"
  - id: g-blocked
    title: "Blocked goal"
    scope: fleet
    status: active
    children:
      - id: ws-blocked
        title: "Blocked stream"
        scope: project
        status: active
        work_items:
          - kind: backlog
            marker: "[blocked] waiting on dep"
`

func TestParse_TreeAndRollups(t *testing.T) {
	d, err := Parse([]byte(sampleYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tree) != 3 {
		t.Fatalf("roots = %d, want 3", len(d.Tree))
	}
	if d.GeneratedAt != "2026-07-03T00:00:00Z" {
		t.Errorf("generated_at = %q", d.GeneratedAt)
	}
	// Roll-ups (design §4.4): blocked wins, then awaiting, then in-flight, then achieved.
	want := map[string]string{
		"g-root":     "in-flight", // has an in-flight child (ws-active)
		"ws-active":  "in-flight", // [in-flight] backlog item
		"ws-done":    "achieved",  // status achieved + [done] item
		"g-gated":    "awaiting",  // [awaiting-auth] item
		"g-blocked":  "blocked",   // child ws-blocked is blocked
		"ws-blocked": "blocked",   // [blocked] item
	}
	for id, wantDisp := range want {
		if got := d.Rollups[id]; got != wantDisp {
			t.Errorf("rollup[%s] = %q, want %q", id, got, wantDisp)
		}
	}
}

func TestParse_WorkItemStatusDerived(t *testing.T) {
	d, _ := Parse([]byte(sampleYAML))
	det, ok := d.Detail("ws-active")
	if !ok {
		t.Fatal("ws-active not found")
	}
	// The backlog item's status is derived from its marker; the issue item is neutral.
	var backlog, issue *WorkItem
	for i := range det.WorkItems {
		switch det.WorkItems[i].Kind {
		case "backlog":
			backlog = &det.WorkItems[i]
		case "issue":
			issue = &det.WorkItems[i]
		}
	}
	if backlog == nil || backlog.Status != "in-flight" {
		t.Errorf("backlog item status = %+v, want in-flight", backlog)
	}
	if issue == nil || issue.Status != "" {
		t.Errorf("issue item status = %+v, want neutral (no gh call in the minimal path)", issue)
	}
}

func TestDetail_OwnerDeskAndMissing(t *testing.T) {
	d, _ := Parse([]byte("version: 1\ngoals:\n  - id: g\n    title: T\n    status: active\n    owner: alpha\n"))
	det, ok := d.Detail("g")
	if !ok || det.Node.ID != "g" {
		t.Fatal("detail g")
	}
	if len(det.DeskStates) != 1 || det.DeskStates[0].Agent != "alpha" {
		t.Errorf("desk_states = %+v, want [alpha]", det.DeskStates)
	}
	if _, ok := d.Detail("nope"); ok {
		t.Error("missing id must return ok=false")
	}
}

func TestParse_Edges(t *testing.T) {
	// ws-active depends_on ws-done → one cross-dependency edge in GoalsDoc.edges[].
	d, err := Parse([]byte(sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Edges) != 1 {
		t.Fatalf("edges = %+v, want 1 (ws-active→ws-done)", d.Edges)
	}
	e := d.Edges[0]
	if e.From != "ws-active" || e.To != "ws-done" || e.Kind != "depends_on" {
		t.Errorf("edge = %+v (kind must be depends_on, ratified spec line 43)", e)
	}
}

func TestParse_DanglingDependsOnRejected(t *testing.T) {
	y := "version: 1\ngoals:\n  - id: a\n    title: A\n    status: active\n    depends_on: [ghost]\n"
	if _, err := Parse([]byte(y)); err == nil || !strings.Contains(err.Error(), "unknown id") {
		t.Errorf("a depends_on to an unknown id must be rejected, got %v", err)
	}
}

func TestCompute_AuthoredPausedCancelledPrecedence(t *testing.T) {
	// A paused node outranks an in-flight child (rule 4 > 5); a cancelled node outranks
	// everything (rule 1). (The pause-yields-to-a-blocker case is its own test above.)
	y := `
version: 1
goals:
  - id: p
    title: Paused
    status: paused
    children:
      - id: pc
        title: Child
        status: active
        work_items:
          - kind: backlog
            marker: "[in-flight] busy"
  - id: c
    title: Cancelled
    status: cancelled
`
	d, _ := Parse([]byte(y))
	if d.Rollups["p"] != "paused" {
		t.Errorf("paused node = %q, want paused (authored precedence, not overridden by in-flight child)", d.Rollups["p"])
	}
	if d.Rollups["pc"] != "in-flight" {
		t.Errorf("child = %q, want in-flight (children still computed)", d.Rollups["pc"])
	}
	if d.Rollups["c"] != "cancelled" {
		t.Errorf("cancelled node = %q, want cancelled", d.Rollups["c"])
	}
}

func TestCompute_PauseYieldsToLiveBlockerNotInFlight(t *testing.T) {
	// Ratified precedence (spec lines 89-103): a PAUSE outranks in-flight (rule 4 > 5)
	// but a blocked/awaiting descendant outranks the pause (rules 2/3 > 4) — a pause
	// never hides a live blocker. Only authored cancelled (rule 1) outranks blocked.
	cases := []struct {
		name, childMarker, want string
	}{
		{"blocked descendant surfaces through a pause", "[blocked] dep down", "blocked"},
		{"awaiting descendant surfaces through a pause", "[awaiting-auth] operator call", "awaiting"},
		{"in-flight descendant yields to the pause", "[in-flight] busy", "paused"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			y := "version: 1\ngoals:\n  - id: p\n    title: Paused\n    status: paused\n    children:\n" +
				"      - id: pc\n        title: Child\n        status: active\n        work_items:\n" +
				"          - kind: backlog\n            marker: \"" + tc.childMarker + "\"\n"
			d, err := Parse([]byte(y))
			if err != nil {
				t.Fatal(err)
			}
			if d.Rollups["p"] != tc.want {
				t.Errorf("paused parent = %q, want %q", d.Rollups["p"], tc.want)
			}
		})
	}
}

func TestCompute_CancelledChildExcludedFromAchieved(t *testing.T) {
	// A cancelled sub-goal is a dead branch: it must NOT hold the parent out of
	// achieved (spec rule 7). Parent has one achieved child + one cancelled child and
	// no open items → achieved.
	y := `
version: 1
goals:
  - id: parent
    title: Parent
    status: active
    children:
      - id: done-kid
        title: Done
        status: achieved
        work_items:
          - kind: inline
            text: shipped
            done: true
      - id: dead-kid
        title: Cancelled
        status: cancelled
`
	d, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if d.Rollups["parent"] != "achieved" {
		t.Errorf("parent = %q, want achieved (cancelled child is a dead branch, excluded)", d.Rollups["parent"])
	}
	if d.Rollups["dead-kid"] != "cancelled" {
		t.Errorf("dead-kid = %q, want cancelled", d.Rollups["dead-kid"])
	}
}

func TestCompute_InlineDoneVsBareIsInFlight(t *testing.T) {
	// Inline is a YAML-deterministic kind: done:true → done, a bare line → in-flight.
	d, _ := Parse([]byte("version: 1\ngoals:\n  - id: g\n    title: T\n    status: active\n    work_items:\n      - kind: inline\n        text: open work\n"))
	if d.Rollups["g"] != "in-flight" {
		t.Errorf("bare inline = %q, want in-flight (open checklist line)", d.Rollups["g"])
	}
	d2, _ := Parse([]byte("version: 1\ngoals:\n  - id: g\n    title: T\n    status: active\n    work_items:\n      - kind: inline\n        text: shipped\n        done: true\n"))
	if d2.Rollups["g"] != "achieved" {
		t.Errorf("inline done + one item = %q, want achieved (all items done, rule 7)", d2.Rollups["g"])
	}
}

func TestCompute_UnresolvedIssueDoesNotOverAchieve(t *testing.T) {
	// A live-resolved kind (issue) is neutral in the minimal parser — it must NOT let
	// an active node roll up achieved (we can't confirm the issue is closed).
	d, _ := Parse([]byte("version: 1\ngoals:\n  - id: g\n    title: T\n    status: active\n    work_items:\n      - kind: issue\n        ref: owner/repo#1\n"))
	if d.Rollups["g"] != "active" {
		t.Errorf("active node with only an unresolved issue = %q, want active (neutral issue never over-achieves)", d.Rollups["g"])
	}
}

func TestCompute_VacuousLeafIsActiveNotAchieved(t *testing.T) {
	// A leaf with zero children AND zero work items must be active, never achieved.
	d, _ := Parse([]byte("version: 1\ngoals:\n  - id: leaf\n    title: Leaf\n    status: active\n"))
	if d.Rollups["leaf"] != "active" {
		t.Errorf("empty leaf = %q, want active (never vacuous-achieved)", d.Rollups["leaf"])
	}
	// An authored achieved leaf IS achieved (authored wins).
	d2, _ := Parse([]byte("version: 1\ngoals:\n  - id: done\n    title: Done\n    status: achieved\n"))
	if d2.Rollups["done"] != "achieved" {
		t.Errorf("authored-achieved leaf = %q, want achieved", d2.Rollups["done"])
	}
}

func TestParse_ConversationAgentPassthrough(t *testing.T) {
	d, _ := Parse([]byte("version: 1\ngoals:\n  - id: g\n    title: T\n    status: active\n    conversation_agent: alpha\n"))
	det, _ := d.Detail("g")
	if det.Node.ConversationAgent != "alpha" {
		t.Errorf("conversation_agent = %q, want alpha (deep-link ref)", det.Node.ConversationAgent)
	}
}

func TestParse_RejectsDuplicateAndCycle(t *testing.T) {
	dup := "version: 1\ngoals:\n  - id: x\n    title: A\n    status: active\n  - id: x\n    title: B\n    status: active\n"
	if _, err := Parse([]byte(dup)); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("duplicate id must be rejected, got %v", err)
	}
	empty := "version: 1\ngoals:\n  - id: \"\"\n    title: A\n    status: active\n"
	if _, err := Parse([]byte(empty)); err == nil {
		t.Error("empty id must be rejected")
	}
}

func TestParse_NullNodeIsTypedErrorNotPanic(t *testing.T) {
	// A null list entry decodes to a nil node — must be a typed error, never a panic.
	for _, y := range []string{
		"version: 1\ngoals:\n  -\n", // null root
		"version: 1\ngoals:\n  - id: a\n    title: A\n    status: active\n    children:\n      -\n", // null child
	} {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("null node must be a typed error, got nil for:\n%s", y)
		}
	}
}

func TestParse_NoGoalsShapeIsEmptySlicesNotNull(t *testing.T) {
	// A version-only file (no goals) must yield tree:[] and edges:[] (not JSON null),
	// matching the missing-file empty doc — one consistent "no goals" shape.
	d, err := Parse([]byte("version: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Tree == nil || d.Edges == nil {
		t.Errorf("no-goals file must have non-nil tree/edges slices, got tree=%v edges=%v", d.Tree, d.Edges)
	}
}

func TestParse_Malformed(t *testing.T) {
	if _, err := Parse([]byte("version: 1\ngoals: [this is not a list of maps")); err == nil {
		t.Error("malformed yaml must be a typed error, not a silent empty tree")
	}
}

func TestLoad_MissingFileIsEmptyNotError(t *testing.T) {
	d, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing file must be empty, not error: %v", err)
	}
	if len(d.Tree) != 0 {
		t.Errorf("missing file → %d roots, want 0", len(d.Tree))
	}
}

// TestParse_CommittedExample guards that the committed generic example stays
// parseable + contract-valid — it is the fixture the Goals UI renders + the
// contract reference for flotilla-dev's core.
func TestParse_CommittedExample(t *testing.T) {
	// internal/dash/goals → repo root is three up.
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "fleet-goals.example.yaml"))
	if err != nil {
		t.Skipf("example not found (ok in isolated checkout): %v", err)
	}
	d, err := Parse(raw)
	if err != nil {
		t.Fatalf("committed fleet-goals.example.yaml must parse: %v", err)
	}
	if len(d.Tree) < 3 {
		t.Errorf("example should have ≥3 top-level goals, got %d", len(d.Tree))
	}
	// It must exercise the achieved + a gated/awaiting or blocked state for the UI.
	seen := map[string]bool{}
	for _, disp := range d.Rollups {
		seen[disp] = true
	}
	// The example must exercise the operator-facing states the UI colors distinctly.
	for _, disp := range []string{"achieved", "in-flight", "awaiting", "blocked"} {
		if !seen[disp] {
			t.Errorf("example should include a %q roll-up (state coverage for the UI)", disp)
		}
	}
}
