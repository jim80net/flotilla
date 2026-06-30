package readermap

import (
	"fmt"
	"strings"
)

// Render builds the published body FROM the envelope fields in the fixed order
// anchor → decision → delta. This is what makes "open from the reader's map, lead
// with the decision" a STRUCTURAL guarantee rather than a property a fuzzy lint
// must verify: the body always opens with the Anchor (the reader's map entry) and
// the Decision always precedes the Delta. The desk's authored prose lives in Delta;
// the render frames it so the reader's map updates cleanly regardless of how the
// desk wrote the body.
//
// Render assumes a validated envelope (call Tier1Lint/Validate first); it does not
// re-validate, so a caller that renders an invalid envelope gets a body with empty
// sections — the publish path runs the lint before the render.
func Render(e Envelope) string {
	decision := strings.TrimSpace(e.Decision)
	var b strings.Builder
	// Lead with the anchor — the reader's map entry, in their terms.
	b.WriteString(strings.TrimSpace(e.Anchor))
	b.WriteString("\n\n")
	// Then the one decision (or the explicit "none"), so the reader sees the action
	// before the supporting detail.
	if e.DecisionIsNone() {
		b.WriteString("Decision: none — no action needed.")
	} else {
		b.WriteString(fmt.Sprintf("Decision: %s", decision))
	}
	b.WriteString("\n\n")
	// Then the delta — what changed (the desk's authored body).
	b.WriteString(strings.TrimSpace(e.Delta))
	return b.String()
}
