// Package idlehold detects the idle-hold antipattern: an agent stalling on a
// non-decision by ending turns with permission-seeking or wait-only language
// instead of acting on already-authorized, reversible work. The detector is PURE
// (pattern matching only — no I/O) so the harness can run it on every turn-final
// and on scheduled wake bodies without coupling to a surface driver.
//
// A genuine operator decision is ONLY one of three kinds: new/not-yet-affirmed
// money spend, irreversible/destructive action, or a genuine divergent fork with
// real tradeoffs. Language that names one of those carve-outs, or records a
// tracked blocker in the open-questions ledger ([blocked]), is NOT idle-hold.
// Everything else that ends a turn with "holding / waiting" is the antipattern
// this package flags.
package idlehold

import (
	"regexp"
	"strings"
	"sync"
)

// StrikeThreshold is how many idle-hold turn-finals (or wait-only wakes) on the
// same agent trigger the forcing-function prompt. "Repeated" in the issue maps to
// two strikes — one might be a slip; two is a pattern.
const StrikeThreshold = 2

// Result is the egress-agnostic verdict for one text body (a turn-final or a
// scheduled wake message).
type Result struct {
	// IdleHold is true when the body matches an antipattern signal AND does NOT
	// carry a genuine-decision or tracked-blocker carve-out.
	IdleHold bool
	// Signal names the matched antipattern class for logging (empty when IdleHold
	// is false).
	Signal string
	// Recommendation is a stated "my recommendation is X" the break prompt can
	// default to (empty when none was found).
	Recommendation string
}

type idleHoldPattern struct {
	signal     string
	re         *regexp.Regexp
	tenseGuard bool // reject past-tense narration ("was holding for …")
	quoteGuard bool // reject quoted mentions of the rule
}

var (
	idleHoldPatterns = []idleHoldPattern{
		// More specific signals first — broader patterns below would shadow them.
		{signal: "only-thing-waiting", re: regexp.MustCompile(`(?i)\bthe\s+only\s+thing\s+waiting\b`)},
		{signal: "wait-only-wake", re: regexp.MustCompile(`(?i)\b(?:check\s+back|wake|ping)\s+(?:in|when|once)\b.{0,80}\b(?:wait|holding|your\s+(?:call|response))\b`)},
		{signal: "say-the-word", re: regexp.MustCompile(`(?i)\bsay\s+the\s+word\b`)},
		{signal: "want-me-or-leave", re: regexp.MustCompile(`(?i)\bwant\s+me\s+to\b.{0,120}\bor\s+(?:leave|wait|sit|stay)\b`)},
		{signal: "shall-i-or", re: regexp.MustCompile(`(?i)\bshall\s+i\b.{0,80}\bor\s+(?:leave|wait|not)\b`)},
		{signal: "should-i-proceed", re: regexp.MustCompile(`(?i)\bshould\s+i\s+proceed\b`)},
		{signal: "your-call-nondecision", re: regexp.MustCompile(`(?i)(?:^|\n)\s*(?:so\s+)?your\s+call[.!?\s]*$`)},
		{signal: "permission-seek-end", re: regexp.MustCompile(`(?i)(?:^|\n)\s*(?:want|should)\s+(?:me\s+)?to\b.{0,60}[?]\s*$`)},
		{signal: "standing-by", re: regexp.MustCompile(`(?i)\bstanding\s+by\b`)},
		{signal: "awaiting-go-ahead", re: regexp.MustCompile(`(?i)\bawaiting\s+your\s+go[- ]?ahead\b`)},
		{signal: "let-me-know-proceed", re: regexp.MustCompile(`(?i)\blet\s+me\s+know\s+how\s+you(?:'d| would)\s+like\s+to\s+proceed\b`)},
		{signal: "ready-when-you-are", re: regexp.MustCompile(`(?i)\bready\s+when\s+you\s+are\b`)},
		{signal: "pending-your-input", re: regexp.MustCompile(`(?i)\bpending\s+your\s+input\b`)},
		{signal: "holding-pattern", re: regexp.MustCompile(`(?i)\bholding\s+pattern\b`)},
		{signal: "holding-for-call", re: regexp.MustCompile(`(?i)\bholding\s+(?:for|on)\b`), tenseGuard: true, quoteGuard: true},
		{signal: "waiting-for-operator", re: regexp.MustCompile(`(?i)\bwaiting\s+(?:for|on)\s+(?:your|the\s+operator|you\b|your\s+call)`), tenseGuard: true, quoteGuard: true},
		{signal: "scheduling-wait", re: regexp.MustCompile(`(?i)\b(?:i(?:'ll| will)|scheduling)\s+wait\b`)},
	}

	// genuineDecisionPatterns exempt a body that names one of the three real
	// operator decisions, or records a tracked blocker the doctrine instructs
	// agents to use instead of bare waiting.
	genuineDecisionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\[awaiting-auth\]`),
		regexp.MustCompile(`(?i)\[blocked\]`),
		regexp.MustCompile(`(?i)\[needs-attention\]`),
		regexp.MustCompile(`(?i)\b(?:new|not[- ]yet[- ]affirmed|unaffirmed)\s+(?:money\s+)?spend\b`),
		regexp.MustCompile(`(?i)\b(?:metered|billable|paid\s+api|subscription\s+token|budget\s+cap|costs?\s+money)\b`),
		regexp.MustCompile(`(?i)\b(?:irreversible|destructive|cannot\s+undo|hard[- ]to[- ]rollback|force[- ]push|delete\s+production)\b`),
		regexp.MustCompile(`(?i)\b(?:mutually[- ]exclusive|divergent\s+direction|genuine\s+fork|two\s+(?:valid\s+)?approaches|real\s+tradeoffs?)\b`),
		regexp.MustCompile(`(?i)\bdecision[- ]type:\s*(?:spend|irreversible|fork)\b`),
	}

	pastTenseBeforeRE = regexp.MustCompile(`(?i)\b(?:i\s+)?(?:was|were|had\s+been|have\s+been)\s+\w*\s*$`)

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
		if findIdleHoldMatch(text, p) != "" {
			return Result{
				IdleHold:       true,
				Signal:         p.signal,
				Recommendation: extractRecommendation(text),
			}
		}
	}
	return Result{}
}

func findIdleHoldMatch(text string, p idleHoldPattern) string {
	loc := p.re.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	if p.tenseGuard && isPastTenseContext(text[:loc[0]]) {
		return ""
	}
	if p.quoteGuard && isQuotedMention(text, loc[0], loc[1]) {
		return ""
	}
	return text[loc[0]:loc[1]]
}

func isPastTenseContext(before string) bool {
	tail := before
	if len(tail) > 80 {
		tail = tail[len(tail)-80:]
	}
	return pastTenseBeforeRE.MatchString(tail)
}

func isQuotedMention(text string, start, end int) bool {
	if start == 0 {
		return false
	}
	open := text[start-1]
	if open != '"' && open != '\'' && open != '`' {
		return false
	}
	// Same-line closing quote after the match.
	lineEnd := strings.Index(text[start:], "\n")
	if lineEnd < 0 {
		lineEnd = len(text) - start
	}
	segment := text[start : start+lineEnd]
	return strings.Contains(segment, string(open))
}

// isGenuineDecision reports whether the body names one of the three real
// operator decision types or a doctrine-prescribed tracked blocker.
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

// Tracker accrues idle-hold strikes per agent. Non-matches do NOT reset the
// counter (a missed detection between two real holds must not zero strikes).
// Strikes reset only after the threshold fires. The map is bounded by fleet size
// in practice; retired agent keys linger harmlessly (one int each).
type Tracker struct {
	mu      sync.Mutex
	strikes map[string]int
}

// NewTracker builds an empty per-agent strike counter.
func NewTracker() *Tracker {
	return &Tracker{strikes: make(map[string]int)}
}

// Record applies one Check result for an agent and reports whether the threshold
// is met (forcing function should fire). Non-idle-hold results leave strikes
// unchanged. When the threshold is met, strikes reset for that agent after firing.
func (t *Tracker) Record(agent string, r Result) (thresholdMet bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !r.IdleHold {
		return false
	}
	t.strikes[agent]++
	if t.strikes[agent] >= StrikeThreshold {
		delete(t.strikes, agent)
		return true
	}
	return false
}

// Strikes returns the current strike count for an agent (for tests).
func (t *Tracker) Strikes(agent string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
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
