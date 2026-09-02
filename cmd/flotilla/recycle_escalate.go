package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/outbox"
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
	case strings.Contains(s, "uncooperative") || strings.Contains(s, "usage credit") ||
		(strings.Contains(s, "phase 1:") && strings.Contains(s, "resume") && strings.Contains(s, "--force")):
		// #558: credit/quota-exhausted sessions — still a handoff-class abort, but
		// recovery must point at resume --force (handled in recycleAbortNotice).
		return abortHandoff
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
		b.WriteString("  - If the desk is genuinely running a turn, let that turn finish, then retry recycle once\n")
		b.WriteString("  - If the composer appears idle but status remains Working/Composing, do NOT retry recycle: its idle gate cannot repair the stuck task state\n")
		b.WriteString("  - Recover the stuck state with: flotilla resume ")
		b.WriteString(agent)
		b.WriteString(" --force (this replaces the live session; use a verified durable handoff when context must survive)\n")
	case abortPhase2Close:
		b.WriteString("  - Investigate the pane; if confirmed dead: flotilla resume ")
		b.WriteString(agent)
		b.WriteString(" --force\n")
		b.WriteString("  - If still live: do NOT relaunch; re-run recycle after Idle, or heal subagent/exit dialogs\n")
	case abortHandoff:
		// #558: uncooperative (credits/quota) must not look like a slow-delivery false-negative.
		if err != nil && (strings.Contains(err.Error(), "uncooperative") ||
			strings.Contains(err.Error(), "usage credit") ||
			(strings.Contains(err.Error(), "resume") && strings.Contains(err.Error(), "--force"))) {
			b.WriteString("  - Session cannot process prompts (credits/quota/rate-limit): do NOT retry recycle\n")
			b.WriteString("  - Relaunch cleanly: flotilla resume ")
			b.WriteString(agent)
			b.WriteString(" --force\n")
			b.WriteString("  - Or restore provider credits/quota, then recycle once the session can run a turn\n")
		} else {
			b.WriteString("  - Check the handoff path still absent; if the desk is wedged: flotilla resume ")
			b.WriteString(agent)
			b.WriteString(" --force\n")
			b.WriteString("  - If the desk is merely slow: wait for Idle, then: flotilla recycle ")
			b.WriteString(agent)
			b.WriteString("\n")
		}
	default:
		b.WriteString("  - Read the error; if desk closed without takeover: flotilla resume ")
		b.WriteString(agent)
		b.WriteString(" (add --force if resume refuses a live pane)\n")
	}
	b.WriteString("This abort MUST reach the coordinator — never log-only (#436).")
	return b.String()
}

type recycleAbortEscalationOps struct {
	submit  func(owner, notice string) error
	enqueue func(rosterDir, sender, owner, notice string) (string, bool, error)
}

type recycleAbortDelivery struct {
	queued   bool
	outboxID string
}

// recycleAbortRoute resolves both sides of the recovery hop. Coordinator self-recycles
// deliberately route through the configured adjutant: the coordinator is the busy/wedged
// recipient in exactly the incident this path must survive, so it cannot also be the sender.
func recycleAbortRoute(cfg *roster.Config, agent string) (sender, owner string, ok bool) {
	if cfg == nil || agent == "" {
		return "", "", false
	}
	owner = cfg.OwningXO(agent, cfg.XOAgent)
	if owner == "" {
		owner = cfg.XOAgent
	}
	if owner == "" {
		owner = cfg.CosAgent
	}
	if owner == "" {
		return "", "", false
	}
	if adj := cfg.AdjutantFor(owner); adj != "" && adj != owner {
		return adj, owner, true
	}
	if owner == agent {
		if cfg.CosAgent != "" && cfg.CosAgent != agent {
			return agent, cfg.CosAgent, true
		}
		return "", "", false
	}
	return agent, owner, true
}

// deliverRecycleAbort first attempts immediate confirmed delivery. Any failure becomes one
// deduplicated durable outbox entry; busy, pane-uncertain, and missing-surface outcomes are
// therefore delayed delivery states rather than dropped escalation attempts (#914).
func deliverRecycleAbort(ops recycleAbortEscalationOps, rosterDir, sender, owner, notice string) (recycleAbortDelivery, error) {
	if err := ops.submit(owner, notice); err == nil {
		return recycleAbortDelivery{}, nil
	} else {
		id, deduped, qerr := ops.enqueue(rosterDir, sender, owner, notice)
		if qerr != nil {
			return recycleAbortDelivery{}, fmt.Errorf("direct delivery failed: %v; durable outbox enqueue also failed: %w", err, qerr)
		}
		log.Printf("flotilla: recycle: abort delivery to %q deferred in %q outbox (id=%s deduped=%t): %v", owner, sender, id, deduped, err)
		return recycleAbortDelivery{queued: true, outboxID: id}, nil
	}
}

func writeRecycleAbortSidecar(home, agent, notice string, delivery recycleAbortDelivery) error {
	dir := filepath.Join(home, ".flotilla", agent)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	status := "delivery=direct"
	if delivery.queued {
		status = "delivery=durable-outbox\noutbox_id=" + delivery.outboxID
	}
	body := fmt.Sprintf("%s\n%s\n---\n%s\n", time.Now().UTC().Format(time.RFC3339), status, notice)
	return os.WriteFile(filepath.Join(dir, "last-recycle-abort.txt"), []byte(body), 0o600)
}

// escalateRecycleAbort surfaces a failed recycle to the owning coordinator's pane,
// falls back to a durable sender outbox, and writes a sidecar under ~/.flotilla/<agent>/
// (#914, successor to #436). Best-effort: never masks the original recycle error.
func escalateRecycleAbort(cfg *roster.Config, rosterPath, agent string, runErr error, handoffPath string) {
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

	sender, owner, ok := recycleAbortRoute(cfg, agent)
	if !ok {
		log.Printf("flotilla: recycle: abort escalate: no distinct sender/coordinator route for %q", agent)
		return
	}
	ops := recycleAbortEscalationOps{
		submit: func(owner, notice string) error {
			drv, found := surface.Get(agentSurface(cfg, owner))
			if !found {
				return fmt.Errorf("no surface for owner %q", owner)
			}
			pane, err := deliver.ResolvePane(agentTitle(cfg, owner))
			if err != nil {
				return fmt.Errorf("resolve owner %q pane: %w", owner, err)
			}
			confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
			return confirm.Submit(drv, pane, notice)
		},
		enqueue: outbox.Enqueue,
	}
	delivery, err := deliverRecycleAbort(ops, filepath.Dir(rosterPath), sender, owner, notice)
	if err != nil {
		log.Printf("flotilla: recycle: abort escalation for %q failed: %v", agent, err)
		return
	}
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		if werr := writeRecycleAbortSidecar(home, agent, notice, delivery); werr != nil {
			log.Printf("flotilla: recycle: abort sidecar write failed: %v", werr)
		}
	}
	if delivery.queued {
		return
	}
	log.Printf("flotilla: recycle: abort escalated directly to coordinator %q for desk %q", owner, agent)
}
