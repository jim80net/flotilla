package voice

import "strings"

// normalizeTranscript is the SINGLE place Grok STT artifacts are cleaned, so the
// stripping is consistent and testable rather than scattered across the pipeline.
// The observed §2-live-probe artifact is a stray LEADING double-quote plus surrounding
// whitespace (e.g. `"Floatilla Voice...`). It strips exactly that — a single leading
// double-quote and surrounding whitespace — and nothing else: interior quotes, a
// trailing quote, and punctuation are preserved (we only strip what the real API was
// observed to emit, not a guessed matched-pair, which over-strips inputs like
// `"hi" there`).
func normalizeTranscript(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, `"`)
	return strings.TrimSpace(s)
}
