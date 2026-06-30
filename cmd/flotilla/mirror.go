package main

import (
	"fmt"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/readermap"
	"github.com/jim80net/flotilla/internal/transport"
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
}

// run performs the mirror for one finished desk. It is OBSERVE-ONLY and BEST-EFFORT: it never
// returns an error (the detector invokes it for its side-effect only), and every outcome emits
// exactly one decision log line so a silent failure cannot hide — the original XO-mirror bugs
// survived for weeks precisely because failures exited silently:
//
//	SKIP <agent>: <reason>        — no webhook, nothing substantive, or a read error
//	MIRROR-FAIL <agent>: <detail> — one or more chunk posts failed
//	POST <agent> <n> chunks       — the turn-final was mirrored
func (m deskMirror) run(agent string) {
	url, ok := m.webhook(agent)
	if !ok {
		m.logf("flotilla watch: mirror SKIP %s: no webhook configured", agent)
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
	// Discord message cannot be un-sent). On this auto-mirror (an INTERNAL channel,
	// no public egress) the ONLY step that suppresses a post is the firewall refuse
	// (a private leak — P2; never suppresses in P0); envelope-validate + tier-1 are
	// WARN-WITH-PUBLISH here, so a deficient or un-enveloped turn-final is flagged
	// but never lost. An enveloped brief is RENDERED from its fields (modeled body).
	body, rmNote, suppress := readerModelInternal(text)
	if suppress {
		m.logf("flotilla watch: mirror SUPPRESS %s: %s", agent, rmNote)
		return
	}

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
}

// readerModelInternal applies the INTERNAL-channel (warn-with-publish) reader-modeling
// policy to a turn-final before the auto-mirror posts it, returning the body to post,
// a status note for the single decision log line, and whether to SUPPRESS the post.
//
// The auto-mirror is an internal channel with no public egress, so the only step that
// suppresses is the firewall refuse (a private leak) — that arm lands in P2 and is a
// clean prepend here (set suppress=true on a firewall hit); in P0 nothing suppresses.
// Envelope-validate + tier-1 are warn-with-publish: an enveloped brief that passes
// tier-1 is RENDERED from its fields (the modeled body); a tier-1-deficient or a
// malformed envelope is published RAW and FLAGGED (never lost — never lose a brief);
// an un-enveloped ordinary turn-final is published raw (today's back-compat behavior).
func readerModelInternal(turnFinal string) (body, note string, suppress bool) {
	// (P2) firewall refuse-check would go here as stage 1 — on a leak, return
	// ("", "<token> leak", true). Not in P0.
	env, outcome := readermap.Detect(turnFinal)
	switch outcome {
	case readermap.OutcomePresent:
		if lint := readermap.Tier1Lint(*env); lint.Pass {
			return readermap.Render(*env), "modeled", false
		} else {
			// Deficient envelope: publish the desk's raw turn-final (preserve what it
			// wrote — never lose) and flag the structural gap for the operator.
			return turnFinal, "WARN tier1 " + lint.Reason, false
		}
	case readermap.OutcomeMalformed:
		return turnFinal, "WARN malformed reader-map envelope", false
	default: // OutcomeAbsent — an ordinary, un-enveloped turn-final (back-compat).
		return turnFinal, "", false
	}
}
