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
	if len(doc.Goals) != 2 {
		t.Fatalf("expected 2 goals, got %d", len(doc.Goals))
	}
	jsonPath := filepath.Join(dir, "fleet-goals.json")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("compiled json missing: %v", err)
	}
}
