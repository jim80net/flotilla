package dispatch

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Bare PR-number citations are deliberately detected so they can fail closed:
// a number without repository identity is not safe terminal evidence.
var barePRCiteRE = regexp.MustCompile(`(?i)\b(?:PR|pull\s+request)\s*#?\s*(\d+)\b`)

var qualifiedPRCiteREs = []*regexp.Regexp{
	regexp.MustCompile(`(?i)https?://github\.com/([a-z0-9_.-]+/[a-z0-9_.-]+)/pull/(\d+)\b`),
	regexp.MustCompile(`(?i)\b([a-z0-9_.-]+/[a-z0-9_.-]+)#(\d+)\b`),
	regexp.MustCompile(`(?i)\b([a-z0-9_.-]+/[a-z0-9_.-]+)\s+(?:PR|pull\s+request)\s*#?\s*(\d+)\b`),
}

// commit citations are deliberately contextual: a bare hexadecimal token may
// be a branch head that is not shipped. Only hashes described as main or as a
// completed merge/squash are eligible for terminal-cargo disposition.
var mergedCommitRE = regexp.MustCompile(`(?i)\b(?:main(?:\s+sha)?|merged(?:\s+(?:main|at))?|squash(?:ed)?)\b[\s:@=-]{0,16}([0-9a-f]{7,40})\b`)

// PRCitation binds a pull-request number to its repository identity.
type PRCitation struct {
	Repository string
	Number     int
}

func (c PRCitation) String() string {
	return c.Repository + "#" + strconv.Itoa(c.Number)
}

// ExtractQualifiedPRCitations returns unique repository-qualified citations
// and whether any PR-style citation in the same message remained unscoped.
func ExtractQualifiedPRCitations(message string) (citations []PRCitation, hasUnscoped bool) {
	masked := []byte(message)
	type locatedCitation struct {
		start, end int
		citation   PRCitation
	}
	var located []locatedCitation
	for _, re := range qualifiedPRCiteREs {
		for _, loc := range re.FindAllStringSubmatchIndex(message, -1) {
			if len(loc) < 6 {
				continue
			}
			repository := strings.ToLower(message[loc[2]:loc[3]])
			n, err := strconv.Atoi(message[loc[4]:loc[5]])
			if err == nil && n > 0 {
				located = append(located, locatedCitation{loc[0], loc[1], PRCitation{Repository: repository, Number: n}})
			}
		}
	}
	sort.Slice(located, func(i, j int) bool { return located[i].start < located[j].start })
	seen := map[string]struct{}{}
	for _, match := range located {
		key := match.citation.String()
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			citations = append(citations, match.citation)
		}
		for i := match.start; i < match.end; i++ {
			masked[i] = ' '
		}
	}
	return citations, barePRCiteRE.Match(masked)
}

// MergedChecker reports whether a cited PR is already MERGED (or main contains
// its merge SHA). Production may wrap `gh pr view` / ledger; tests inject fakes.
// Empty/nil checker never suppresses.
type MergedChecker func(repository string, pr int) bool

// CommitOnMainChecker reports whether an explicitly merged/main commit citation
// is reachable from the repository's mainline reference.
type CommitOnMainChecker func(sha string) bool

// ExtractMergedCommitSHAs returns unique, explicitly terminal commit citations.
func ExtractMergedCommitSHAs(message string) []string {
	matches := mergedCommitRE.FindAllStringSubmatch(message, -1)
	seen := map[string]struct{}{}
	var out []string
	for _, m := range matches {
		sha := strings.ToLower(m[1])
		if _, ok := seen[sha]; ok {
			continue
		}
		seen[sha] = struct{}{}
		out = append(out, sha)
	}
	return out
}

// ShouldSuppressMerged reports whether message contains only repository-scoped
// PR citations and every cited PR is known merged. A single bare PR number
// makes the whole proof ambiguous and fails open for delivery.
func ShouldSuppressMerged(message string, isMerged MergedChecker) (citation PRCitation, ok bool) {
	if isMerged == nil {
		return PRCitation{}, false
	}
	prs, unscoped := ExtractQualifiedPRCitations(message)
	if unscoped || len(prs) == 0 {
		return PRCitation{}, false
	}
	for _, pr := range prs {
		if !isMerged(pr.Repository, pr.Number) {
			return PRCitation{}, false
		}
	}
	// All merged — return the first for logging.
	return prs[0], true
}

// ShouldSuppressTerminal accepts either the conservative all-cited-PRs merged
// proof or an explicitly terminal SHA that is confirmed on main. It never
// infers completion from prose such as "chapter closed" alone.
func ShouldSuppressTerminal(message string, isMerged MergedChecker, isCommitOnMain CommitOnMainChecker) (evidence string, ok bool) {
	if pr, merged := ShouldSuppressMerged(message, isMerged); merged {
		return "pr:" + pr.String(), true
	}
	if isCommitOnMain == nil {
		return "", false
	}
	for _, sha := range ExtractMergedCommitSHAs(message) {
		if isCommitOnMain(sha) {
			return "sha:" + sha, true
		}
	}
	return "", false
}

// ChapterHoldActive reports whether a marker string requests chapter HOLD
// semantics for non-urgent resume (#616).
func ChapterHoldActive(marker string) bool {
	m := strings.TrimSpace(strings.ToLower(marker))
	return m == "hold" || m == "chapter-hold" || m == "1" || m == "true"
}

// ChapterHoldFile is the optional roster-adjacent marker that holds non-urgent
// dropped-dispatch reinjects until the chapter ends (#616).
const ChapterHoldFile = "flotilla-chapter-hold"

// ChapterHoldFromRoster reports whether <rosterDir>/flotilla-chapter-hold is active.
// Missing file ⇒ not holding. File contents "hold"/"true"/"1"/empty ⇒ holding.
func ChapterHoldFromRoster(rosterDir string) bool {
	if rosterDir == "" {
		return false
	}
	path := filepath.Join(rosterDir, ChapterHoldFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return true // presence alone is HOLD
	}
	return ChapterHoldActive(s)
}
