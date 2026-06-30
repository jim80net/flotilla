package main

import (
	"log"
	"os"
	"os/exec"
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

// newRateLimitAutoSwitchDispatch builds the detector's RateLimitAutoSwitch callback: argv-array
// exec of `flotilla switch <agent> --auto`, storm-cooldown recording, cap gate, log side-channel
// only. endFlight must be the detector's AutoSwitchFlight.End bound method.
func newRateLimitAutoSwitchDispatch(cfg *roster.Config, rosterPath, launchPath string, flat *launch.Config, endFlight func(string)) func([]watch.RateLimitAutoSwitchCandidate) {
	bin := "flotilla"
	if exe, err := os.Executable(); err == nil {
		bin = exe
	}
	return func(candidates []watch.RateLimitAutoSwitchCandidate) {
		now := time.Now()
		for _, c := range candidates {
			agent := c.Agent
			scope := mapRateLimitScope(c.Scope)

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
			}(agent, cmd)
		}
	}
}
