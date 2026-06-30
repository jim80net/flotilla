// Package readermap holds the pure, I/O-free primitives of flotilla's mechanical
// reader-modeling: the reader-map delta Envelope every published artifact carries,
// the three-way Detect predicate that locates an envelope inside a free-text
// turn-final, the deterministic tier-1 structural lint (field PRESENCE only), and
// the Render that builds a published body FROM the envelope fields so the
// open-from-the-anchor / lead-with-the-decision shape holds by construction.
//
// The package is deliberately free of tmux, Discord, secrets, and surface imports
// so the modeling rules are unit-testable in isolation; the publish path
// (cmd/flotilla) wires these primitives onto the runtime mirror + CLI egresses.
// What this package does NOT supply is the modeling JUDGMENT — choosing the true
// anchor and the one decision is the LLM judge's job (tier-2, the willing-to-wait
// CLI path); structure forces the shape, the judge supplies the quality.
package readermap

import (
	"fmt"
	"strings"
)

// Audience is the reader an artifact is modeled for. It is OPEN-stringly-typed: the
// named constants below are conveniences, but Validate accepts ANY non-empty value
// (a desk audience is "desk:<name>"), so the audience set extends without a schema
// change — the spec's "open-stringly-typed; extension path documented".
type Audience string

const (
	AudienceOperator   Audience = "operator"
	AudienceNewcomer   Audience = "newcomer"
	AudienceMaintainer Audience = "maintainer"
	// A desk audience is the string "desk:<name>" — open-stringly-typed, no const.
)

// DecisionNone is the explicit sentinel a Decision carries when the reader has NO
// action to take. Decision MUST be either a real action or this exact string — an
// EMPTY Decision is invalid, because "the reader need do nothing" must be stated,
// not omitted (an omitted decision reads as a forgotten one, corrupting the map).
const DecisionNone = "none"

// Envelope is the reader-map delta carried by every published artifact: WHO it is
// for (Audience), WHICH map entry it updates in the reader's terms (Anchor), WHAT
// changed (Delta), and the ONE action the reader must take (Decision, or "none").
// It is the I/O contract between the desk's authoring LLM (which fills it) and the
// publish path (which validates the shape and, on the CLI path, judges the quality)
// — and the uniform data the dash map view renders.
type Envelope struct {
	Audience Audience `json:"audience"`
	Anchor   string   `json:"anchor"`
	Delta    string   `json:"delta"`
	Decision string   `json:"decision"`
}

// Validate enforces field PRESENCE — the deterministic, model-free structural rule.
// It checks that every field is non-empty and that Decision is present (a real
// action or the explicit DecisionNone). It does NOT and CANNOT judge whether the
// Anchor is the reader's REAL map entry or the Decision is THE decision — that
// content judgment is the tier-2 LLM judge's job. Audience is open-stringly-typed,
// so Validate only requires it non-empty (any value is a valid extension audience).
func (e Envelope) Validate() error {
	if strings.TrimSpace(string(e.Audience)) == "" {
		return fmt.Errorf("readermap: audience is empty")
	}
	if strings.TrimSpace(e.Anchor) == "" {
		return fmt.Errorf("readermap: anchor is empty")
	}
	if strings.TrimSpace(e.Delta) == "" {
		return fmt.Errorf("readermap: delta is empty")
	}
	if strings.TrimSpace(e.Decision) == "" {
		return fmt.Errorf("readermap: decision is empty (use %q when no action is needed)", DecisionNone)
	}
	return nil
}

// DecisionIsNone reports whether the envelope's Decision is the explicit no-action
// sentinel (case-insensitive, trimmed) — the dash uses it to distinguish a desk
// with a pending action from one with nothing for the reader to do.
func (e Envelope) DecisionIsNone() bool {
	return strings.EqualFold(strings.TrimSpace(e.Decision), DecisionNone)
}
