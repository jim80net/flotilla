package dash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

// TestHandleGoals_LiveDeskBinding exercises the full HTTP read path: the goals file's desk work
// item binds to the SAME snapshot the fleet board reads, so "alpha working" makes the node in-flight.
func TestHandleGoals_LiveDeskBinding(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)

	// Snapshot: alpha is working, xo idle.
	writeSnapshot(t, filepath.Join(dir, "flotilla-detector-state.json"),
		watch.Snapshot{DeskStates: map[string]surface.State{"xo": surface.StateIdle, "alpha": surface.StateWorking}},
		now.Add(-10*time.Second))

	// Backlog: one in-flight line the goal attaches to.
	backlog := "## Backlog\n- [in-flight] wire the goals dashboard view\n- [blocked] operator sign-off\n"
	if err := os.WriteFile(filepath.Join(dir, ".flotilla-state.md"), []byte(backlog), 0o600); err != nil {
		t.Fatal(err)
	}

	// Goals file (default path <roster-dir>/fleet-goals.json).
	goals := `{
	  "version": 1,
	  "goals": [
	    {"id": "product", "title": "Product", "scope": "fleet"},
	    {"id": "dash", "title": "Dashboard", "scope": "project", "parent": "product",
	     "work_items": [
	       {"kind": "desk", "agent": "alpha"},
	       {"kind": "backlog", "match": "goals dashboard view"}
	     ]},
	    {"id": "gate", "title": "Gate", "scope": "project", "parent": "product",
	     "work_items": [{"kind": "backlog", "match": "operator sign-off"}]}
	  ]
	}`
	if err := os.WriteFile(filepath.Join(dir, "fleet-goals.json"), []byte(goals), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := doGet(t, srv, "/api/goals")
	if rec.Code != 200 {
		t.Fatalf("status code %d", rec.Code)
	}
	var doc GoalsDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !doc.Found {
		t.Fatalf("goals should be found, got %+v", doc)
	}
	byID := indexByID(doc.Goals)
	// alpha is working AND the backlog item is in-flight → the dash node is in-flight.
	if byID["dash"].StatusDisplay != "in-flight" {
		t.Errorf("dash node should be in-flight (alpha working), got %q", byID["dash"].StatusDisplay)
	}
	// gate attaches a [blocked] backlog line → blocked (ratified: [blocked] → blocked, not awaiting).
	if byID["gate"].StatusDisplay != "blocked" {
		t.Errorf("gate node should be blocked ([blocked] backlog), got %q", byID["gate"].StatusDisplay)
	}
	// The fleet parent rolls up its children: any blocked child → blocked.
	if byID["product"].StatusDisplay != "blocked" {
		t.Errorf("product should roll up blocked from the gate child, got %q", byID["product"].StatusDisplay)
	}
	// The desk work item carries the live board state word.
	found := false
	for _, wi := range byID["dash"].WorkItems {
		if wi.Kind == "desk" && wi.Detail == "working" {
			found = true
		}
	}
	if !found {
		t.Errorf("dash desk item should show live state 'working', items=%+v", byID["dash"].WorkItems)
	}
}

// TestHandleGoals_AbsentFile: no goals file → an honest Found=false document (never fabricated).
func TestHandleGoals_AbsentFile(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	rec := doGet(t, srv, "/api/goals")
	if rec.Code != 200 {
		t.Fatalf("status code %d", rec.Code)
	}
	var doc GoalsDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Found || doc.Message == "" {
		t.Fatalf("absent goals file should be Found=false with a message, got %+v", doc)
	}
}

// TestHandleGoals_InvalidFileSurfacesError: a cyclic goals file surfaces the load error fail-closed.
func TestHandleGoals_InvalidFileSurfacesError(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	bad := `{"goals":[{"id":"a","title":"A","parent":"b"},{"id":"b","title":"B","parent":"a"}]}`
	if err := os.WriteFile(filepath.Join(dir, "fleet-goals.json"), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := doGet(t, srv, "/api/goals")
	var doc GoalsDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Found || doc.Error == "" {
		t.Fatalf("cyclic goals file should surface an error, got %+v", doc)
	}
}

func TestHandleGoalsMeta_DefaultViewWithoutRenderedDocument(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	body := `{"version":1,"default_view":true,"goals":[{"id":"root","title":"Root"}]}`
	if err := os.WriteFile(filepath.Join(dir, "fleet-goals.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := doGet(t, srv, "/api/goals/meta")
	if rec.Code != 200 {
		t.Fatalf("status code %d", rec.Code)
	}
	var meta goalsMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if !meta.Found || !meta.DefaultView || meta.Error != "" {
		t.Fatalf("meta=%+v, want found/default without error", meta)
	}
	if got := rec.Header().Get("Server-Timing"); got == "" {
		t.Error("metadata endpoint must expose Server-Timing")
	}
}
