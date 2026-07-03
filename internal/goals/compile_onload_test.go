package goals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaybeCompileYAMLToJSON_CompilesWhenJSONAbsent(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "fleet-goals.yaml")
	jsonPath := filepath.Join(dir, "fleet-goals.json")
	if err := os.WriteFile(yamlPath, []byte(`version: 1
goals:
  - id: g
    title: Goal
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MaybeCompileYAMLToJSON(yamlPath, jsonPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("compiled json missing: %v", err)
	}
}

func TestMaybeCompileYAMLToJSON_SkipsWhenJSONNewer(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "fleet-goals.yaml")
	jsonPath := filepath.Join(dir, "fleet-goals.json")
	if err := os.WriteFile(yamlPath, []byte(`version: 1
goals:
  - id: old
    title: Old
`), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(jsonPath, []byte(`{"version":1,"goals":[{"id":"fresh","title":"Fresh"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MaybeCompileYAMLToJSON(yamlPath, jsonPath); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"fresh"`) {
		t.Fatalf("newer json cache should not be overwritten, got %s", b)
	}
}
