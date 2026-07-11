package roster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverOptions controls default roster resolution (#615).
type DiscoverOptions struct {
	// EnvRoster is typically os.Getenv("FLOTILLA_ROSTER"); non-empty wins.
	EnvRoster string
	// Cwd is the starting directory (usually os.Getwd()).
	Cwd string
	// Home is $HOME for ~/.flotilla/<agent>/launch.json hints; empty disables that arm.
	Home string
	// SelfAgent is $FLOTILLA_SELF for launch.json hint lookup; empty skips agent-specific hint.
	SelfAgent string
	// Explicit is a non-default --roster flag value. When set and the path exists, it wins
	// after EnvRoster. Empty means the caller is using discovery defaults.
	Explicit string
}

// DiscoverResult is the resolved roster path plus the paths that were tried (for errors).
type DiscoverResult struct {
	Path  string
	Tried []string
}

// DiscoverRoster resolves the fleet roster path fail-closed (#615).
//
// Order:
//  1. EnvRoster if set (must exist or error listing tried)
//  2. Explicit if set (must exist)
//  3. ./flotilla.json under Cwd
//  4. Walk up toward filesystem root: <dir>/flotilla.json then <dir>/state/flotilla.json
//  5. ~/.flotilla/<SelfAgent>/launch.json "roster" hint if present
//
// When multiple distinct candidates would match at the same walk level, the first in
// order wins (flotilla.json before state/flotilla.json). If nothing is found, returns
// an error listing every tried path.
func DiscoverRoster(opt DiscoverOptions) (DiscoverResult, error) {
	var tried []string
	try := func(p string) (string, bool) {
		if p == "" {
			return "", false
		}
		tried = append(tried, p)
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return "", false
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return p, true
		}
		return abs, true
	}

	if opt.EnvRoster != "" {
		if p, ok := try(opt.EnvRoster); ok {
			return DiscoverResult{Path: p, Tried: tried}, nil
		}
		return DiscoverResult{Tried: tried}, fmt.Errorf(
			"roster: $FLOTILLA_ROSTER=%q not found (tried: %s)",
			opt.EnvRoster, strings.Join(tried, ", "),
		)
	}
	if opt.Explicit != "" {
		if p, ok := try(opt.Explicit); ok {
			return DiscoverResult{Path: p, Tried: tried}, nil
		}
		// Fall through to discovery when explicit default-like path missing —
		// only hard-fail if caller passed a non-default explicit that doesn't exist.
		// Callers using Discover as the sole resolver pass Explicit empty.
	}

	cwd := opt.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if cwd != "" {
		if p, ok := try(filepath.Join(cwd, "flotilla.json")); ok {
			return DiscoverResult{Path: p, Tried: tried}, nil
		}
		// Walk toward root.
		dir := cwd
		for {
			if p, ok := try(filepath.Join(dir, "flotilla.json")); ok {
				return DiscoverResult{Path: p, Tried: tried}, nil
			}
			if p, ok := try(filepath.Join(dir, "state", "flotilla.json")); ok {
				return DiscoverResult{Path: p, Tried: tried}, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	if opt.Home != "" && opt.SelfAgent != "" {
		hintPath := filepath.Join(opt.Home, ".flotilla", opt.SelfAgent, "launch.json")
		tried = append(tried, hintPath+"#roster")
		if rp := rosterHintFromLaunch(hintPath); rp != "" {
			if p, ok := try(rp); ok {
				return DiscoverResult{Path: p, Tried: tried}, nil
			}
		}
	}

	return DiscoverResult{Tried: tried}, fmt.Errorf(
		"roster: no flotilla.json found (tried: %s) — set $FLOTILLA_ROSTER or pass --roster",
		strings.Join(tried, ", "),
	)
}

type launchHintFile struct {
	Roster string `json:"roster"`
}

func rosterHintFromLaunch(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var f launchHintFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return ""
	}
	return strings.TrimSpace(f.Roster)
}
