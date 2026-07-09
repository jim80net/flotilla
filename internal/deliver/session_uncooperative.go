package deliver

import (
	"regexp"
	"strings"
)

// SessionUncooperativeTailLines bounds dead-end banner scans to the live footer /
// current-turn region (mirrors RateLimitTailLines — scrollback prose must not
// false-positive a recycle diagnosis).
const SessionUncooperativeTailLines = 16

// Known non-cooperative session banners: the harness cannot process any prompt
// (including a recycle handoff). Live-captured credit-exhausted Claude/Fable
// sessions (#558) plus existing rate-limit chrome. Generic phrases only — no
// deployment identifiers.
var (
	// sessionUsageCreditRE matches "You're out of usage credits" / "out of usage credits"
	// / "Run /usage-credits" (Claude account-side exhaustion).
	sessionUsageCreditRE = regexp.MustCompile(`(?i)(?:you(?:'re| are)?\s+)?out of usage credits|run\s+/usage-credits|/usage-credits`)
	// sessionReachedLimitRE matches "You've reached your … limit" (model/account caps).
	sessionReachedLimitRE = regexp.MustCompile(`(?i)you(?:'ve| have)\s+reached your\b.{0,80}\blimit\b`)
	// sessionUsageLimitRE matches bare "usage limit" / "usage-limit" footers.
	sessionUsageLimitRE = regexp.MustCompile(`(?i)\busage[-\s]?limit\b`)
	// sessionRateLimitRE matches provider rate-limit footers (Claude server-side phrase
	// is checked separately as a fixed string; this covers grok/aider wording).
	sessionRateLimitRE = regexp.MustCompile(`(?i)\brate limit(?:ed|s)?\b|\brate-limit(?:ed)?\b`)
)

// SessionUncooperative reports whether a captured pane shows a known dead-end
// banner — the session cannot process prompts, so a graceful recycle handoff
// will never confirm. Pure / testable — no pane I/O.
//
// On hit, phrase is a short operator-facing excerpt (the matched banner, trimmed).
// Callers should recommend `flotilla resume <agent> --force` rather than retrying
// the same graceful recycle path (#558).
func SessionUncooperative(captured string) (hit bool, phrase string) {
	if captured == "" {
		return false, ""
	}
	tail := TailRegion(captured, SessionUncooperativeTailLines)
	if tail == "" {
		return false, ""
	}
	// Prefer the fixed Claude server-side phrase (already used by RateLimitProbe).
	if strings.Contains(tail, ClaudeServerSidePhrase) {
		return true, ClaudeServerSidePhrase
	}
	lower := strings.ToLower(tail)
	// Prose false-positive guard: ordinary conversation that mentions "rate limit"
	// in scrollback history is excluded by TailRegion; still require non-prose
	// credit/limit patterns first (most load-bearing for #558).
	for _, re := range []*regexp.Regexp{sessionUsageCreditRE, sessionReachedLimitRE, sessionUsageLimitRE} {
		if loc := re.FindStringIndex(lower); loc != nil {
			return true, excerptPhrase(tail, loc[0], loc[1])
		}
	}
	// Rate-limit wording in the live tail (grok STATUS / aider retry chrome).
	if loc := sessionRateLimitRE.FindStringIndex(lower); loc != nil {
		// Avoid treating a pure prose sentence without chrome as a dead-end when
		// the only match is "rate limit" mid-paragraph in a short tail of chat.
		// Require a short line (footer chrome) or known throttle context words.
		line := lineContaining(lower, loc[0])
		if isThrottleFooterLine(line) {
			return true, excerptPhrase(tail, loc[0], loc[1])
		}
	}
	return false, ""
}

func excerptPhrase(tail string, start, end int) string {
	// Map lower-case match indices onto the original tail (same UTF-8 byte lengths
	// for ASCII banners we match; non-ASCII is rare in these footers).
	if start < 0 || end > len(tail) || start >= end {
		return strings.TrimSpace(tail)
	}
	// Expand to the full line for operator readability.
	lineStart, lineEnd := start, end
	for lineStart > 0 && tail[lineStart-1] != '\n' {
		lineStart--
	}
	for lineEnd < len(tail) && tail[lineEnd] != '\n' {
		lineEnd++
	}
	s := strings.TrimSpace(tail[lineStart:lineEnd])
	if len(s) > 120 {
		s = s[:117] + "…"
	}
	return s
}

func lineContaining(lower string, idx int) string {
	if idx < 0 || idx >= len(lower) {
		return ""
	}
	start, end := idx, idx
	for start > 0 && lower[start-1] != '\n' {
		start--
	}
	for end < len(lower) && lower[end] != '\n' {
		end++
	}
	return strings.TrimSpace(lower[start:end])
}

func isThrottleFooterLine(line string) bool {
	if line == "" {
		return false
	}
	// Reject multi-sentence chat that merely discusses limits (design docs, postmortems).
	if strings.Contains(line, "discussed") || strings.Contains(line, "design doc") ||
		strings.Contains(line, "we talked") || strings.Contains(line, "for example") {
		return false
	}
	// Short footer / status lines (not multi-sentence chat).
	if len(line) <= 90 {
		return true
	}
	// Longer lines need throttle-action chrome beyond a casual mention.
	return strings.Contains(line, "limiting") ||
		strings.Contains(line, "try again") ||
		strings.Contains(line, "sleeping") ||
		strings.Contains(line, "retrying")
}
