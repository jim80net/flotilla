package dash

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/jim80net/flotilla/internal/backlog"
)

// QueueItem is the operator-facing projection of one backlog drive-queue line for
// the dash (#419). Title and summary are reader-modeled; Internal carries the full
// coordinator ledger prose for a collapsed drill-in — never as the primary view.
type QueueItem struct {
	Status   string `json:"status"`
	Title    string `json:"title"`
	Summary  string `json:"summary,omitempty"`
	Scope    string `json:"scope,omitempty"` // desk agent name; empty = coordinator/XO scope
	Internal string `json:"internal,omitempty"`
	Raw      string `json:"raw"`
}

var (
	queueDisplayDelimiter = " :: "
	internalJargon        = regexp.MustCompile(`(?i)(PR\s*#\d+|\b[0-9a-f]{7,40}\b|cos\s*gate|cubic|RFC3339|\d{4}-\d{2}-\d{2}T\d{2}:\d{2})`)
	scopeAtAgent          = regexp.MustCompile(`(?i)@([a-z][a-z0-9-]*)\b`)
	scopeArrowAgent       = regexp.MustCompile(`(?i)(?:→|->)\s*([a-z][a-z0-9-]*)\b`)
	// Compiled ONCE (not per line) — these run in ParseQueueItemDisplay's per-line hot path.
	listMarkerBody = regexp.MustCompile(`^\s*(?:\d+\.|[-*+])\s*\[[^\]]+\]\s*(.*)$`)
	listGlyph      = regexp.MustCompile(`^\s*(?:\d+\.|[-*+])\s+`)
)

// ParseQueueItemDisplay turns one raw backlog markdown list line into the operator
// layer the work-queue modal renders first. An explicit "title :: summary" segment
// (double-colon spaced) in the post-marker body is preferred; otherwise the title
// is derived from plain-language text before internal jargon tokens.
func ParseQueueItemDisplay(rawLine string) QueueItem {
	raw := strings.TrimSpace(rawLine)
	item := QueueItem{Raw: raw, Title: "Work item", Internal: raw}
	marker := backlog.ClassifyLine(raw)
	if marker == "" {
		item.Title = deriveTitle(stripListGlyph(raw))
		return item
	}
	item.Status = marker
	body := queueBodyAfterMarker(raw)
	if body == "" {
		item.Title = humanStatus(marker)
		return item
	}
	if parts := strings.Split(body, queueDisplayDelimiter); len(parts) >= 2 {
		item.Title = strings.TrimSpace(parts[0])
		item.Summary = strings.TrimSpace(parts[1])
		if len(parts) > 2 {
			item.Internal = strings.TrimSpace(strings.Join(parts[2:], queueDisplayDelimiter))
		} else if looksInternal(item.Summary) {
			item.Internal = item.Summary
			item.Summary = ""
		} else {
			item.Internal = body
		}
	} else {
		item.Title = deriveTitle(body)
		item.Internal = body
		if !TitleIsOperatorFacing(item.Title) {
			item.Title = humanStatus(marker)
		}
	}
	if item.Title == "" {
		item.Title = humanStatus(marker)
	}
	if item.Internal == item.Title || item.Internal == item.Summary {
		item.Internal = body
	}
	item.Scope = extractScope(body)
	return item
}

// extractScope returns the desk agent a backlog item is scoped to (@desk or → desk).
// An empty scope means coordinator/XO-level (fleet-wide for the hub), not a desk item.
func extractScope(body string) string {
	for _, m := range scopeAtAgent.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		agent := strings.ToLower(strings.TrimSpace(m[1]))
		if agent == "operator" {
			continue
		}
		return agent
	}
	for _, m := range scopeArrowAgent.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		agent := strings.ToLower(strings.TrimSpace(m[1]))
		if agent == "operator" {
			continue // "→ operator" is coordinator/XO-level (a review ask), not a desk scope
		}
		return agent
	}
	return ""
}

func queueBodyAfterMarker(raw string) string {
	m := listMarkerBody.FindStringSubmatch(raw)
	if len(m) < 2 {
		return stripListGlyph(raw)
	}
	return strings.TrimSpace(m[1])
}

func stripListGlyph(raw string) string {
	return strings.TrimSpace(listGlyph.ReplaceAllString(raw, ""))
}

func deriveTitle(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "Work item"
	}
	if idx := internalJargon.FindStringIndex(body); idx != nil && idx[0] > 0 {
		candidate := strings.TrimSpace(body[:idx[0]])
		candidate = trimTrailingPunct(candidate)
		if len(candidate) >= 8 {
			return truncateRunes(candidate, 120)
		}
	}
	if sent := firstSentence(body); sent != "" {
		return truncateRunes(sent, 120)
	}
	return truncateRunes(body, 120)
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if r == '.' && i+1 < len(s) && (s[i+1] == ' ' || s[i+1] == '\n') {
			return strings.TrimSpace(s[:i+1])
		}
	}
	return s
}

func trimTrailingPunct(s string) string {
	return strings.TrimRight(s, " ,;:-—")
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func looksInternal(s string) bool {
	return internalJargon.MatchString(s)
}

func humanStatus(marker string) string {
	switch marker {
	case "in-flight", "pending":
		return "In progress"
	case "next":
		return "Up next"
	case "blocked", "needs-attention":
		return "Blocked"
	case "awaiting-auth":
		return "Awaiting your decision"
	default:
		return strings.ReplaceAll(marker, "-", " ")
	}
}

// BuildQueueItems projects raw unblocked backlog lines for the dash API.
func BuildQueueItems(lines []string) []QueueItem {
	out := make([]QueueItem, 0, len(lines))
	for _, line := range lines {
		out = append(out, ParseQueueItemDisplay(line))
	}
	return out
}

// TitleIsOperatorFacing reports whether a derived title is free of internal ledger
// tokens — used by render-lint tests (#419).
func TitleIsOperatorFacing(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}
	if internalJargon.MatchString(title) {
		return false
	}
	// Reject titles that are mostly uppercase ledger dumps.
	upper := 0
	letters := 0
	for _, r := range title {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.IsUpper(r) {
			upper++
		}
	}
	if letters > 20 && upper*100/letters > 85 {
		return false
	}
	return true
}
