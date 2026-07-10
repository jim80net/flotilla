package main

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/readermap"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/sessionmirror"
	"github.com/jim80net/flotilla/internal/transport"
	"github.com/jim80net/flotilla/internal/watch"
)

// mirrorChunkLimit is the per-chunk rune budget for a mirrored turn-final. It sits below Discord's
// hard MaxContentRunes (2000) to leave headroom for any future per-chunk prefix and to match the
// proven XO-mirror hook's 1900 budget.
const mirrorChunkLimit = 1900

// deskMirror holds the injected collaborators the per-desk visibility mirror needs, so the
// best-effort decision logic (resolve webhook → read turn-final → chunk → post) is unit-testable
// without tmux, a real ~/.claude transcript tree, or a live Discord webhook. The watch daemon wires
// these to the real implementations (secrets.Webhook, the surface ResultReader, the transport Post).
type deskMirror struct {
	// webhook resolves an agent's channel-bound webhook URL; ok=false ⇒ no webhook configured (skip).
	webhook func(agent string) (url string, ok bool)
	// turnFinal returns the desk's substantive turn-final text; ok=false ⇒ nothing substantive to
	// mirror (no session / no completed turn / pure command noise); err ⇒ a read failure.
	turnFinal func(agent string) (text string, ok bool, err error)
	// post sends one chunk under the desk's identity to its webhook.
	post func(url, username, content string) error
	// logf writes exactly one journald decision line per mirror.
	logf func(format string, args ...any)
	// rosterDir is the roster directory root for session-mirror/<agent>.jsonl append.
	// Empty ⇒ session-mirror ledger write is inert (Discord-only deployments).
	rosterDir string
	// now supplies the ledger timestamp (tests pin it); nil ⇒ time.Now.
	now func() time.Time
	// ledgerAppend overrides sessionmirror.Append for tests; nil ⇒ the real append.
	ledgerAppend func(rosterDir, agent string, rec sessionmirror.Record) error
	// ledgerOnly skips Discord posting after the session-mirror ledger append. Retained for
	// tests; production coordinator mirrors post Discord + ledger (harness-agnostic).
	ledgerOnly bool
	// onDiscordSuccess is invoked after all Discord chunks post successfully with the
	// reader-modeled body. Used by CoordinatorMirrorOnFinish to append the CoS ledger.
	onDiscordSuccess func(agent, body string)
}

// run performs the mirror for one finished desk. It is OBSERVE-ONLY and BEST-EFFORT: it never
// returns an error (the detector invokes it for its side-effect only), and every outcome emits
// exactly one decision log line so a silent failure cannot hide — the original XO-mirror bugs
// survived for weeks precisely because failures exited silently:
//
//	WARN <agent>: no webhook — Discord skipped; session-mirror still attempted (#506/#572)
//	SKIP <agent>: <reason>   — nothing substantive, or a read error
//	MIRROR-FAIL <agent>: <detail> — one or more chunk posts failed
//	POST <agent> <n> chunks  — Discord + (when rosterDir set) session-mirror
//	LEDGER <agent>           — session-mirror only (no Discord: ledgerOnly or missing webhook)
//
// #572: missing webhook must NOT suppress the session-mirror ledger — dash conversations
// depend on it. Discord is best-effort on top; the loud WARN remains.
func (m deskMirror) run(agent string) {
	postDiscord := !m.ledgerOnly
	var url string
	haveHook := false
	if postDiscord {
		if m.webhook != nil {
			url, haveHook = m.webhook(agent)
		}
		if !haveHook {
			// LOUD finish-edge gap (#506): missing webhook is a provisioning error — operators
			// hunting "why didn't Discord see this turn?" need a greppable WARN naming the seat.
			// Session-mirror still proceeds when rosterDir is set (#572 dash parity).
			m.logf("flotilla watch: mirror WARN %s: no webhook configured — seat will not appear on Discord (provision %s); attempting session-mirror only",
				agent, roster.WebhookKey(agent))
		}
	}
	// Nothing useful to do without Discord and without a ledger target.
	if !haveHook && m.rosterDir == "" && !m.ledgerOnly {
		return
	}
	if m.turnFinal == nil {
		m.logf("flotilla watch: mirror SKIP %s: no turn-final source", agent)
		return
	}
	text, ok, err := m.turnFinal(agent)
	if err != nil {
		m.logf("flotilla watch: mirror SKIP %s: read turn-final: %v", agent, err)
		return
	}
	if !ok {
		m.logf("flotilla watch: mirror SKIP %s: no substantive turn-final to mirror", agent)
		return
	}

	// The synchronous pre-post reader-modeling pipeline (runs BEFORE the post — a
	// Discord message cannot be un-sent). The partition firewall (Pillar D) does NOT
	// run here: dash + operator-channel mirroring are fleet-internal surfaces (#465).
	// Envelope-validate + tier-1 are WARN-WITH-PUBLISH, so a deficient or un-enveloped
	// turn-final is flagged but never lost. An enveloped brief is RENDERED from its
	// fields (modeled body). Public-repo egress is guarded by the static
	// check-private-boundary.sh + pre-push hook instead.
	d := readerModelInternal(text)

	ledgerOK := m.appendSessionMirror(agent, text, d)

	recentNotify := m.rosterDir != "" &&
		watch.RecentNotifyWithinTTL(roster.LayerLastNotifyPath(m.rosterDir, agent), watch.DefaultRecentNotifySuppressTTL, m.mirrorNow())
	// #595: notify already reached the operator on Discord — skip finish-edge mirror POST.
	if postDiscord && haveHook && recentNotify {
		postDiscord = false
	}

	// Ledger-only modes: explicit ledgerOnly, Discord unavailable, or recent notify (#595).
	if !postDiscord || !haveHook {
		if !ledgerOK {
			return
		}
		body, rmNote := d.body, d.note
		runes := utf8.RuneCountInString(body)
		skipNote := ""
		if recentNotify {
			skipNote = " (Discord skipped: recent notify within 3m)"
		}
		if rmNote != "" {
			m.logf("flotilla watch: mirror LEDGER %s resplen=%d%s %s", agent, runes, skipNote, rmNote)
		} else {
			m.logf("flotilla watch: mirror LEDGER %s resplen=%d%s", agent, runes, skipNote)
		}
		return
	}

	body, rmNote := d.body, d.note
	chunks := transport.Chunk(body, mirrorChunkLimit)
	n := len(chunks)
	runes := utf8.RuneCountInString(body) // resplen: the canary diagnostic for a post-hoc truncation hunt
	for i, chunk := range chunks {
		out := chunk
		if n > 1 {
			out = fmt.Sprintf("(%d/%d)\n%s", i+1, n, chunk)
		}
		if err := m.post(url, agent, out); err != nil {
			// A redaction-safe error (the transport's Post never leaks the webhook URL). Stop on the first
			// failure — the remaining chunks would post out of context anyway.
			m.logf("flotilla watch: mirror MIRROR-FAIL %s: chunk %d/%d: %v", agent, i+1, n, err)
			return
		}
	}
	if rmNote != "" {
		m.logf("flotilla watch: mirror POST %s %d chunks resplen=%d %s", agent, n, runes, rmNote)
	} else {
		m.logf("flotilla watch: mirror POST %s %d chunks resplen=%d", agent, n, runes)
	}
	if m.onDiscordSuccess != nil {
		m.onDiscordSuccess(agent, body)
	}
}

// mirrorDecision is the outcome of the pre-post pipeline for one turn-final: the body
// to publish and the status note for the single decision log line.
type mirrorDecision struct {
	body     string
	note     string
	envelope *readermap.Envelope
}

func (m deskMirror) mirrorNow() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// appendSessionMirror fans out the session-mirror ledger write after readerModelInternal
// (same turn-final read — no second pane probe).
func (m deskMirror) appendSessionMirror(agent, verbose string, d mirrorDecision) bool {
	if m.rosterDir == "" {
		return false
	}
	rec := sessionmirror.NewRecord(sessionmirror.Input{
		Agent:      agent,
		At:         m.mirrorNow(),
		Verbose:    verbose,
		Info:       d.body,
		MirrorNote: d.note,
		Envelope:   d.envelope,
	})
	appendFn := m.ledgerAppend
	if appendFn == nil {
		appendFn = func(rosterDir, agent string, rec sessionmirror.Record) error {
			return sessionmirror.Append(rosterDir, agent, rec, sessionmirror.AppendOptions{})
		}
	}
	if err := appendFn(m.rosterDir, agent, rec); err != nil {
		m.logf("flotilla watch: mirror LEDGER-FAIL %s: %v", agent, err)
		return false
	}
	return true
}

// readerModelInternal applies the INTERNAL-channel reader-modeling pipeline to a
// turn-final before the auto-mirror posts it to dash / operator-channel surfaces.
// The partition firewall (Pillar D) does NOT run here — fleet-internal surfaces may
// legitimately carry deployment names (#465). Public-repo egress is guarded by the
// static check-private-boundary.sh + pre-push hook instead.
//
// Pipeline: envelope detect → validate → tier-1 (warn-with-publish):
//
//	an enveloped brief that passes tier-1 is RENDERED from its fields (modeled body);
//	a tier-1-deficient or malformed envelope is published RAW and FLAGGED (never lost
//	— never lose a brief); an un-enveloped ordinary turn-final is published raw
//	(today's back-compat behavior).
//
// Parsed envelopes flow to the session-mirror ledger via mirrorDecision.envelope
// (sessionmirror.NewRecord); a separate prepend ledger is not required on this path.
//
// NOTE (deliberate, spec'd): on the PASS path the published body is Render(env) — the
// modeled envelope fields ONLY. Prose the desk wrote OUTSIDE the reader-map fence is
// intentionally NOT republished (the spec's "body is rendered from the envelope
// fields"). A reader-map fence thus means "this turn IS a brief; publish the modeled
// envelope" — desks emit the fence only in response to `flotilla brief`, which trains
// them to put the brief's substance INSIDE `delta`, not in surrounding prose. A turn
// with no fence is Absent → published raw, so nothing is ever lost on a non-brief turn.
func readerModelInternal(turnFinal string) mirrorDecision {
	// Envelope detect → validate → tier-1 (warn-with-publish).
	var d mirrorDecision
	env, outcome := readermap.Detect(turnFinal)
	switch outcome {
	case readermap.OutcomePresent:
		envCopy := *env
		if lint := readermap.Tier1Lint(*env); lint.Pass {
			d = mirrorDecision{body: readermap.Render(*env), note: "modeled", envelope: &envCopy}
		} else {
			// Deficient envelope: publish the desk's raw turn-final (preserve what it
			// wrote — never lose) and flag the structural gap for the operator.
			d = mirrorDecision{body: turnFinal, note: "WARN tier1 " + lint.Reason, envelope: &envCopy}
		}
	case readermap.OutcomeMalformed:
		d = mirrorDecision{body: turnFinal, note: "WARN malformed reader-map envelope"}
	default: // OutcomeAbsent — an ordinary, un-enveloped turn-final (back-compat).
		d = mirrorDecision{body: turnFinal}
	}
	return d
}
