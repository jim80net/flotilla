package org

import (
	"fmt"
	"sort"
	"strings"
)

// Agree checks that a file-compiled DAG does not contradict the channel-derived
// primary parent edges (org-truth v1 PR2). For each agent present in the file
// DAG, if the derived graph asserts a non-empty primary parent, it MUST equal
// the file's reports_to (empty file parent is written as "" for the root).
//
// Agents only in the derived graph (not listed in the org file) are not checked
// — the org file need not enumerate every desk. Agents only in the file with a
// reports_to and no derived parent are allowed (org may declare structure
// channels do not yet express).
func Agree(fileDAG, derived *DAG) error {
	if fileDAG == nil {
		return fmt.Errorf("org-truth: agree: nil file DAG")
	}
	if derived == nil {
		return fmt.Errorf("org-truth: agree: nil derived DAG")
	}
	for id := range fileDAG.Nodes {
		fileParent := fileDAG.PrimaryParent(id)
		derParent := derived.PrimaryParent(id)
		if derParent == "" {
			continue // channels assert nothing — no contradiction
		}
		if derParent != fileParent {
			fp := fileParent
			if fp == "" {
				fp = "(none)"
			}
			return fmt.Errorf("org-truth: agent %q: org reports_to %q disagrees with channel-derived parent %q", id, fp, derParent)
		}
	}
	return nil
}

// CheckHomes enforces the one-home-channel-id invariant when an org file is present:
//
//  1. Each node declares at most one home_channel_id (schema field — always true).
//  2. home_channel_id values are unique across nodes.
//  3. An agent that is xo_agent of two or more non-fleet-command channels MUST
//     declare a home_channel_id that is one of those channels.
//
// ownedHomes maps agent name → non-fleet-command channel ids they own as xo_agent.
func CheckHomes(f *File, ownedHomes map[string][]string) error {
	if f == nil {
		return fmt.Errorf("org-truth: check homes: nil file")
	}
	declared := map[string]string{} // agent → home_channel_id
	seenCh := map[string]string{}   // channel id → agent
	for _, n := range f.Nodes {
		if n.HomeChannelID == "" {
			continue
		}
		if other, ok := seenCh[n.HomeChannelID]; ok {
			return fmt.Errorf("org-truth: home_channel_id %q claimed by both %q and %q", n.HomeChannelID, other, n.ID)
		}
		seenCh[n.HomeChannelID] = n.ID
		declared[n.ID] = n.HomeChannelID
	}
	for agent, homes := range ownedHomes {
		if len(homes) <= 1 {
			// Optional: if declared, it should match the single home when set.
			if h, ok := declared[agent]; ok && len(homes) == 1 && h != homes[0] {
				return fmt.Errorf("org-truth: agent %q home_channel_id %q does not match owned home %q", agent, h, homes[0])
			}
			continue
		}
		h, ok := declared[agent]
		if !ok || h == "" {
			sort.Strings(homes)
			return fmt.Errorf("org-truth: agent %q owns multiple home channels [%s]; declare one home_channel_id", agent, strings.Join(homes, ", "))
		}
		if !containsString(homes, h) {
			sort.Strings(homes)
			return fmt.Errorf("org-truth: agent %q home_channel_id %q is not among owned homes [%s]", agent, h, strings.Join(homes, ", "))
		}
	}
	return nil
}

func containsString(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}
