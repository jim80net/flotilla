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

// ActiveHarnessFileName is the workspace's runtime active-harness overlay. It names
// the live failover slot a desk is currently running, so a mid-incident switch routes
// to the new harness with NO commit to the portable roster (the roster stays the
// declared default; the overlay is the runtime source of truth). Host-local, atomic.
const ActiveHarnessFileName = "active-harness.json"

// SlotPrimary is the canonical name of the primary (declared-default) harness slot.
// An ABSENT overlay means the primary slot; fallbacks are named "fallback-0",
// "fallback-1", … in chain order. These names are the overlay's `slot` vocabulary.
const SlotPrimary = "primary"

// ActiveOverlay is the parsed `~/.flotilla/<agent>/active-harness.json`: the live slot
// a desk is running plus the metadata a switch records. It is host-local and written
// atomically (WriteActiveOverlay); an absent overlay means the primary slot. The
// switch/cooldown machinery owns `Reason`/`CooldownUntil`/`PoisonedProviders`; the
// routing seam (ResolveHarness / agentSurface) reads `Slot`/`Surface`.
type ActiveOverlay struct {
	// Slot is the active slot's name ("primary"/"fallback-N"). Absent ⇒ primary.
	Slot string `json:"slot"`
	// Surface is the active slot's registered driver name (e.g. "grok"). Routing
	// (agentSurface) reads this BEFORE the roster surface so watch/send follow the
	// switch with no roster commit.
	Surface string `json:"surface,omitempty"`
	// Provider is the active slot's logical provider (e.g. "xai"), distinct from a
	// SubscriptionID (two subscriptions of one provider share a provider). Load-bearing
	// for failover poisoning.
	Provider string `json:"provider,omitempty"`
	// SubscriptionID is a billing/account bucket within a provider (NOT a secret).
	SubscriptionID string `json:"subscription_id,omitempty"`
	// SwitchedAt is when the switch that wrote this overlay completed (RFC3339).
	SwitchedAt string `json:"switched_at,omitempty"`
	// SwitchToken ties the overlay to the switch attempt that wrote it (idempotency).
	SwitchToken string `json:"switch_token,omitempty"`
	// Reason is a short operator-facing note (e.g. "server-side throttle").
	Reason string `json:"reason,omitempty"`
	// CooldownUntil, when set (RFC3339), is when this slot's provider/subscription
	// poison expires (the switch machinery's bookkeeping).
	CooldownUntil string `json:"cooldown_until,omitempty"`
	// PoisonedProviders is the set of providers this desk must NOT fail back onto until
	// their cooldown expires (the switch machinery's bookkeeping).
	PoisonedProviders []string `json:"poisoned_providers,omitempty"`
}

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

// ReadActiveOverlay reads `~/.flotilla/<agent>/active-harness.json`. Returns:
//   - (overlay, true, nil)  when present and parseable;
//   - (zero, false, nil)    when no overlay exists (⇒ the primary slot — the common
//     case for an un-switched desk);
//   - (zero, false, err)    when the file is present but unreadable or unparseable.
//
// Callers on the ROUTING/RESOLUTION hot path (ResolveHarness, agentSurface) treat the
// error case as fail-SAFE (fall back to primary/roster) — a torn overlay must never
// make a live desk unresolvable or unroutable. Callers that need to distinguish "no
// overlay" from "torn overlay" (e.g. --repair) get the third return to do so.
func ReadActiveOverlay(agent string) (ActiveOverlay, bool, error) {
	dir, err := Dir(agent)
	if err != nil {
		return ActiveOverlay{}, false, err
	}
	path := filepath.Join(dir, ActiveHarnessFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ActiveOverlay{}, false, nil // no overlay → primary slot
		}
		return ActiveOverlay{}, false, fmt.Errorf("read active-harness overlay %q: %w", path, err)
	}
	var ov ActiveOverlay
	if err := json.Unmarshal(raw, &ov); err != nil {
		return ActiveOverlay{}, false, fmt.Errorf("parse active-harness overlay %q: %w", path, err)
	}
	return ov, true, nil
}

// WriteActiveOverlay atomically writes an agent's active-harness overlay
// (temp-file + rename within the workspace dir, mirroring the recycle status record's
// atomic-rename — a partial write must never be read as a torn overlay). It creates the
// workspace dir if absent. The switch core calls this ONLY after a confirmed relaunch +
// marker read-back, so the overlay can never name a slot the pane is not actually
// running.
func WriteActiveOverlay(agent string, ov ActiveOverlay) error {
	dir, err := Dir(agent)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create workspace dir %q for the active-harness overlay: %w", dir, err)
	}
	data, err := json.MarshalIndent(ov, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal active-harness overlay: %w", err)
	}
	final := filepath.Join(dir, ActiveHarnessFileName)
	tmp, err := os.CreateTemp(dir, ActiveHarnessFileName+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp for the active-harness overlay: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write the active-harness overlay: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close the active-harness overlay temp: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("finalize the active-harness overlay: %w", err)
	}
	return nil
}

// ResolveHarness resolves the desk's LIVE harness slot: it (1) resolves the recipe
// chain via the EXISTING ResolveRecipe precedence (workspace launch.json → flat
// flotilla-launch.json), (2) reads the active-harness overlay's slot name, and (3)
// returns that slot's name and recipe-for-slot. An ABSENT overlay ⇒ the primary slot.
// A TORN/unreadable overlay is fail-SAFE: it falls back to the primary slot rather than
// erroring the desk out (a bad overlay must never make a live desk unresolvable). An
// overlay naming a slot the chain does NOT contain (a stale/torn slot reference) is
// likewise fail-safe to primary.
//
// The per-slot launch comes from the chain via launch.Recipe.Slots() (the single
// backward-compat helper: an undeclared chain synthesizes the flat Launch as the
// implied "primary" slot). The recipe-level Cwd/Tmux/State stay shared across slots —
// the DESK (worktree + pane) is stable across a switch; only the foreground harness
// process changes. The active SURFACE for a slot is carried on the overlay
// (ReadActiveOverlay.Surface) and read by the routing seam (agentSurface).
func ResolveHarness(agent string, flat *launch.Config) (string, launch.Recipe, error) {
	chain, err := ResolveRecipe(agent, flat)
	if err != nil {
		return "", launch.Recipe{}, err
	}
	ov, ok, ovErr := ReadActiveOverlay(agent)
	if ovErr != nil || !ok || ov.Slot == "" {
		// No overlay, a torn overlay, or an overlay that names no slot ⇒ fail-safe to
		// the primary slot. (A torn overlay is the fail-SAFE case, per the spec: never
		// error the desk out on a bad overlay.)
		return SlotPrimary, slotRecipe(chain, SlotPrimary), nil
	}
	r, found := slotRecipeByName(chain, ov.Slot)
	if !found {
		// The overlay names a slot the chain no longer has (stale chain edit / torn
		// overlay) ⇒ fail-safe to primary rather than resolving an absent slot.
		return SlotPrimary, slotRecipe(chain, SlotPrimary), nil
	}
	return ov.Slot, r, nil
}

// ResolveActiveRecipe is the recipe-shaped view of ResolveHarness, used by switch/resume
// relaunch where only the slot's recipe (launch + cwd + tmux) is needed.
func ResolveActiveRecipe(agent string, flat *launch.Config) (launch.Recipe, error) {
	_, r, err := ResolveHarness(agent, flat)
	return r, err
}

// slotRecipe returns the recipe for a named slot of a resolved chain, panicking-free:
// a name absent from the chain falls back to the resolved recipe unchanged (the primary
// path's launch). Callers that must DISTINGUISH "slot not in chain" use slotRecipeByName.
func slotRecipe(chain launch.Recipe, name string) launch.Recipe {
	if r, ok := slotRecipeByName(chain, name); ok {
		return r
	}
	return chain
}

// slotRecipeByName projects a named chain slot onto a recipe-for-slot: the slot's Launch
// replaces the recipe's foreground command while the shared desk fields (Cwd/Tmux/State)
// are preserved. It reports whether the chain contains the named slot. It consumes
// launch.Recipe.Slots() so the backward-compat rule (undeclared chain ⇒ implied primary)
// lives in ONE place (the launch package), never duplicated here.
func slotRecipeByName(chain launch.Recipe, name string) (launch.Recipe, bool) {
	for _, s := range chain.Slots() {
		if s.Name == name {
			out := chain // copy the shared desk fields (Cwd/Tmux/State, and the chain itself)
			out.Launch = s.Launch
			return out, true
		}
	}
	return launch.Recipe{}, false
}
