package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

// recycleAbortClass classifies a recycle failure for escalation copy (#436 / #443).
type recycleAbortClass string

const (
	abortBusyDesk    recycleAbortClass = "busy-desk"
	abortPhase2Close recycleAbortClass = "phase-2-close"
	abortHandoff     recycleAbortClass = "handoff"
	abortSelf        recycleAbortClass = "self-recycle"
	abortOther       recycleAbortClass = "other"
)

// classifyRecycleAbort maps an error string to an abort class (pure).
func classifyRecycleAbort(err error) recycleAbortClass {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "phase 0:") || strings.Contains(s, "did not settle to idle"):
		return abortBusyDesk
	case strings.Contains(s, "phase 2 re-verify") && strings.Contains(s, "no longer idle"):
		return abortBusyDesk
	case strings.Contains(s, "phase 2:") && (strings.Contains(s, "did not confirm") || strings.Contains(s, "closing")):
		return abortPhase2Close
	case strings.Contains(s, "phase 1:") || strings.Contains(s, "handoff"):
		return abortHandoff
	case strings.Contains(s, "own pane") || strings.Contains(s, "self"):
		return abortSelf
	default:
		return abortOther
	}
}

// isRetryableBusy reports whether the abort is a busy-desk class that daemon/CLI
// may re-attempt after a short wait (#436 busy-desk retry).
func isRetryableBusy(err error) bool {
	return classifyRecycleAbort(err) == abortBusyDesk
}

// recycleAbortNotice builds the operator/coordinator-facing escalation body (#436).
// Pure — no I/O.
func recycleAbortNotice(agent, phase string, class recycleAbortClass, err error, handoffPath string) string {
	var b strings.Builder
	b.WriteString("[flotilla recycle ABORT] Desk ")
	b.WriteString(agent)
	b.WriteString(" recycle failed")
	if class != "" {
		b.WriteString(" (class=")
		b.WriteString(string(class))
		b.WriteString(")")
	}
	if phase != "" {
		b.WriteString(" at ")
		b.WriteString(phase)
	}
	b.WriteString(".\n")
	if err != nil {
		b.WriteString("Error: ")
		b.WriteString(err.Error())
		b.WriteString("\n")
	}
	if handoffPath != "" {
		b.WriteString("Handoff path (may be durable): ")
		b.WriteString(handoffPath)
		b.WriteString("\n")
	}
	b.WriteString("Prescribed recovery:\n")
	switch class {
	case abortBusyDesk:
		b.WriteString("  - Wait for the desk to settle Idle, then: flotilla recycle ")
		b.WriteString(agent)
		b.WriteString("\n")
	case abortPhase2Close:
		b.WriteString("  - Investigate the pane; if confirmed dead: flotilla resume ")
		b.WriteString(agent)
		b.WriteString(" --force\n")
		b.WriteString("  - If still live: do NOT relaunch; re-run recycle after Idle, or heal subagent/exit dialogs\n")
	default:
		b.WriteString("  - Read the error; if desk closed without takeover: flotilla resume ")
		b.WriteString(agent)
		b.WriteString(" (add --force if resume refuses a live pane)\n")
	}
	b.WriteString("This abort MUST reach the coordinator — never log-only (#436).")
	return b.String()
}

// escalateRecycleAbort surfaces a failed recycle to the owning coordinator's pane
// and a durable sidecar under ~/.flotilla/<agent>/ (#436). Best-effort: never
// masks the original recycle error.
func escalateRecycleAbort(cfg *roster.Config, agent string, runErr error, handoffPath string) {
	if runErr == nil || agent == "" {
		return
	}
	class := classifyRecycleAbort(runErr)
	phase := ""
	if msg := runErr.Error(); strings.Contains(msg, "phase ") {
		// e.g. "phase 2: …"
		if i := strings.Index(msg, "phase "); i >= 0 {
			rest := msg[i:]
			if j := strings.IndexAny(rest, ":"); j > 0 {
				phase = strings.TrimSpace(rest[:j])
			}
		}
	}
	notice := recycleAbortNotice(agent, phase, class, runErr, handoffPath)
	log.Printf("flotilla: recycle: ESCALATE %s", notice)

	// Durable sidecar next to last-recycle.json so a successor finds it without the log.
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".flotilla", agent)
		_ = os.MkdirAll(dir, 0o700)
		path := filepath.Join(dir, "last-recycle-abort.txt")
		body := fmt.Sprintf("%s\n---\n%s\n", time.Now().UTC().Format(time.RFC3339), notice)
		if werr := os.WriteFile(path, []byte(body), 0o600); werr != nil {
			log.Printf("flotilla: recycle: abort sidecar write failed: %v", werr)
		}
	}

	if cfg == nil {
		return
	}
	owner := cfg.OwningXO(agent, cfg.XOAgent)
	if owner == "" {
		owner = cfg.XOAgent
	}
	if owner == "" || owner == agent {
		// No separate coordinator — still try CosAgent.
		if cfg.CosAgent != "" && cfg.CosAgent != agent {
			owner = cfg.CosAgent
		} else {
			return
		}
	}
	// Inject into the coordinator pane (side channel — not the aborted desk).
	drv, ok := surface.Get(agentSurface(cfg, owner))
	if !ok {
		log.Printf("flotilla: recycle: abort escalate: no surface for owner %q", owner)
		return
	}
	pane, err := deliver.ResolvePane(agentTitle(cfg, owner))
	if err != nil {
		log.Printf("flotilla: recycle: abort escalate: resolve owner %q pane: %v", owner, err)
		return
	}
	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if err := confirm.Submit(drv, pane, notice); err != nil {
		log.Printf("flotilla: recycle: abort escalate deliver to %q failed: %v", owner, err)
		return
	}
	log.Printf("flotilla: recycle: abort escalated to coordinator %q for desk %q", owner, agent)
}
