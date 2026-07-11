package dispatch

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PR number citations in dispatch bodies (e.g. "PR #614", "pull request 475").
var prCiteRE = regexp.MustCompile(`(?i)\b(?:PR|pull\s+request)\s*#?\s*(\d+)\b`)

// ExtractPRNumbers returns unique PR numbers cited in message, ascending.
func ExtractPRNumbers(message string) []int {
	matches := prCiteRE.FindAllStringSubmatch(message, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	var out []int
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// MergedChecker reports whether a cited PR is already MERGED (or main contains
// its merge SHA). Production may wrap `gh pr view` / ledger; tests inject fakes.
// Empty/nil checker never suppresses.
type MergedChecker func(pr int) bool

// ShouldSuppressMerged reports whether message cites at least one PR and every
// cited PR is known-merged (or the checker affirms any single cited PR when
// policy is any-merged — we require ALL cited PRs merged to auto-consume, so a
// multi-PR dispatch is not silenced by one merge).
func ShouldSuppressMerged(message string, isMerged MergedChecker) (pr int, ok bool) {
	if isMerged == nil {
		return 0, false
	}
	prs := ExtractPRNumbers(message)
	if len(prs) == 0 {
		return 0, false
	}
	for _, n := range prs {
		if !isMerged(n) {
			return 0, false
		}
	}
	// All merged — return the first for logging.
	return prs[0], true
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
