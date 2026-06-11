package voice

import "strings"

// normalizeTranscript is the SINGLE place Grok STT artifacts are cleaned, so the
// stripping is consistent and testable rather than scattered across the pipeline.
// Observed artifacts on the §2 live probe: a stray leading double-quote and
// surrounding whitespace (e.g. `"Floatilla Voice...`). It strips at most ONE leading
// and one matching trailing double-quote (a transcript is never legitimately wrapped
// in quotes by the speaker) and trims whitespace; it does NOT touch interior quotes or
// punctuation.
func normalizeTranscript(s string) string {
	s = strings.TrimSpace(s)
	// Strip a matched surrounding quote pair, else a stray leading quote alone.
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		s = s[1 : len(s)-1]
	} else {
		s = strings.TrimPrefix(s, `"`)
	}
	return strings.TrimSpace(s)
}
