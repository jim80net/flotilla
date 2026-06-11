package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jim80net/flotilla/internal/launch"
)

// LaunchFileName is the workspace's launch-recipe file.
const LaunchFileName = "launch.json"

// LoadRecipe reads ~/.flotilla/<agent>/launch.json as a SINGLE launch.Recipe (no
// agents map — the agent is the directory name) and validates it with the same rules
// the flat file uses (launch.ValidateRecipe). Returns:
//   - (recipe, true, nil)  when present and valid;
//   - (zero, false, nil)   when no workspace launch.json exists (the caller falls
//     back to the flat flotilla-launch.json — the migration path);
//   - (zero, false, err)   when the file is present but invalid or unreadable —
//     fail-closed, never resume on a malformed recipe.
func LoadRecipe(agent string) (launch.Recipe, bool, error) {
	dir, err := Dir(agent)
	if err != nil {
		return launch.Recipe{}, false, err
	}
	path := filepath.Join(dir, LaunchFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return launch.Recipe{}, false, nil // no workspace recipe → fall back
		}
		return launch.Recipe{}, false, fmt.Errorf("read workspace recipe %q: %w", path, err)
	}
	var r launch.Recipe
	if err := json.Unmarshal(raw, &r); err != nil {
		return launch.Recipe{}, false, fmt.Errorf("parse workspace recipe %q: %w", path, err)
	}
	if err := launch.ValidateRecipe(fmt.Sprintf("workspace recipe %q", path), r); err != nil {
		return launch.Recipe{}, false, err
	}
	return r, true, nil
}

// ResolveRecipe resolves an agent's launch recipe: the workspace launch.json first,
// else the flat launch.Config (the migration fallback), else a clear error naming both
// locations it looked in. flat may be nil (no flat file present).
func ResolveRecipe(agent string, flat *launch.Config) (launch.Recipe, error) {
	r, ok, err := LoadRecipe(agent)
	if err != nil {
		return launch.Recipe{}, err
	}
	if ok {
		return r, nil
	}
	if flat != nil {
		if r, ok := flat.Recipe(agent); ok {
			return r, nil
		}
	}
	dir, _ := Dir(agent)
	return launch.Recipe{}, fmt.Errorf(
		"no launch recipe for %q: neither %s nor the flat launch file has one",
		agent, filepath.Join(dir, LaunchFileName))
}
