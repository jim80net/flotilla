package readermap

// LintResult is the typed outcome of the tier-1 structural lint: PASS, or a
// PRESENCE-FAIL with the reason. Tier-1 is deterministic and model-free — it
// distinguishes a structurally-complete envelope from one missing a field; it
// NEVER judges content (a slop-but-present envelope PASSES tier-1, and is caught
// only by the tier-2 LLM judge). Structure ≠ modeling: that boundary is the whole
// point of the two tiers.
type LintResult struct {
	Pass   bool
	Reason string // empty when Pass; the presence-failure description otherwise
}

// Tier1Lint is the deterministic tier-1 structural lint: it checks ONLY field
// PRESENCE/non-emptiness (delegating to Envelope.Validate, the single source of the
// presence rules). It does NOT check that the body "opens with the anchor and leads
// with the decision" by matching prose — that shape is guaranteed BY CONSTRUCTION by
// Render (which builds the body from the fields in a fixed order), so tier-1 needs
// no fuzzy string match and cannot smuggle in the content judgment the design
// assigns to tier-2. A slop envelope ({anchor:"my work", decision:"none"}) has
// present, non-empty fields and therefore PASSES tier-1 — catching that is the
// tier-2 judge's job, not tier-1's.
func Tier1Lint(e Envelope) LintResult {
	if err := e.Validate(); err != nil {
		return LintResult{Pass: false, Reason: err.Error()}
	}
	return LintResult{Pass: true}
}
