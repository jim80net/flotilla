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
	"unicode/utf8"
)

// maxLineBytes is the POSIX PIPE_BUF atomic-append boundary (Linux: 4096). Two SEPARATE
// processes append to the same ledger — the `flotilla watch` daemon's mirror hook
// (operator→XO) and a `flotilla notify` process (XO→operator) — so a line that exceeds
// PIPE_BUF could interleave with another appender's write and corrupt both. Line
// GUARANTEES every rendered line is ≤ this many bytes (clamping the gist, and clipping
// as an unconditional backstop), so a single O_APPEND write is always atomic w.r.t.
// other appenders on a local filesystem — independent of any field's length.
const maxLineBytes = 4096

// maxGistRunes is the human-readable default clamp on the gist (the message body — the
// field carrying arbitrary operator/XO content). It keeps the COMMON line far under
// maxLineBytes: 280 runes ≤ 1120 raw bytes, and even at Go's worst-case %q expansion
// (~10 bytes/rune for unprintable supplementary codepoints) ≈ 2.8 KB of gist plus a
// realistic prefix is ≈ 2.9 KB — well under the 4096 PIPE_BUF bound. maxLineBytes is the
// hard backstop for the uncommon case where the (type-unbounded) channel/from/to fields
// would otherwise push the line over PIPE_BUF.
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
	// #407: the audit line (above) clamps the gist to keep the append atomic; when that
	// clamps a real message, persist the FULL body to the loopback-only companion store so
	// the dash can render the operator's complete words. Best-effort — the audit record has
	// already landed durably, so a companion-write failure must NOT turn a successful append
	// into an error (the dash simply falls back to the clamped gist for that entry).
	if WillClamp(e.Gist) {
		if err := WriteBody(path, e); err != nil {
			fmt.Fprintf(os.Stderr, "flotilla: cos ledger companion body write failed (dash will show the clamped gist): %v\n", err)
		}
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
//
// Line GUARANTEES len(result) ≤ maxLineBytes: the gist is rune-clamped, and if the
// type-unbounded channel/from/to fields still push the line past PIPE_BUF the line is
// clipped (rune-safe) as an unconditional backstop — so the single O_APPEND write the
// caller issues is always atomic w.r.t. a concurrent appender. It also GUARANTEES the
// result is a single physical line: the gist is escaped via %q, and channel/from/to are
// rendered with %s so an embedded CR/LF in any of them (a Discord-sourced channel id, a
// roster agent name) would otherwise inject a second physical line and forge a ledger
// entry — they are flattened first.
func Line(e Entry) string {
	channel := e.Channel
	if channel == "" {
		channel = "-"
	}
	line := fmt.Sprintf("- %s · %s · %s → %s · %q\n",
		e.Time.UTC().Format(time.RFC3339), flattenField(channel), flattenField(e.From), flattenField(e.To), clampGist(e.Gist))
	if len(line) > maxLineBytes {
		// Backstop: the gist is already rune-clamped, but channel/from/to are unbounded
		// by type. If a pathological field pushes the rendered line past PIPE_BUF, clip
		// it so the O_APPEND write stays atomic. Unreachable for roster/Discord-bounded
		// fields (agent names, Discord snowflake channel ids); a clipped line is strictly
		// safer than a torn cross-appender interleave that would corrupt two lines.
		line = clipToBytes(line, maxLineBytes)
	}
	return line
}

// clipToBytes returns line truncated to at most maxBytes bytes on a UTF-8 rune boundary,
// always ending in exactly one '\n'. It is Line's unconditional PIPE_BUF backstop.
func clipToBytes(line string, maxBytes int) string {
	body := strings.TrimRight(line, "\n")
	limit := maxBytes - 1 // reserve one byte for the trailing newline
	if len(body) <= limit {
		return body + "\n"
	}
	out := make([]byte, 0, limit)
	for _, r := range body {
		if len(out)+utf8.RuneLen(r) > limit {
			break
		}
		out = utf8.AppendRune(out, r)
	}
	return string(out) + "\n"
}

// flattenField escapes CR/LF in a field rendered inline with %s (channel/from/to), so
// a newline can never inject a second physical line — preserving the one-line-per-entry
// invariant (and with it the atomic-append reasoning) regardless of the field's source.
// The common case (no CR/LF) returns the input unchanged. The gist needs no equivalent:
// it is rendered with %q, which already escapes newlines.
func flattenField(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(s)
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
	return string(r[:maxGistRunes]) + clampMarker
}
