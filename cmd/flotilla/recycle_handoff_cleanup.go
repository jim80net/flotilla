package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	recycleHandoffMaxAge = 7 * 24 * time.Hour
	recycleSweepMax      = 32
)

// removeAbortedRecycleHandoff removes only the unique handoff owned by the failed
// attempt. A successor can therefore never mistake an aborted picture for a live one.
func removeAbortedRecycleHandoff(path string) error {
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect aborted handoff %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to remove aborted handoff %q: not a regular file", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove aborted handoff %q: %w", path, err)
	}
	log.Printf("flotilla: recycle: removed aborted handoff %s", path)
	return nil
}

type recycleHandoffCandidate struct {
	path    string
	modTime time.Time
	marked  bool
}

// sweepRecycleHandoffOrphans bounds accumulation without risking the newest unconsumed
// live handoff. It ignores symlinks and unrelated files, and caps removals per invocation.
func sweepRecycleHandoffOrphans(designatedPath string, now time.Time) (int, error) {
	dir := filepath.Dir(designatedPath)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read recycle handoff directory %q: %w", dir, err)
	}
	var candidates []recycleHandoffCandidate
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "recycle-") || !(strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".aborted")) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, fmt.Errorf("inspect recycle handoff %q: %w", filepath.Join(dir, name), err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		candidates = append(candidates, recycleHandoffCandidate{
			path: filepath.Join(dir, name), modTime: info.ModTime(), marked: strings.HasSuffix(name, ".aborted"),
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modTime.After(candidates[j].modTime) })
	newestLive := ""
	for _, candidate := range candidates {
		if !candidate.marked {
			newestLive = candidate.path
			break
		}
	}
	removed := 0
	for _, candidate := range candidates {
		// The newest ordinary handoff may be the live slot's unconsumed picture.
		if candidate.path == newestLive {
			continue
		}
		if !candidate.marked && now.Sub(candidate.modTime) < recycleHandoffMaxAge {
			continue
		}
		if removed == recycleSweepMax {
			break
		}
		if err := os.Remove(candidate.path); err != nil {
			return removed, fmt.Errorf("remove orphan recycle handoff %q: %w", candidate.path, err)
		}
		removed++
		log.Printf("flotilla: recycle: removed orphan handoff %s", candidate.path)
	}
	return removed, nil
}
