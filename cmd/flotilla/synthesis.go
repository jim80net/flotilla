package main

import (
	"fmt"
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

// synthDigestTicks is the visibility-synthesis digest sub-cadence in detector TICKS (Q-B): a
// synthesizing agent fires WakeSynthesis at most once per this many ticks while it has work owed,
// so a burst of subordinate finishes coalesces into ONE curated rollup. A tick IS the
// heartbeat_interval, so this is "a small multiple of heartbeat_interval" expressed in ticks — the
// daemon bounds the RATE; the skill bounds the CONTENT (it may reply idle). Kept a small constant
// (not a roster knob) per the resolved design; revisit if a deployment needs it tunable.
const synthDigestTicks = 3

// Visibility synthesis (B2) §3 + §7 — the transcript-first READ of a subordinate's latest state
// (the SAME surface.ResultReader seam Tier 1 uses) and the thin WakeSynthesis prompt composer.
//
// The read is READ-ONLY reuse of the shipped Tier-1 mirror's reader: resolve the subordinate's pane
// (ResolvePane(agentTitle)) → its driver's surface.ResultReader → LatestResult(pane). It binds to
// the SEAM, NOT to claudestore directly (which would exclude a grok subordinate). A subordinate
// whose surface has no ResultReader, or whose pane will not resolve, is a CLEAN SKIP (ok=false) —
// exactly as the Tier-1 mirror skips an unreadable desk. No ledger, no write-path, no relay.

// synthTurnFinal returns a subordinate's latest turn-final text via the shared surface.ResultReader
// seam, mirroring deskMirrorOnFinish.turnFinal: a surface without a ResultReader or an unresolvable
// pane is a clean skip (ok=false), and a read error is carried through so the caller can log the
// reason while still treating it as a skip.
func synthTurnFinal(cfg *roster.Config) func(agent string) (string, bool, error) {
	return func(agent string) (string, bool, error) {
		drv, ok := surface.Get(agentSurface(cfg, agent))
		if !ok {
			return "", false, fmt.Errorf("unknown surface for agent %q", agent)
		}
		rr, ok := drv.(surface.ResultReader)
		if !ok {
			// No session-store reader for this surface — nothing to read (clean skip).
			return "", false, nil
		}
		pane, err := deliver.ResolvePane(agentTitle(cfg, agent))
		if err != nil {
			return "", false, err
		}
		text, err := rr.LatestResult(pane)
		if err != nil {
			return "", false, err
		}
		return text, true, nil
	}
}

// synthReadOneFromTurnFinal adapts a turn-final reader (text, ok, err) into the detector's SynthRead
// seam (text, ok): an UNREADABLE subordinate (no ResultReader / unresolvable pane / read error) is a
// clean skip (ok=false) so the materiality gate EXCLUDES it (never hashes it as empty — re-trio
// P2-4). The error is intentionally collapsed to ok=false here; the production caller logs nothing
// per-read (the materiality path is silent-best-effort), matching the Tier-1 mirror's skip handling.
func synthReadOneFromTurnFinal(turnFinal func(string) (string, bool, error)) func(string) (string, bool) {
	return func(agent string) (string, bool) {
		text, ok, err := turnFinal(agent)
		if err != nil || !ok {
			return "", false
		}
		return text, true
	}
}

// synthesisWakeBody composes the WakeSynthesis prompt — the SELF-SUFFICIENT trigger that points the
// agent at its read set (the agents below it), the CONCRETE read command for each, its post target
// (the channel it owns), the per-tier output contract, and the narrow-answer discipline. It is
// self-sufficient BY DESIGN: the skill (visibility-synthesis.md) defers the read mechanism to "the
// daemon's wake prompt" and a DIRECTLY-LAUNCHED agent (no `flotilla workspace init`, no
// `--append-system-prompt-file`) has no skill file to load — so the wake prompt itself names the
// `flotilla result` command, making synthesis work harness-launch-agnostically (the workspace skill
// is an enrichment, not a hard dependency). rosterPath is the path the DAEMON actually loaded (passed
// absolute), so the command resolves the live roster regardless of the agent's own cwd. A synthesis
// wake targets a synthesizing sub-coordinator, NOT the daemon's liveness-tracked clock XO — it MUST
// NOT carry a liveness-ack instruction (#190): an idle sub-XO touching the clock XO's ack file would
// mask a genuinely-dead coordinator from the AckAge wedge.
func synthesisWakeBody(agent, binPath, rosterPath string, readSet, postChannels []string) string {
	var b strings.Builder
	b.WriteString("[flotilla visibility-synthesis] You are OWED a curated rollup of the tier BELOW you. ")
	b.WriteString("Run your `visibility-synthesis` skill (or, if you have none, follow the contract below).\n")

	if len(readSet) > 0 {
		// Name the CONCRETE read command, not just the agent names — `result` is read-only (the same
		// surface.ResultReader seam Tier 1 uses) and needs no workspace, so a directly-launched agent can
		// service the synthesis. Use the daemon's OWN absolute binary path (binPath) — NOT bare `flotilla`
		// — because a directly-launched agent may not have flotilla on its $PATH (the live fleet invokes
		// ~/go/bin/flotilla by absolute path). --roster carries the daemon's live absolute path so it
		// resolves from the agent's own cwd. Tier-3 reads project-XOs (themselves synthesizers) the same
		// way — the command returns each subordinate's latest turn-final regardless of its tier.
		b.WriteString("READ — for EACH agent below you, run `")
		b.WriteString(binPath)
		b.WriteString(" result --roster ")
		b.WriteString(rosterPath)
		b.WriteString(" <name>` to get its LATEST turn-final state. Your subordinates: ")
		b.WriteString(strings.Join(readSet, ", "))
		b.WriteString(".\n")
	} else {
		b.WriteString("READ: (no subordinates resolve right now — reply idle)\n")
	}

	if len(postChannels) > 0 {
		b.WriteString("POST your synthesis into the channel you own: ")
		b.WriteString(strings.Join(postChannels, ", "))
		b.WriteString(" (via its webhook).\n")
	} else {
		b.WriteString("POST: (no owned channel resolved — surface this, do not drop the synthesis)\n")
	}

	b.WriteString("CONTRACT: Tier 2 (an XO) = a curated DOMAIN rollup grouped by subordinate — the material " +
		"state only, surfacing anything that needs the operator's eye. Tier 3 (the meta-XO, into the " +
		"fleet-command channel) = a one-paragraph fleet HEADLINE + the open OPERATOR-DECISIONS (best-effort " +
		"over each subordinate's latest turn) + DRILL-DOWN pointers down the channel graph.\n")
	b.WriteString("DISCIPLINE: curate only what CHANGED since your last synthesis. If nothing material " +
		"changed, reply 'idle' — never manufacture a synthesis.\n")
	// Mirror the skill's skip-on-unreadable discipline INTO the prompt — a directly-launched agent has
	// no skill file, and during a rate-limit storm a subordinate's `flotilla result` may error or
	// return its last errored turn. Without this, the agent might report an unreadable/errored
	// subordinate as "changed" or "went silent", or fail the whole rollup for one.
	b.WriteString("SKIP an unreadable subordinate: if `flotilla result <name>` errors or returns an " +
		"error/rate-limited turn, treat that subordinate as UNKNOWN — synthesize over the ones you CAN " +
		"read, never fail the whole rollup for one, and never report a skipped one as 'changed' or 'went silent'.")
	return b.String()
}

// synthesisRead resolves a synthesizing agent's READ SET (the agents below it — AgentsBelow) and
// reads each subordinate's latest turn-final state through the shared ResultReader seam. It is used
// BOTH as the detector's SynthRead (per-subordinate materiality reads, via synthReadOneFromTurnFinal)
// and to name the read set in the wake prompt. Read-only reuse of the Tier-1 reader.
func synthesisReadSet(cfg *roster.Config, agent string) []string {
	return cfg.AgentsBelow(agent)
}

// synthParentsResolver wires the detector's SynthParents to roster.AgentsAbove — the owed-marking
// resolver (a finishing desk marks each parent above it owed). Pure read-only derivation.
func synthParentsResolver(cfg *roster.Config) func(agent string) []string {
	return func(agent string) []string { return cfg.AgentsAbove(agent) }
}
