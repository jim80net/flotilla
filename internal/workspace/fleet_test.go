package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
)

func writeWS(t *testing.T, root, agent, body string) {
	t.Helper()
	dir := filepath.Join(root, agent)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LaunchFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func recipeJSON(tmux string) string {
	return fmt.Sprintf(`{"launch":"claude","cwd":"/abs","tmux":%q}`, tmux)
}

func TestFleetTmuxCheckCollisionAndClean(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	writeWS(t, root, "a", recipeJSON("flotilla:a"))
	writeWS(t, root, "b", recipeJSON("flotilla:shared"))
	writeWS(t, root, "c", recipeJSON("flotilla:shared")) // collides with b

	if w, err := FleetTmuxCheck("a", "flotilla:a", nil); err != nil || len(w) != 0 {
		t.Errorf("a (unique) should be clean: warns=%v err=%v", w, err)
	}
	if _, err := FleetTmuxCheck("c", "flotilla:shared", nil); err == nil {
		t.Error("c should collide with b on flotilla:shared")
	}
	if _, err := FleetTmuxCheck("a", "", nil); err != nil {
		t.Errorf("empty target must be a no-op: %v", err)
	}
}

func TestFleetTmuxCheckSkipsMalformedSibling(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	writeWS(t, root, "a", recipeJSON("flotilla:a"))
	writeWS(t, root, "bad", `{not json`)

	w, err := FleetTmuxCheck("a", "flotilla:a", nil)
	if err != nil {
		t.Fatalf("a broken sibling must NOT fail-close this agent: %v", err)
	}
	if len(w) == 0 {
		t.Error("expected a skip warning for the malformed sibling")
	}
}

func TestFleetTmuxCheckFlatUnionDuringMigration(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root) // no workspaces yet
	flat := &launch.Config{Agents: map[string]launch.Recipe{
		"x": {Launch: "c", Cwd: "/abs", Tmux: "flotilla:shared"},
	}}
	if _, err := FleetTmuxCheck("y", "flotilla:shared", flat); err == nil {
		t.Error("y should collide with the unmigrated flat agent x on flotilla:shared")
	}
}
