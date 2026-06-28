package transport

import (
	"strconv"
	"time"
)

// Message is the medium-agnostic, relay-relevant projection of a coordination
// message — the fields the catch-up reconciler and the dedup gate need, decoupled
// from any concrete medium's full type. It is the successor to discord.Message at
// the bus seam: SnowID is the message id parsed as a time-ordered uint64 cursor key
// (a Discord snowflake today; any monotonic id a future medium supplies). Its fields
// mirror discord.Message exactly, so the extraction is a faithful, byte-pinned
// re-typing of the catch-up machinery rather than a behavior change.
//
// WebhookID flags the transport's OWN outbound (the audit mirror): the catch-up
// path's accept filter drops a self-post author-agnostically (WebhookID != "")
// BEFORE the operator-author check — the same self-mirror guard the live Subscribe
// adapter applies inline, here applied to history read over REST (which includes the
// transport's own posts). AuthorID is the message's sender (the operator-author
// check keys on it).
type Message struct {
	ID        string
	SnowID    uint64
	AuthorID  string
	WebhookID string
	Content   string
	Timestamp time.Time
}

// CatchUp is an OPTIONAL Transport capability: the at-least-once ingestion backstop
// for a transport whose live Subscribe can DROP messages (the discord gateway
// reconnect/resume gap). It walks the contiguous run of messages above a
// per-destination cursor and is reconciled against a durable cursor, INDEPENDENT of
// the live transport (discord's REST works precisely when the websocket is
// unhealthy — which is when messages are lost). A transport whose delivery cannot
// gap (e.g. loopback web, in-process) need not implement it; callers type-assert it
// (mirroring surface.ResultReader) and skip the backstop cleanly when absent.
type CatchUp interface {
	// MessagesAfter walks messages with id > afterID on dest, contiguous + ascending,
	// page by page; capped=true ⇒ more remain above the returned batch. Mirrors
	// discord.REST.MessagesAfterPaged.
	MessagesAfter(dest Destination, afterID string, pageLimit, maxPages int) (msgs []Message, capped bool, err error)
	// Latest returns dest's single most recent message to tail-init a cursor on first
	// boot (mirrors discord.REST.Latest) — so prior history is not replayed. ok=false
	// ⇒ the destination is empty.
	Latest(dest Destination) (msg Message, ok bool, err error)
}

// ParseSnowflake parses a time-ordered message id (a Discord snowflake) into a
// uint64 cursor key. Empty or non-numeric input yields ok=false (the caller skips
// it — never a panic). It lives in the transport package (not internal/discord) so
// the dedup gate — the catch-up machinery — carries the projection without an
// internal/discord import.
func ParseSnowflake(id string) (uint64, bool) {
	if id == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
