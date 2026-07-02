package workspace

import (
	"encoding/json"
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
	var r launch.Recipe
	if json.Unmarshal(raw, &r) == nil && r.Cwd != "" && filepath.IsAbs(r.Cwd) {
		return r.Cwd, identityFile, nil
	}
	return hostDir, identityFile, nil
}
