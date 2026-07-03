package tracker

import (
	"fmt"
	"regexp"
	"strings"
)

// goalIDTrailerLine matches a coordinator `goal-id: <slug>` trailer on its own line
// anywhere in an issue body. The prefix is case-sensitive; the slug is captured
// case-sensitively. Malformed lines (missing slug, invalid characters) do not match.
var goalIDTrailerLine = regexp.MustCompile(`(?m)^[ \t]*goal-id:[ \t]+([A-Za-z0-9][A-Za-z0-9_.-]*)[ \t]*\r?$`)

// ParseGoalIDTrailer extracts the goal slug from a `goal-id: <slug>` trailer line in
// an issue body. It returns "" when the trailer is absent or malformed. The first valid
// matching line wins when multiple trailers are present.
func ParseGoalIDTrailer(body string) string {
	m := goalIDTrailerLine.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// EnrichIssue populates derived read-model fields from the issue body (goal-id trailer).
func EnrichIssue(issue *Issue) {
	if issue == nil {
		return
	}
	issue.GoalID = ParseGoalIDTrailer(issue.Body)
}

// IssueRef formats a pinned-repo issue reference (owner/repo#N) for goals work items.
func IssueRef(repo string, number int) string {
	return fmt.Sprintf("%s#%d", strings.TrimSpace(repo), number)
}
