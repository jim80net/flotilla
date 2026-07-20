package researchannotation

type AnchorState string

const (
	AnchorAttached    AnchorState = "attached"
	AnchorNeedsReview AnchorState = "needs_review"
)

type Resolution struct {
	State AnchorState `json:"state"`
	Start int         `json:"start"`
	End   int         `json:"end"`
}

// Resolve uses offsets only when quote and context still match. Otherwise it
// searches for a unique quote/context match; zero or multiple matches are kept
// as needs_review rather than silently attaching to the wrong passage.
func Resolve(text string, anchor Anchor) Resolution {
	return resolve(text, anchor, true)
}

// Reanchor deliberately ignores stale offsets after a document digest changes.
// It attaches only when quote and context have exactly one match.
func Reanchor(text string, anchor Anchor) Resolution {
	return resolve(text, anchor, false)
}

func resolve(text string, anchor Anchor, useOffsets bool) Resolution {
	runes := []rune(text)
	quote := []rune(anchor.Quote)
	if useOffsets && matchesAt(runes, quote, anchor, anchor.Start) {
		return Resolution{State: AnchorAttached, Start: anchor.Start, End: anchor.End}
	}
	matches := make([]int, 0, 2)
	for start := 0; start+len(quote) <= len(runes); start++ {
		if matchesAt(runes, quote, anchor, start) {
			matches = append(matches, start)
			if len(matches) > 1 {
				return Resolution{State: AnchorNeedsReview}
			}
		}
	}
	if len(matches) != 1 {
		return Resolution{State: AnchorNeedsReview}
	}
	return Resolution{State: AnchorAttached, Start: matches[0], End: matches[0] + len(quote)}
}

func matchesAt(text, quote []rune, anchor Anchor, start int) bool {
	end := start + len(quote)
	if start < 0 || end > len(text) || string(text[start:end]) != string(quote) {
		return false
	}
	prefix := []rune(anchor.Prefix)
	if len(prefix) > start || string(text[start-len(prefix):start]) != string(prefix) {
		return false
	}
	suffix := []rune(anchor.Suffix)
	if end+len(suffix) > len(text) || string(text[end:end+len(suffix)]) != string(suffix) {
		return false
	}
	return true
}
