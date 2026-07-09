package main

import (
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/workspace"
)

// autoRevertEligible is the pure eligibility predicate for restoring a seat to its
// preferred (primary) harness slot after usage limits clear (#510 / #466 phase 2).
//
// Rules:
//   - overlay present and slot is not primary (absent overlay ⇒ already on primary)
//   - primary provider is not under active poison cooldown
//
// Hysteresis (N consecutive clear probes) is owned by the detector; this helper only
// answers "is this seat on a degraded slot that may restore?"
func autoRevertEligible(agent string, poison PoisonState, now time.Time) bool {
	_ = now // reserved for future CooldownUntil-on-overlay checks
	ov, ok, err := workspace.ReadActiveOverlay(agent)
	if err != nil || !ok {
		return false
	}
	slot := ov.Slot
	if slot == "" || slot == workspace.SlotPrimary {
		return false
	}
	// Overlay names the CURRENT (fallback) slot. Primary-provider poison is checked
	// separately via primaryProviderPoisoned against the launch chain.
	_ = poison
	return true
}

// primaryProviderPoisoned reports whether the agent's primary slot provider is still
// under active poison (server-side) or its subscription is account-side poisoned.
func primaryProviderPoisoned(agent string, flat *launch.Config, poison PoisonState) bool {
	if flat == nil {
		return false
	}
	chain, err := workspace.ResolveActiveRecipe(agent, flat)
	if err != nil {
		return true // fail-closed: do not thrash restore when recipe is missing
	}
	slots := chain.Slots()
	if len(slots) == 0 {
		return true
	}
	primary := slots[0]
	if primary.Provider != "" && poison.Providers[primary.Provider] {
		return true
	}
	if primary.SubscriptionID != "" && poison.Subscriptions[primary.SubscriptionID] {
		return true
	}
	return false
}

// newRateLimitAutoRevertDispatch builds the detector's RateLimitAutoRevert callback:
// argv-array exec of `flotilla switch <agent> --to primary`, flight End, log side-channel.
func newRateLimitAutoRevertDispatch(rosterPath, launchPath string, flat *launch.Config, endFlight func(string)) func([]string) {
	bin := "flotilla"
	if exe, err := os.Executable(); err == nil {
		bin = exe
	}
	return func(agents []string) {
		now := time.Now()
		poison, err := loadActivePoison(now)
		if err != nil {
			log.Printf("flotilla watch: auto-revert: load poison failed: %v", err)
			for _, a := range agents {
				endFlight(a)
			}
			return
		}
		for _, agent := range agents {
			if !autoRevertEligible(agent, poison, now) {
				log.Printf("flotilla watch: auto-revert %q: not on degraded slot — skip", agent)
				endFlight(agent)
				continue
			}
			if primaryProviderPoisoned(agent, flat, poison) {
				log.Printf("flotilla watch: auto-revert %q: primary provider still poisoned — skip", agent)
				endFlight(agent)
				continue
			}
			args := []string{"switch", agent, "--to", workspace.SlotPrimary, "--roster", rosterPath}
			if launchPath != "" {
				args = append(args, "--launch", launchPath)
			}
			cmd := exec.Command(bin, args...)
			go func(agent string, cmd *exec.Cmd) {
				defer endFlight(agent)
				out, err := cmd.CombinedOutput()
				if err != nil {
					log.Printf("flotilla watch: auto-revert %q failed: %v\n%s", agent, err, out)
					return
				}
				log.Printf("flotilla watch: auto-revert %q completed — restored preferred tier", agent)
			}(agent, cmd)
		}
	}
}
