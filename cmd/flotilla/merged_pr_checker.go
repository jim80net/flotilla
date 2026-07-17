package main

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
)

const mergedPRCacheTTL = 2 * time.Minute

type mergedPRCacheEntry struct {
	merged  bool
	checked time.Time
}

// newMergedPRChecker resolves PR state in each recipient's declared authority
// domain. Failures fail open for delivery (never suppress): transient GitHub or
// credential trouble must not discard work. The short cache keeps the watch
// tick from turning pending inbound rows into GitHub polling traffic.
func newMergedPRChecker(currentRoster func() *roster.Config) func(string, int) bool {
	var mu sync.Mutex
	cache := map[string]mergedPRCacheEntry{}
	return func(recipient string, pr int) bool {
		if pr <= 0 || currentRoster == nil {
			return false
		}
		cfg := currentRoster()
		if cfg == nil {
			return false
		}
		agent, err := cfg.Agent(recipient)
		if err != nil || agent.PrimaryRepo == "" {
			return false
		}
		key := agent.PrimaryRepo + "#" + strconv.Itoa(pr)
		now := time.Now()
		mu.Lock()
		if hit, ok := cache[key]; ok && now.Sub(hit.checked) < mergedPRCacheTTL {
			mu.Unlock()
			return hit.merged
		}
		mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "gh", "pr", "view", strconv.Itoa(pr), "--repo", agent.PrimaryRepo, "--json", "state", "--jq", ".state").Output()
		merged := err == nil && strings.TrimSpace(string(out)) == "MERGED"
		mu.Lock()
		cache[key] = mergedPRCacheEntry{merged: merged, checked: now}
		mu.Unlock()
		return merged
	}
}

func newCommitOnMainChecker(currentRoster func() *roster.Config) func(string, string) bool {
	return func(recipient, sha string) bool {
		if currentRoster == nil || sha == "" {
			return false
		}
		cfg := currentRoster()
		if cfg == nil {
			return false
		}
		agent, err := cfg.Agent(recipient)
		if err != nil || agent.WorktreePath == "" {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// Prefer the locally updated remote-tracking main used for merge/deploy
		// gates; fall back to the checked-out main branch when no remote exists.
		for _, mainRef := range []string{"origin/main", "main"} {
			cmd := exec.CommandContext(ctx, "git", "-C", agent.WorktreePath, "merge-base", "--is-ancestor", sha, mainRef)
			if cmd.Run() == nil {
				return true
			}
		}
		return false
	}
}
