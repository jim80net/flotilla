package deliver

import "strings"

// RateLimitTailLines bounds the scan for a throttle banner to the live footer /
// current-turn region (mirrors ParseBusy's tail discipline — a banner scrolled into
// history must not false-positive).
const RateLimitTailLines = 8

// ClaudeServerSidePhrase is the live-captured Anthropic provider-wide throttle banner
// (operator incident 2026-06-29). Scope: server-side — every subscription under the
// provider is hit.
const ClaudeServerSidePhrase = "Server is temporarily limiting requests"

// GrokRateLimitPhrase is the live-captured official grok CLI rate-limit text (archived
// grok driver design, STATUS_MESSAGES). Scope characterized as account-side (per-key).
const GrokRateLimitPhrase = "Rate limit exceeded"

// TailRegion returns the last n lines of a captured pane (line-bounded, like ParseBusy).
func TailRegion(captured string, n int) string {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ClaudeRateLimitHit reports whether the tail region of captured shows the Anthropic
// server-side throttle banner. Pure / testable — no pane I/O.
func ClaudeRateLimitHit(captured string) (bool, string) {
	tail := TailRegion(captured, RateLimitTailLines)
	if strings.Contains(tail, ClaudeServerSidePhrase) {
		return true, ClaudeServerSidePhrase
	}
	return false, ""
}

// GrokRateLimitHit reports whether the tail region shows grok's rate-limit banner.
func GrokRateLimitHit(captured string) (bool, string) {
	tail := TailRegion(captured, RateLimitTailLines)
	if strings.Contains(tail, GrokRateLimitPhrase) {
		return true, GrokRateLimitPhrase
	}
	return false, ""
}
