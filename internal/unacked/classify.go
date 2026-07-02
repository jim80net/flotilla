package unacked

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	trivialAck = regexp.MustCompile(`(?i)^\s*(?:thanks?|thank you|ok(?:ay)?|yep|yes|no|👍|✅|done|got it|sounds good|perfect|great)\s*[.!]*\s*$`)
	// Soft-ack phrases only — bare "on it" matches substantive prose ("focus on it next").
	workingOnIt = regexp.MustCompile(`(?i)\b(?:working on (?:it|your)|still working(?: on)?|i(?:'|')?ll route|getting on it)\b`)
	requestLead = regexp.MustCompile(`(?i)^(?:please|can you|could you|would you|need you to|go ahead|ship|fix|implement|review|merge|deploy|check|investigate|look into)\b`)
)

// looksLikeRequest is a mechanical v1 classifier: questions, desk mentions, or
// imperative operator directives — excluding trivial acknowledgments.
func looksLikeRequest(content string) bool {
	s := strings.TrimSpace(content)
	if s == "" {
		return false
	}
	body := stripLeadingAtMention(s)
	if body == "" {
		return false
	}
	if trivialAck.MatchString(body) {
		return false
	}
	if strings.Contains(body, "?") {
		return true
	}
	if strings.HasPrefix(s, "@") {
		return true // @-addressed, non-trivial after desk prefix stripped
	}
	if requestLead.MatchString(body) {
		return true
	}
	// Longer operator prose without a question mark is often a directive.
	if len([]rune(body)) >= 80 {
		return true
	}
	return false
}

// stripLeadingAtMention removes a leading @desk token (relay.Route shape) so
// trivial-ack classification applies to "@xo thanks" the same as bare "thanks".
func stripLeadingAtMention(s string) string {
	if !strings.HasPrefix(s, "@") {
		return s
	}
	afterAt := s[1:]
	i := strings.IndexFunc(afterAt, unicode.IsSpace)
	if i < 0 {
		return ""
	}
	return strings.TrimLeft(afterAt[i:], " \t\r\n")
}

// isWorkingOnIt matches hotline soft-ack prose (aligned with reply.go escalation).
func isWorkingOnIt(content string) bool {
	return workingOnIt.MatchString(content)
}
