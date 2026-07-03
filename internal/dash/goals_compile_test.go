package dash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHandleGoals_CompileOnLoadFromYAML(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)

	yaml := `version: 1
goals:
  - id: product
    title: Product
    scope: fleet
  - id: dash
    title: Dashboard
    scope: project
    parent: product
`
	yamlPath := filepath.Join(dir, "fleet-goals.yaml")
	if err := os.WriteFile(yamlPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	// Touch yaml after a pause so mtime is strictly newer than any absent json.
	time.Sleep(10 * time.Millisecond)

	rec := doGet(t, srv, "/api/goals")
	if rec.Code != 200 {
		t.Fatalf("status code %d", rec.Code)
	}
	var doc GoalsDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !doc.Found {
		t.Fatalf("yaml should compile on load, got %+v", doc)
	}
	// The two AUTHORED goals compiled from the yaml must be present. (The doc may carry
	// additional roster-materialized desk cards — #324 Inc 2 — for channel members not in
	// the goals file, so assert the authored nodes by id rather than a raw count.)
	byID := make(map[string]RenderedGoal, len(doc.Goals))
	for _, g := range doc.Goals {
		byID[g.ID] = g
	}
	if _, ok := byID["product"]; !ok {
		t.Errorf("compiled doc missing authored goal 'product'; got %+v", doc.Goals)
	}
	if _, ok := byID["dash"]; !ok {
		t.Errorf("compiled doc missing authored goal 'dash'; got %+v", doc.Goals)
	}
	jsonPath := filepath.Join(dir, "fleet-goals.json")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("compiled json missing: %v", err)
	}
}
