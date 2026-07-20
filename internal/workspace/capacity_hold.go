package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	CapacityHoldFileName = "capacity-hold.json"
	CapacityHoldSchema   = "flotilla.capacity_hold/v1"
)

// CapacityHold is the fleet-ops-owned, host-local guard that prevents a seat
// from being restored onto capacity that is known to be unavailable. Unknown
// fields are intentionally tolerated so operations can add evidence without
// coupling the recovery CLI to the producer's full document.
type CapacityHold struct {
	Schema          string   `json:"schema"`
	Class           string   `json:"class"`
	PolicyID        string   `json:"policy_id"`
	Status          string   `json:"status"`
	ForbidPrimary   bool     `json:"forbid_primary"`
	ForbidSurfaces  []string `json:"forbid_surfaces"`
	HardLimitUntil  string   `json:"hard_limit_until"`
	RestoreAfter    string   `json:"restore_after"`
	RestoreRequires string   `json:"restore_requires"`
	Conservation    struct {
		PolicyID string `json:"policy_id"`
	} `json:"conservation"`
	SurfaceConservation struct {
		PolicyID string `json:"policy_id"`
	} `json:"surface_conservation"`
}

// TargetAuthorization is an explicit authorization for a conservation-only
// primary launch. Its zero value is intentionally unauthorised.
type TargetAuthorization struct {
	ScheduledE2E bool
}

// EnforceCapacityHold refuses a primary or explicitly forbidden target while
// the seat's sticky capacity hold is active. It is deliberately called after
// target resolution but before any handoff, pane, trust, or overlay mutation.
//
// A malformed hold fails closed for primary recovery but never prevents a
// fallback recovery: operators must retain a path off the exhausted surface.
func EnforceCapacityHold(agent, operation, targetSlot, targetSurface string, now time.Time) error {
	return EnforceHarnessTarget(agent, operation, targetSlot, targetSurface, now, TargetAuthorization{})
}

// EnforceHarnessTarget is the shared pre-mutation boundary for every path that
// can launch or select a harness. Capacity availability and e2e-only
// conservation are independent: clearing a hard-limit window does not clear a
// conservation policy. Scheduled e2e/canary authorization only waives the
// latter; it never overrides an active or explicit capacity hold.
func EnforceHarnessTarget(agent, operation, targetSlot, targetSurface string, now time.Time, auth TargetAuthorization) error {
	dir, err := Dir(agent)
	if err != nil {
		return capacityHoldMalformedError(agent, operation, targetSlot, targetSurface, "resolve workspace", err)
	}
	path := filepath.Join(dir, CapacityHoldFileName)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return capacityHoldMalformedError(agent, operation, targetSlot, targetSurface, path, err)
	}

	var hold CapacityHold
	if err := json.Unmarshal(b, &hold); err != nil {
		return capacityHoldMalformedError(agent, operation, targetSlot, targetSurface, path, err)
	}
	if hold.Schema != CapacityHoldSchema {
		return capacityHoldMalformedError(agent, operation, targetSlot, targetSurface, path, fmt.Errorf("unsupported schema %q", hold.Schema))
	}

	active, until, err := hold.active(now)
	if err != nil {
		return capacityHoldMalformedError(agent, operation, targetSlot, targetSurface, path, err)
	}

	primary := strings.EqualFold(strings.TrimSpace(targetSlot), SlotPrimary)
	forbiddenPrimary := primary && hold.ForbidPrimary
	forbiddenSurface := containsFold(hold.ForbidSurfaces, targetSurface)
	capacityBlocked := forbiddenPrimary || forbiddenSurface || (active && primary)
	if !capacityBlocked {
		if policy := hold.e2eOnlyPolicy(); policy != "" && targetsPrimaryCodex(targetSlot, targetSurface) && !auth.ScheduledE2E {
			return fmt.Errorf("refusing %s for %q to slot %q (surface %q): e2e-only conservation policy %q in %s requires an explicit scheduled e2e/canary authorization; current fallback retained, desk untouched", operation, agent, targetSlot, targetSurface, policy, path)
		}
		return nil
	}

	reason := "capacity hold explicitly forbids the target"
	if active {
		reason = "capacity hold is ACTIVE"
	}
	if until != "" {
		reason += " until " + until
	}
	if forbiddenPrimary && !active {
		reason += " and forbids primary"
	}
	if forbiddenSurface {
		reason += fmt.Sprintf(" and forbids surface %q", targetSurface)
	}
	return fmt.Errorf("refusing %s for %q to slot %q (surface %q): %s in %s; desk untouched", operation, agent, targetSlot, targetSurface, reason, path)
}

func (h CapacityHold) e2eOnlyPolicy() string {
	for _, candidate := range []string{h.PolicyID, h.Conservation.PolicyID, h.SurfaceConservation.PolicyID, h.Class} {
		candidate = strings.TrimSpace(candidate)
		if strings.Contains(strings.ToLower(candidate), "e2e-only") {
			return candidate
		}
	}
	// Older fleet-ops documents carried the contract only in restore_requires.
	// Honor that explicit source instead of silently allowing a primary launch.
	requires := strings.ToLower(h.RestoreRequires)
	if strings.Contains(requires, "scheduled e2e") || strings.Contains(requires, "scheduled canary") {
		return "e2e-only (restore_requires)"
	}
	return ""
}

func targetsPrimaryCodex(slot, surface string) bool {
	return strings.EqualFold(strings.TrimSpace(slot), SlotPrimary) || strings.EqualFold(strings.TrimSpace(surface), "codex")
}

func (h CapacityHold) active(now time.Time) (bool, string, error) {
	sticky := strings.EqualFold(strings.TrimSpace(h.Status), "ACTIVE")
	deadline := strings.TrimSpace(h.HardLimitUntil)
	if deadline == "" {
		deadline = strings.TrimSpace(h.RestoreAfter)
	}
	if deadline == "" {
		return sticky, "", nil
	}
	until, err := time.Parse(time.RFC3339, deadline)
	if err != nil {
		return false, "", fmt.Errorf("invalid capacity deadline %q: %w", deadline, err)
	}
	return sticky || now.Before(until), until.UTC().Format(time.RFC3339), nil
}

func capacityHoldMalformedError(agent, operation, targetSlot, targetSurface, path string, cause error) error {
	if !targetsPrimaryCodex(targetSlot, targetSurface) {
		return nil
	}
	return fmt.Errorf("refusing %s for %q to primary (surface %q): capacity hold %s is unreadable or invalid: %v; desk untouched", operation, agent, targetSurface, path, cause)
}

func containsFold(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}
