package main

import (
	"fmt"
	"unicode/utf8"

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

	chunks := transport.Chunk(text, mirrorChunkLimit)
	n := len(chunks)
	runes := utf8.RuneCountInString(text) // resplen: the canary diagnostic for a post-hoc truncation hunt
	for i, chunk := range chunks {
		body := chunk
		if n > 1 {
			body = fmt.Sprintf("(%d/%d)\n%s", i+1, n, chunk)
		}
		if err := m.post(url, agent, body); err != nil {
			// A redaction-safe error (the transport's Post never leaks the webhook URL). Stop on the first
			// failure — the remaining chunks would post out of context anyway.
			m.logf("flotilla watch: mirror MIRROR-FAIL %s: chunk %d/%d: %v", agent, i+1, n, err)
			return
		}
	}
	m.logf("flotilla watch: mirror POST %s %d chunks resplen=%d", agent, n, runes)
}
