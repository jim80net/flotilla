package transport

import "github.com/jim80net/flotilla/internal/discord"

// Chunk splits text into ordered chunks, each at most limit runes — the
// medium-agnostic chunker the bus's outbound paths use instead of importing
// discord.ChunkContent directly. It is exposed as a package function (taking an
// explicit limit) for callers that need a budget SMALLER than a transport's natural
// cap: the audit mirror and the reply leg chunk at a sub-cap (headroom for a
// per-chunk "(i/N)" prefix), which the no-argument Transport.Chunk (transport cap)
// cannot express. Transport.Chunk delegates here with the transport's own cap.
//
// It wraps discord.ChunkContent (one implementation, no duplicated splitting logic):
// the paragraph-boundary-preferring, rune-measured split is identical regardless of
// medium, so the discord chunker is the shared primitive until a medium needs a
// different splitting rule.
func Chunk(text string, limit int) []string {
	return discord.ChunkContent(text, limit)
}
