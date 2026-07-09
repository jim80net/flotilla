package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
	"github.com/jim80net/flotilla/internal/workspace"
)

func mapRateLimitScope(s surface.RateLimitScope) RateLimitScope {
	switch s {
	case surface.RateLimitAccountSide:
		return RateLimitAccountSide
	default:
		return RateLimitServerSide
	}
}

// autoSwitchHooks are optional side channels after a successful auto-switch (#510).
type autoSwitchHooks struct {
	// afterSuccess is called with the agent that switched (coordinator re-notify, etc.).
	afterSuccess func(agent string)
}

// newRateLimitAutoSwitchDispatch builds the detector's RateLimitAutoSwitch callback: argv-array
// exec of `flotilla switch <agent> --auto`, storm-cooldown recording, cap gate, log side-channel
// only. probeMaterial is the in-process RateLimitMaterial callback (streak state lives in the
// watch process, not the switch subprocess). endFlight must be the detector's AutoSwitchFlight.End.
//
// Policy notes (#205 / #510):
//   - "Sustained" throttle for the SWITCH decision = RateLimitProbe's 2-consecutive-read debounce
//     in the watch process, edge-triggered once per episode (rateLimitActive).
//   - Storm threshold (≥2 reports / 10m) gates only failover-target POISON (provider-cooldowns.json),
//     not whether a switch fires — do not conflate the two.
//   - Coordinators are eligible (#510); restore to primary is a separate auto-revert dispatch.
func newRateLimitAutoSwitchDispatch(cfg *roster.Config, rosterPath, launchPath string, flat *launch.Config, probeMaterial func(agent string) (bool, surface.RateLimitScope, string, bool), endFlight func(string), hooks autoSwitchHooks) func([]watch.RateLimitAutoSwitchCandidate) {
	bin := "flotilla"
	if exe, err := os.Executable(); err == nil {
		bin = exe
	}
	return func(candidates []watch.RateLimitAutoSwitchCandidate) {
		now := time.Now()
		for _, c := range candidates {
			agent := c.Agent
			scope := mapRateLimitScope(c.Scope)

			// In-process final guard: materiality streak lives here; the switch subprocess must
			// not re-derive it from an empty globalRateLimitStreak.
			if limited, _, _, ok := probeMaterial(agent); !ok || !limited {
				log.Printf("flotilla watch: auto-switch %q: throttle cleared before dispatch — skip", agent)
				endFlight(agent)
				continue
			}

			times, err := loadAutoSwitchCapTimes(agent)
			if err != nil {
				log.Printf("flotilla watch: auto-switch %q: cap load failed: %v", agent, err)
				endFlight(agent)
				continue
			}
			capDec := switchCapDecision(times, now, autoSwitchCapWindow, defaultAutoSwitchCap, false)
			if !capDec.Allowed {
				if capDec.CapJustExhausted {
					log.Printf("flotilla watch: auto-switch cap exhausted for %q (%d in window) — desk stays on current harness", agent, capDec.InWindowCount)
				}
				endFlight(agent)
				continue
			}

			chain, err := workspace.ResolveActiveRecipe(agent, flat)
			if err != nil {
				log.Printf("flotilla watch: auto-switch %q: resolve recipe failed: %v", agent, err)
				endFlight(agent)
				continue
			}
			fromSurface := agentSurface(cfg, agent)
			provider, sub := fromSlotMeta(chain, fromSurface)
			if _, err := recordProviderStorm(provider, sub, scope, now); err != nil {
				log.Printf("flotilla watch: auto-switch %q: storm record failed: %v", agent, err)
				endFlight(agent)
				continue
			}

			scopeFlag := "server-side"
			if scope == RateLimitAccountSide {
				scopeFlag = "account-side"
			}
			args := []string{"switch", agent, "--auto", "--rate-limit-scope", scopeFlag, "--roster", rosterPath}
			if launchPath != "" {
				args = append(args, "--launch", launchPath)
			}
			cmd := exec.Command(bin, args...)
			go func(agent string, cmd *exec.Cmd) {
				defer endFlight(agent)
				out, err := cmd.CombinedOutput()
				if err != nil {
					log.Printf("flotilla watch: auto-switch %q failed: %v\n%s", agent, err, out)
					return
				}
				if err := recordAutoSwitchCap(agent, time.Now()); err != nil {
					log.Printf("flotilla watch: auto-switch %q: cap record failed: %v", agent, err)
				}
				log.Printf("flotilla watch: auto-switch %q completed", agent)
				if hooks.afterSuccess != nil {
					hooks.afterSuccess(agent)
				}
			}(agent, cmd)
		}
	}
}

// leaderExhaustionAlertBody is the loud operator-facing notice when a coordinator hits
// usage limits (#510). Keep generic — no deployment identifiers.
func leaderExhaustionAlertBody(agent string, scope surface.RateLimitScope) string {
	return fmt.Sprintf(
		"LEADER EXHAUSTION: coordinator %q hit a material %s usage/rate limit. "+
			"Watch is resuscitating via auto-switch (launch failover chain) when eligible; "+
			"verify the seat is back on a healthy tier. Adjutant (if any) must escalate, not ignore.",
		agent, scope.String())
}

// leaderExhaustionAdjutantBody is the urgent adjutant detector job for leader rate-limit (#510).
func leaderExhaustionAdjutantBody(leader string, scope surface.RateLimitScope, charterPath string) string {
	var b strings.Builder
	b.WriteString("[flotilla adjutant] URGENT — leader exhaustion signal for ")
	b.WriteString(leader)
	b.WriteString(" (")
	b.WriteString(scope.String())
	b.WriteString(" rate-limit / usage wall).\n\n")
	b.WriteString("Required duty (not optional):\n")
	b.WriteString("1. ACK recognition — this is not silent ignorance; the leader may be unresponsive.\n")
	b.WriteString("2. ESCALATE LOUDLY to the operator (surface channel / turn-final) naming the leader and the limit class.\n")
	b.WriteString("3. Do NOT invent a full seat relaunch yourself unless your charter grants solo recovery — ")
	b.WriteString("the watch daemon owns mechanical resuscitation (auto-switch) when enabled.\n")
	b.WriteString("4. After resuscitation, help re-brief subordinates if the leader's context was interrupted.\n")
	if charterPath != "" {
		b.WriteString(adjutantCharterGovernanceLine(charterPath))
	}
	return b.String()
}

// coordinatorResuscitationNotifyBody is injected to AgentsBelow after a successful
// coordinator auto-switch (#510 re-notify).
func coordinatorResuscitationNotifyBody(leader, toSurface string) string {
	return fmt.Sprintf(
		"[flotilla] Your coordinator %s was resuscitated onto harness %q after a usage/rate-limit episode. "+
			"Continue authorized work; if you were mid-handoff or waiting on the leader, re-surface status once they settle.",
		leader, toSurface)
}
