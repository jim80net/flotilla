package discord

import "strings"

// ChunkContent splits text into ordered chunks, each at most limit RUNES, for posting a body that
// exceeds Discord's per-message content limit (MaxContentRunes). Post alone CLAMPS an over-limit
// body (truncating it with an ellipsis); a caller that must deliver the WHOLE body — the per-desk
// turn-final mirror, the generalized XO mirror, a future `notify --chunk` — chunks first and posts
// each chunk in order.
//
// It splits on paragraph boundaries ("\n\n") so a chunk break lands between paragraphs rather than
// mid-sentence wherever possible: paragraphs are accumulated into a chunk until the next one would
// exceed the limit. A single paragraph LONGER than the limit (no boundary to split on) is
// hard-split on rune boundaries so no chunk ever exceeds the limit. Limits are measured in runes,
// not bytes, for parity with MaxContentRunes / clampContent (a multi-byte rune is never split).
//
// Ported from the working XO mirror hook's chunk() (BUG-4 fix), with the byte-length checks replaced
// by rune-length checks. An empty input yields a single empty chunk so a caller always has something
// to post (matching the hook's `parts or [text[:lim]]`).
func ChunkContent(text string, limit int) []string {
	if limit < 1 {
		limit = MaxContentRunes
	}
	paras := splitParagraphs(text)

	var chunks []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			chunks = append(chunks, string(cur))
			cur = nil
		}
	}
	for _, para := range paras {
		p := []rune(para)
		// Candidate length if this paragraph is appended to the current chunk (with the "\n\n"
		// separator when the chunk is non-empty).
		sep := 0
		if len(cur) > 0 {
			sep = 2
		}
		switch {
		case len(cur)+sep+len(p) <= limit:
			if sep > 0 {
				cur = append(cur, '\n', '\n')
			}
			cur = append(cur, p...)
		case len(p) <= limit:
			// Fits on its own but not appended — start a fresh chunk with it.
			flush()
			cur = append(cur, p...)
		default:
			// A single over-limit paragraph: emit the pending chunk, then hard-split this one.
			flush()
			for start := 0; start < len(p); start += limit {
				end := start + limit
				if end > len(p) {
					end = len(p)
				}
				chunks = append(chunks, string(p[start:end]))
			}
		}
	}
	flush()
	if len(chunks) == 0 {
		return []string{""}
	}
	return chunks
}

// splitParagraphs splits on the "\n\n" paragraph boundary, preserving order. It is a plain split (no
// trimming) so the reassembled chunks stay faithful to the source spacing within the limit.
func splitParagraphs(text string) []string {
	return strings.Split(text, "\n\n")
}
