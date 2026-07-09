package workspace

import (
	"fmt"
	"path/filepath"
)

// IdentityHome resolves where a desk's native identity file lives. When worktreeCwd is a
// non-empty absolute path (from the flat launch recipe), identity lives in the worktree.
// Otherwise identity stays under ~/.flotilla/<agent>/ (legacy bare-dir desks).
func IdentityHome(agent, surface, worktreeCwd string) (dir, identityFile string, err error) {
	identityFile, err = IdentityFileName(surface)
	if err != nil {
		return "", "", err
	}
	if worktreeCwd != "" {
		if !filepath.IsAbs(worktreeCwd) {
			return "", "", fmt.Errorf("launch recipe for %q: cwd %q is not absolute", agent, worktreeCwd)
		}
		return worktreeCwd, identityFile, nil
	}
	hostDir, err := Dir(agent)
	if err != nil {
		return "", "", err
	}
	return hostDir, identityFile, nil
}
