// Package cos implements the chief-of-staff context-integration substrate
// (federation companion #108): a deterministic, append-structured "who-knows-what"
// ledger of operator↔XO exchanges across every per-XO channel (#105 federation).
// It is the productized form of the hand-kept context ledger.
//
// Two layers, deliberately separated (see the change design):
//   - This package is the DETERMINISTIC SUBSTRATE — flotilla appends one structured
//     fact per exchange, with NO large-language-model call. Reliable, auditable, cheap.
//   - The cos_agent (an LLM) reads the ledger on its heartbeat and writes its
//     INTEGRATED view (summaries, the who-knows-what matrix) into its OWN region/file,
//     so flotilla's append never collides with the CoS's curation. This package never
//     touches that region.
//
// The mirror is OBSERVE-ONLY: it records traffic the relay and `notify` already handle
// and grants the CoS no delivery path or relay-auth change.
package cos

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// maxGistRunes bounds the gist so a rendered line stays well under the POSIX
// PIPE_BUF (4096 bytes) atomic-append boundary. Two SEPARATE processes append to the
// same ledger — the `flotilla watch` daemon's mirror hook (operator→XO) and a
// `flotilla notify` process (XO→operator) — so a line that exceeds PIPE_BUF could
// interleave with another appender's write and corrupt both. Clamping the gist (the
// only unbounded field) keeps every line single + atomic: 280 runes ≤ 1120 bytes,
// and even with %q escaping (~2×) plus the fixed prefix the line is < 2.5 KB.
const maxGistRunes = 280

// Entry is one mirrored operator↔XO exchange — the unit appended to the ledger.
type Entry struct {
	// Time is when the exchange was mirrored (the caller passes time.Now(); tests
	// pass a fixed time so the rendered line is deterministic).
	Time time.Time
	// Channel is the Discord channel the exchange occurred on (the federation origin
	// channel for an inbound relay; the XO's own channel for an outbound notify). May
	// be empty (legacy single-channel with no channel_id, or an unresolved channel).
	Channel string
	// From and To are the exchange parties: "operator" and an XO name (in either
	// order — operator→XO inbound, XO→operator outbound).
	From string
	To   string
	// Gist is the message body. It is clamped + flattened to a single line on render
	// (see maxGistRunes); the full body lives in the pane/Discord, the ledger carries
	// the gist for the who-knows-what picture.
	Gist string
}

// Append atomically appends one entry to the ledger at path, creating the file (and
// not the parent dir — the roster dir always exists) if absent. The whole line is
// written in a SINGLE O_APPEND write so concurrent appenders (separate processes)
// never interleave: on a local filesystem an O_APPEND write of ≤ PIPE_BUF bytes is
// atomic w.r.t. other appenders, and Line keeps every rendered line bounded.
//
// It is the caller's responsibility to gate on cos_agent being set (an unset CoS
// means this is never called — the capability is inert). Callers treat a returned
// error as best-effort: the mirror is observe-only and MUST NOT fail the operator's
// delivery/reply path, so they log rather than propagate.
func Append(path string, e Entry) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("cos ledger open %q: %w", path, err)
	}
	if _, err := f.WriteString(Line(e)); err != nil {
		f.Close()
		return fmt.Errorf("cos ledger append %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cos ledger close %q: %w", path, err)
	}
	return nil
}

// Line renders an entry as the durable ledger line (exported so tests + a future
// reader assert one format). Shape:
//
//   - 2026-06-18T14:03:05Z · <channel> · <from> → <to> · "<gist>"
//
// The timestamp is RFC3339 in UTC (stable, sortable, tz-free). The gist is rendered
// with %q so a multi-line or quote-bearing body is escaped onto ONE physical line
// (the atomicity precondition) and is unambiguously delimited. An empty channel
// renders as "-" so the field is never blank (which would shift the column layout).
func Line(e Entry) string {
	channel := e.Channel
	if channel == "" {
		channel = "-"
	}
	return fmt.Sprintf("- %s · %s · %s → %s · %q\n",
		e.Time.UTC().Format(time.RFC3339), channel, e.From, e.To, clampGist(e.Gist))
}

// clampGist flattens leading/trailing whitespace and truncates the gist to
// maxGistRunes (appending an ellipsis marker) so the rendered line stays bounded and
// single-purpose. Truncation is rune-safe (never splits a multi-byte rune).
func clampGist(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= maxGistRunes {
		return s
	}
	return string(r[:maxGistRunes]) + "…"
}
