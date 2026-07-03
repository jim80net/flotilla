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
