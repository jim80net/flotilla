package unacked

import (
	"regexp"
	"strings"
)

var (
	trivialAck  = regexp.MustCompile(`(?i)^\s*(?:thanks?|thank you|ok(?:ay)?|yep|yes|no|👍|✅|done|got it|sounds good|perfect|great)\s*[.!]*\s*$`)
	workingOnIt = regexp.MustCompile(`(?i)\b(?:working on (?:it|your)|i(?:'|')?ll route|still working|on it)\b`)
	requestLead = regexp.MustCompile(`(?i)^(?:please|can you|could you|would you|need you to|go ahead|ship|fix|implement|review|merge|deploy|check|investigate|look into)\b`)
)

// looksLikeRequest is a mechanical v1 classifier: questions, desk mentions, or
// imperative operator directives — excluding trivial acknowledgments.
func looksLikeRequest(content string) bool {
	s := strings.TrimSpace(content)
	if s == "" {
		return false
	}
	if trivialAck.MatchString(s) {
		return false
	}
	if strings.Contains(s, "?") {
		return true
	}
	if strings.HasPrefix(s, "@") {
		return true
	}
	if requestLead.MatchString(s) {
		return true
	}
	// Longer operator prose without a question mark is often a directive.
	if len([]rune(s)) >= 80 {
		return true
	}
	return false
}

// isWorkingOnIt matches hotline soft-ack prose (aligned with reply.go escalation).
func isWorkingOnIt(content string) bool {
	return workingOnIt.MatchString(content)
}
