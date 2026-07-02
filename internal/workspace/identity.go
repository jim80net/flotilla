package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jim80net/flotilla/internal/launch"
)

// IdentityHome resolves where a desk's native identity file lives. Worktree desks
// store identity in launch.json cwd; legacy bare-dir desks keep it under
// ~/.flotilla/<agent>/ until their next rotation migration.
func IdentityHome(agent, surface string) (dir, identityFile string, err error) {
	identityFile, err = IdentityFileName(surface)
	if err != nil {
		return "", "", err
	}
	hostDir, err := Dir(agent)
	if err != nil {
		return "", "", err
	}
	raw, err := os.ReadFile(filepath.Join(hostDir, LaunchFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return hostDir, identityFile, nil
		}
		return "", "", err
	}
	recipePath := filepath.Join(hostDir, LaunchFileName)
	var r launch.Recipe
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", "", fmt.Errorf("parse workspace recipe %q: %w", recipePath, err)
	}
	if r.Cwd == "" {
		return hostDir, identityFile, nil
	}
	if !filepath.IsAbs(r.Cwd) {
		return "", "", fmt.Errorf("workspace recipe %q: cwd %q is not absolute", recipePath, r.Cwd)
	}
	return r.Cwd, identityFile, nil
}
