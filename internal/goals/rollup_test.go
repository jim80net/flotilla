package goals

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/dash"
)

// rollupDoc parses YAML, compiles to the dash GoalsFile contract, and builds the rendered doc.
func rollupDoc(t *testing.T, yaml string, extra ...func(*dash.GoalsInputs)) dash.GoalsDoc {
	t.Helper()
	f, err := ParseYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	gf, err := dash.ParseGoalsFile(b)
	if err != nil {
		t.Fatalf("ParseGoalsFile: %v", err)
	}
	in := dash.GoalsInputs{File: gf, FileOK: true}
	for _, fn := range extra {
		fn(&in)
	}
	return dash.BuildGoals(in)
}

func rollupByID(doc dash.GoalsDoc) map[string]dash.RenderedGoal {
	m := make(map[string]dash.RenderedGoal, len(doc.Goals))
	for _, g := range doc.Goals {
		m[g.ID] = g
	}
	return m
}

const sampleYAML = `
version: 1
goals:
  - id: g-root
    title: Root
    status: active
    children:
      - id: ws-active
        title: Active
        status: active
        depends_on: [ws-done]
        work_items:
          - kind: backlog
            match: "[in-flight] shipping it"
          - kind: issue
            ref: "owner/repo#1"
      - id: ws-done
        title: Done
        status: achieved
        work_items:
          - kind: backlog
            match: "[done] shipped"
  - id: g-gated
    title: Gated
    status: active
    work_items:
      - kind: backlog
        match: "[awaiting-auth] operator call"
  - id: g-blocked
    title: Blocked
    status: active
    children:
      - id: ws-blocked
        title: Blocked stream
        status: active
        work_items:
          - kind: backlog
            match: "[blocked] waiting on dep"
`

func TestRollup_TreeAndPrecedence(t *testing.T) {
	doc := rollupDoc(t, sampleYAML, func(in *dash.GoalsInputs) {
		in.Backlog = strings.Join([]string{
			"## Backlog",
			"- [in-flight] shipping it",
			"- [done] shipped",
			"- [awaiting-auth] operator call",
			"- [blocked] waiting on dep",
		}, "\n")
	})
	want := map[string]string{
		"g-root":     "in-flight",
		"ws-active":  "in-flight",
		"ws-done":    "achieved",
		"g-gated":    "awaiting",
		"g-blocked":  "blocked",
		"ws-blocked": "blocked",
	}
	byID := rollupByID(doc)
	for id, wantDisp := range want {
		if got := byID[id].StatusDisplay; got != wantDisp {
			t.Errorf("rollup[%s] = %q, want %q", id, got, wantDisp)
		}
	}
}

func TestRollup_UnresolvedIssueNeutral(t *testing.T) {
	doc := rollupDoc(t, `version: 1
goals:
  - id: g
    title: T
    status: active
    work_items:
      - kind: issue
        ref: owner/repo#1
`)
	if rollupByID(doc)["g"].StatusDisplay != "active" {
		t.Errorf("unresolved issue must not over-achieve")
	}
}

func TestRollup_AuthoredPausedCancelled(t *testing.T) {
	doc := rollupDoc(t, `version: 1
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
            match: "[in-flight] busy"
  - id: c
    title: Cancelled
    status: cancelled
`, func(in *dash.GoalsInputs) {
		in.Backlog = "## Backlog\n- [in-flight] busy\n"
	})
	byID := rollupByID(doc)
	if byID["p"].StatusDisplay != "paused" {
		t.Errorf("paused = %q, want paused", byID["p"].StatusDisplay)
	}
	if byID["pc"].StatusDisplay != "in-flight" {
		t.Errorf("child = %q, want in-flight", byID["pc"].StatusDisplay)
	}
	if byID["c"].StatusDisplay != "cancelled" {
		t.Errorf("cancelled = %q, want cancelled", byID["c"].StatusDisplay)
	}
}

func TestRollup_PauseYieldsToLiveBlockerNotInFlight(t *testing.T) {
	cases := []struct {
		name, match, want string
	}{
		{"blocked through pause", "[blocked] dep down", "blocked"},
		{"awaiting through pause", "[awaiting-auth] operator call", "awaiting"},
		{"in-flight yields to pause", "[in-flight] busy", "paused"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			y := `version: 1
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
            match: "` + tc.match + `"
`
			doc := rollupDoc(t, y, func(in *dash.GoalsInputs) {
				in.Backlog = "## Backlog\n- " + tc.match + "\n"
			})
			if rollupByID(doc)["p"].StatusDisplay != tc.want {
				t.Errorf("paused parent = %q, want %q", rollupByID(doc)["p"].StatusDisplay, tc.want)
			}
		})
	}
}

func TestRollup_CancelledChildExcludedFromAchieved(t *testing.T) {
	doc := rollupDoc(t, `version: 1
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
`)
	byID := rollupByID(doc)
	if byID["parent"].StatusDisplay != "achieved" {
		t.Errorf("parent = %q, want achieved", byID["parent"].StatusDisplay)
	}
	if byID["dead-kid"].StatusDisplay != "cancelled" {
		t.Errorf("dead-kid = %q, want cancelled", byID["dead-kid"].StatusDisplay)
	}
}

func TestRollup_InlineDoneVsBare(t *testing.T) {
	doc := rollupDoc(t, `version: 1
goals:
  - id: g
    title: T
    status: active
    work_items:
      - kind: inline
        text: open work
`)
	if rollupByID(doc)["g"].StatusDisplay != "in-flight" {
		t.Errorf("bare inline → in-flight, got %q", rollupByID(doc)["g"].StatusDisplay)
	}
	doc2 := rollupDoc(t, `version: 1
goals:
  - id: g
    title: T
    status: active
    work_items:
      - kind: inline
        text: shipped
        done: true
`)
	if rollupByID(doc2)["g"].StatusDisplay != "achieved" {
		t.Errorf("inline done → achieved, got %q", rollupByID(doc2)["g"].StatusDisplay)
	}
}

func TestRollup_VacuousLeafIsActiveNotAchieved(t *testing.T) {
	doc := rollupDoc(t, `version: 1
goals:
  - id: leaf
    title: Leaf
    status: active
`)
	if rollupByID(doc)["leaf"].StatusDisplay != "active" {
		t.Errorf("empty leaf = %q, want active", rollupByID(doc)["leaf"].StatusDisplay)
	}
	doc2 := rollupDoc(t, `version: 1
goals:
  - id: done
    title: Done
    status: achieved
`)
	if rollupByID(doc2)["done"].StatusDisplay != "achieved" {
		t.Errorf("authored achieved leaf = %q, want achieved", rollupByID(doc2)["done"].StatusDisplay)
	}
}

func TestRollup_ConversationAgentPassthrough(t *testing.T) {
	f, err := ParseYAML([]byte(`version: 1
goals:
  - id: g
    title: T
    status: active
    conversation_agent: alpha
`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Goals[0].ConversationAgent != "alpha" {
		t.Errorf("conversation_agent = %q", f.Goals[0].ConversationAgent)
	}
}
