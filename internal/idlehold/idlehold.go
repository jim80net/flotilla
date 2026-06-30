// Package idlehold detects the idle-hold antipattern: an agent stalling on a
// non-decision by ending turns with permission-seeking or wait-only language
// instead of acting on already-authorized, reversible work. The detector is PURE
// (pattern matching only — no I/O) so the harness can run it on every turn-final
// and on scheduled wake bodies without coupling to a surface driver.
//
// A genuine operator decision is ONLY one of three kinds: new/not-yet-affirmed
// money spend, irreversible/destructive action, or a genuine divergent fork with
// real tradeoffs. Language that names one of those carve-outs is NOT idle-hold.
// Everything else that ends a turn with "holding / waiting" is the antipattern
// this package flags.
package idlehold

import (
	"regexp"
	"strings"
)

// StrikeThreshold is how many consecutive idle-hold turn-finals (or wait-only
// wakes) on the same agent trigger the forcing-function prompt. "Repeated" in
// the issue maps to two strikes — one might be a slip; two is a pattern.
const StrikeThreshold = 2

// Result is the egress-agnostic verdict for one text body (a turn-final or a
// scheduled wake message).
type Result struct {
	// IdleHold is true when the body matches an idle-hold signal AND does NOT
	// carry a genuine-decision carve-out.
	IdleHold bool
	// Signal names the matched antipattern class for logging (empty when IdleHold
	// is false).
	Signal string
	// Recommendation is a stated "my recommendation is X" the break prompt can
	// default to (empty when none was found).
	Recommendation string
}

var (
	// idleHoldPatterns are case-insensitive antipattern signals from the
	// operator's standing rules (be-proactive, anti-hesitation corollary). Each
	// entry is (signal name, compiled pattern).
	idleHoldPatterns = []struct {
		signal string
		re     *regexp.Regexp
	}{
		// More specific signals first — broader patterns below would shadow them.
		{"only-thing-waiting", regexp.MustCompile(`(?i)\bthe\s+only\s+thing\s+waiting\b`)},
		{"wait-only-wake", regexp.MustCompile(`(?i)\b(?:check\s+back|wake|ping)\s+(?:in|when|once)\b.{0,80}\b(?:wait|holding|your\s+(?:call|response))\b`)},
		{"say-the-word", regexp.MustCompile(`(?i)\bsay\s+the\s+word\b`)},
		{"want-me-or-leave", regexp.MustCompile(`(?i)\bwant\s+me\s+to\b.{0,120}\bor\s+(?:leave|wait|sit|stay)\b`)},
		{"shall-i-or", regexp.MustCompile(`(?i)\bshall\s+i\b.{0,80}\bor\s+(?:leave|wait|not)\b`)},
		{"your-call-nondecision", regexp.MustCompile(`(?i)(?:^|\n)\s*(?:so\s+)?your\s+call[.!?\s]*$`)},
		{"permission-seek-end", regexp.MustCompile(`(?i)(?:^|\n)\s*(?:want|should)\s+(?:me\s+)?to\b.{0,60}[?]\s*$`)},
		{"holding-for-call", regexp.MustCompile(`(?i)\bholding\s+(?:for|on)\b`)},
		{"waiting-for-operator", regexp.MustCompile(`(?i)\bwaiting\s+(?:for|on)\s+(?:your|the\s+operator|you\b|your\s+call)`)},
		{"scheduling-wait", regexp.MustCompile(`(?i)\b(?:i(?:'ll| will)|scheduling)\s+wait\b`)},
	}

	// genuineDecisionPatterns exempt a body that names one of the three real
	// operator decisions — spend, irreversible, or divergent fork.
	genuineDecisionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\[awaiting-auth\]`),
		regexp.MustCompile(`(?i)\b(?:new|not[- ]yet[- ]affirmed|unaffirmed)\s+(?:money\s+)?spend\b`),
		regexp.MustCompile(`(?i)\b(?:metered|billable|paid\s+api|subscription\s+token|budget\s+cap|costs?\s+money)\b`),
		regexp.MustCompile(`(?i)\b(?:irreversible|destructive|cannot\s+undo|hard[- ]to[- ]rollback|force[- ]push|delete\s+production)\b`),
		regexp.MustCompile(`(?i)\b(?:mutually[- ]exclusive|divergent\s+direction|genuine\s+fork|two\s+(?:valid\s+)?approaches|real\s+tradeoffs?)\b`),
		regexp.MustCompile(`(?i)\bdecision[- ]type:\s*(?:spend|irreversible|fork)\b`),
	}

	recommendationRE = regexp.MustCompile(`(?i)\bmy\s+recommendation\s+is[:\s]+(.+?)(?:\.|$|\n)`)
)

// Check classifies one body. It returns IdleHold=true only when an antipattern
// signal matches AND no genuine-decision carve-out is present.
func Check(text string) Result {
	if text == "" {
		return Result{}
	}
	if isGenuineDecision(text) {
		return Result{}
	}
	for _, p := range idleHoldPatterns {
		if p.re.MatchString(text) {
			return Result{
				IdleHold:       true,
				Signal:         p.signal,
				Recommendation: extractRecommendation(text),
			}
		}
	}
	return Result{}
}

// isGenuineDecision reports whether the body names one of the three real
// operator decision types (the carve-outs from the anti-hesitation corollary).
func isGenuineDecision(text string) bool {
	for _, re := range genuineDecisionPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func extractRecommendation(text string) string {
	m := recommendationRE.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// Tracker counts consecutive idle-hold strikes per agent. A non-idle-hold turn
// resets the counter; reaching StrikeThreshold means the forcing function fires.
type Tracker struct {
	strikes map[string]int
}

// NewTracker builds an empty per-agent strike counter.
func NewTracker() *Tracker {
	return &Tracker{strikes: make(map[string]int)}
}

// Record applies one Check result for an agent and reports whether the threshold
// is met (forcing function should fire). A non-idle-hold result resets strikes.
func (t *Tracker) Record(agent string, r Result) (thresholdMet bool) {
	if !r.IdleHold {
		delete(t.strikes, agent)
		return false
	}
	t.strikes[agent]++
	return t.strikes[agent] >= StrikeThreshold
}

// Strikes returns the current strike count for an agent (for tests).
func (t *Tracker) Strikes(agent string) int {
	return t.strikes[agent]
}

// BreakPrompt is the forcing-function message injected when the strike threshold
// is met. recommendation may be empty; when set it is quoted as the default action.
const breakPromptPrefix = "[flotilla idle-hold break] Your recent turn(s) ended by holding or " +
	"waiting on a choice that is NOT a genuine operator decision. The three real decisions are: " +
	"new/not-yet-affirmed money spend, irreversible/destructive action, or a genuine divergent " +
	"fork with real tradeoffs. Do NOT end another turn with bare \"holding\" or schedule a " +
	"wait-only wake.\n\nEither:\n" +
	"(A) Make the call yourself NOW on already-authorized, reversible work"

// BreakPrompt composes the idle-hold break message for injection into the agent's pane.
func BreakPrompt(recommendation string) string {
	var b strings.Builder
	b.WriteString(breakPromptPrefix)
	if recommendation != "" {
		b.WriteString(" — default to your stated recommendation: ")
		b.WriteString(recommendation)
	}
	b.WriteString(".\n")
	b.WriteString("(B) Escalate a CONCRETE blocker naming which decision-type applies " +
		"(spend / irreversible / fork) with specifics — never a bare \"waiting.\"\n\n" +
		"Then execute; do not ask permission for work the goal already requires.")
	return b.String()
}
