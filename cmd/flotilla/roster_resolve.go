package main

import (
	"os"
	"path/filepath"

	"github.com/jim80net/flotilla/internal/roster"
)

// resolveRosterPath applies #615 discovery when the flag path is empty or the
// relative default is missing. An explicit path that exists wins; a missing
// non-default explicit path falls through to discovery so worktree cwd still works.
func resolveRosterPath(flagValue string) (string, error) {
	if env := os.Getenv("FLOTILLA_ROSTER"); env != "" {
		if st, err := os.Stat(env); err == nil && !st.IsDir() {
			return absOr(env), nil
		}
		// Fail closed when env is set but missing — do not silently walk.
		cwd, _ := os.Getwd()
		home, _ := os.UserHomeDir()
		_, err := roster.DiscoverRoster(roster.DiscoverOptions{
			EnvRoster: env,
			Cwd:       cwd,
			Home:      home,
			SelfAgent: os.Getenv("FLOTILLA_SELF"),
		})
		return "", err
	}
	if flagValue != "" {
		if st, err := os.Stat(flagValue); err == nil && !st.IsDir() {
			return absOr(flagValue), nil
		}
	}
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	res, err := roster.DiscoverRoster(roster.DiscoverOptions{
		Cwd:       cwd,
		Home:      home,
		SelfAgent: os.Getenv("FLOTILLA_SELF"),
	})
	if err != nil {
		// Preserve historical default for callers that pass flotilla.json and expect
		// Load's own missing-file error when discovery finds nothing.
		if flagValue != "" {
			return flagValue, nil
		}
		return "", err
	}
	return res.Path, nil
}

func absOr(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
