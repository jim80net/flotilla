package opencodeperm

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSeedNarrowRecyclePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(`{"plugin":["alpha"],"permission":{"edit":"ask","bash":"deny"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(dir, "worktree")
	changed, err := Seed(path, cwd)
	if err != nil || !changed {
		t.Fatalf("Seed changed=%v err=%v", changed, err)
	}
	var doc map[string]any
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	permission := doc["permission"].(map[string]any)
	edit := permission["edit"].(map[string]any)
	if edit["*"] != "ask" || edit[handoffEditPattern] != "allow" {
		t.Fatalf("edit rules = %#v", edit)
	}
	if permission["bash"] != "deny" {
		t.Fatalf("bash policy changed = %#v", permission["bash"])
	}
	if plugin := doc["plugin"].([]any); len(plugin) != 1 || plugin[0] != "alpha" {
		t.Fatalf("unrelated config changed: %#v", doc)
	}
	changed, err = Seed(path, cwd)
	if err != nil || changed {
		t.Fatalf("idempotent Seed changed=%v err=%v", changed, err)
	}
}

func TestSeedRefusesInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte(`{/* keep */}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Seed(path, "/tmp/alpha"); err == nil {
		t.Fatal("Seed must refuse invalid JSON")
	}
}

func TestSeedJSONCPreservesCommentsAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.jsonc")
	raw := []byte("{\n  // project policy stays documented\n  \"plugin\": [\"alpha\"],\n  \"permission\": {\n    \"edit\": {\n      // preserve edit-map documentation\n      \"*\": \"ask\",\n      \"z*\": \"ask\",\n    },\n    \"bash\": {\n      // preserve bash-map documentation\n      \"*\": \"deny\",\n      \"z*\": \"ask\",\n      \"rm -f /tmp/alpha/.flotilla/handoffs/recycle-*.md\": \"allow\",\n      \"rm -f \\\"/tmp/alpha/.flotilla/handoffs/recycle-*.md\\\"\": \"allow\",\n    },\n  },\n}\n")
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	changed, err := Seed(path, "/tmp/alpha")
	if err != nil || !changed {
		t.Fatalf("Seed changed=%v err=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{[]byte("// project policy stays documented"), []byte("// preserve edit-map documentation"), []byte("// preserve bash-map documentation"), []byte(handoffEditPattern)} {
		if !bytes.Contains(got, want) {
			t.Fatalf("JSONC output lost %q:\n%s", want, got)
		}
	}
	if bytes.Contains(got, []byte("rm -f")) {
		t.Fatalf("JSONC retained obsolete desk-side cleanup permission:\n%s", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
	changed, err = Seed(path, "/tmp/alpha")
	if err != nil || changed {
		t.Fatalf("idempotent JSONC Seed changed=%v err=%v", changed, err)
	}
}

func TestSeedEffectiveIncludesOverridingProjectLayers(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "config", "opencode.json")
	cwd := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(filepath.Join(cwd, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(cwd, "opencode.json")
	nested := filepath.Join(cwd, ".opencode", "opencode.json")
	for _, path := range []string{root, nested} {
		if err := os.WriteFile(path, []byte(`{"permission":{"edit":"ask","bash":"ask"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := SeedEffective(user, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 3 {
		t.Fatalf("changed = %v, want user + two project layers", changed)
	}
	for _, path := range []string{user, root, nested} {
		var doc map[string]any
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		permission := doc["permission"].(map[string]any)
		if permission["edit"].(map[string]any)[handoffEditPattern] != "allow" {
			t.Fatalf("%s lacks narrow edit rule: %#v", path, permission)
		}
	}
}

func TestSeedWritesManagedRuleLast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	cwd := "/tmp/alpha"
	// z* sorts after the managed edit pattern and also matches it. If Go's
	// normal map ordering leaks through, OpenCode's last-match evaluator asks.
	input := `{"permission":{"edit":{"z*":"ask"},"bash":{"z*":"ask"}}}`
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Seed(path, cwd); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	managedAt := bytes.Index(raw, []byte(`".flotilla/handoffs/recycle-*.md"`))
	if managedAt < 0 {
		t.Fatalf("managed edit rule missing from %s", raw)
	}
	if zAt := bytes.LastIndex(raw[:managedAt], []byte(`"z*"`)); zAt < 0 {
		t.Fatalf("managed rule was not emitted after existing z* rule: %s", raw)
	}
}

func TestSeedRetractsDeskSideCleanupRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	cwd := "/tmp/alpha"
	glob := "/tmp/alpha/.flotilla/handoffs/recycle-*.md"
	input := `{"permission":{"edit":"ask","bash":{"*":"ask","rm -f /tmp/alpha/.flotilla/handoffs/recycle-*.md":"allow","rm -f \"/tmp/alpha/.flotilla/handoffs/recycle-*.md\"":"allow"}}}`
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Seed(path, cwd); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("rm -f "+glob)) || bytes.Contains(raw, []byte(`rm -f \"`+glob)) {
		t.Fatalf("desk-side cleanup permission survived coordinator-cleanup migration: %s", raw)
	}
}
