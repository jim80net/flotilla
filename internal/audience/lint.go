// Package audience provides fail-closed reader-modeling checks for operator-facing prose.
package audience

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Finding is one actionable audience-contract violation.
type Finding struct {
	Line    int    `json:"line"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

var (
	issueRefRE  = regexp.MustCompile(`(?i)(?:\b(?:PR|issue)\s*)?#\d+\b`)
	shaRE       = regexp.MustCompile(`(?i)\b[0-9a-f]{7,40}\b`)
	walkLabelRE = regexp.MustCompile(`(?i)\b(?:walk\s*\d+|seven[- ]?c|7c)\b`)
	scoreRE     = regexp.MustCompile(`(?i)(?:\b\d+\s*/\s*\d+\b|\bscore\b.{0,20}\b(?:delta|up|rose|improved|→|->)\b)`)
	mdPrefixRE  = regexp.MustCompile(`^(?:#{1,6}|>|[-*+]|\d+\.)\s+`)
	groundedRE  = regexp.MustCompile(`(?i)\b(?:before|after|now|no longer|cannot|could|failed|failure|fixed|shipped|live|blocks?|blocked|available|unavailable|visible|hidden|lost|arrives?|retains?|keeps?|prevents?|supports?|allows?|rejected|broke|broken|needs?)\b`)
)

var defaultJargon = []string{"dogfood", "harness", "nonce", "outbox", "worktree"}

// DefaultJargon returns a copy of the built-in operator-spine lexicon.
func DefaultJargon() []string { return append([]string(nil), defaultJargon...) }

// LintParade checks the main spine of a slides.md deck. Content inside details
// blocks is technical depth and intentionally receives the softer rule.
func LintParade(src string, jargon []string) []Finding {
	lines := splitLines(src)
	var findings []Finding
	inDetails := false
	slideStart := 1
	var spine []numberedLine
	seenJargon := map[string]bool{}
	flush := func(end int) {
		if len(spine) == 0 {
			return
		}
		findings = append(findings, lintParadeSlide(spine, slideStart, jargon, seenJargon)...)
		spine = nil
		slideStart = end + 1
	}
	for i, raw := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		lower := strings.ToLower(trimmed)
		if !inDetails && trimmed == "---" {
			flush(lineNo - 1)
			continue
		}
		if strings.Contains(lower, "<details") {
			inDetails = true
			continue
		}
		if inDetails {
			if strings.Contains(lower, "</details>") {
				inDetails = false
			}
			continue
		}
		spine = append(spine, numberedLine{lineNo, raw})
	}
	flush(len(lines))
	return sorted(findings)
}

type numberedLine struct {
	n int
	s string
}

func lintParadeSlide(lines []numberedLine, slideStart int, jargon []string, seenJargon map[string]bool) []Finding {
	var findings []Finding
	var meaningful []numberedLine
	for _, line := range lines {
		text := visibleMarkdown(line.s)
		if text == "" {
			continue
		}
		meaningful = append(meaningful, numberedLine{line.n, text})
		if issueRefRE.MatchString(text) && !identifierFooter(text) {
			findings = append(findings, Finding{line.n, "identifier-on-spine", "move issue/PR identifiers to a detail or footer; lead with the product meaning"})
		}
		findings = append(findings, lintJargon(line.n, text, jargon, seenJargon)...)
	}
	if len(meaningful) == 0 {
		return findings
	}
	title := meaningful[0]
	if issueRefRE.MatchString(title.s) || shaRE.MatchString(title.s) {
		findings = append(findings, Finding{title.n, "identifier-title", "title must state the reader-visible claim, not an issue number or commit"})
	}
	claims := meaningful[1:]
	if len(claims) == 0 {
		findings = append(findings, Finding{title.n, "missing-claim", "slide needs a before/change/after, defect, shipped behavior, or operator outcome"})
		return findings
	}
	if dividerOnly(claims) {
		return findings
	}
	onlyScore := true
	grounded := groundedRE.MatchString(title.s)
	for _, claim := range claims {
		grounded = grounded || groundedRE.MatchString(claim.s)
		if !scoreRE.MatchString(claim.s) && !walkLabelRE.MatchString(claim.s) && !issueRefRE.MatchString(claim.s) {
			onlyScore = false
			break
		}
	}
	if onlyScore {
		findings = append(findings, Finding{slideStart, "score-only", "score or walk movement is not a product claim; name what changed for the reader"})
	}
	if !grounded {
		findings = append(findings, Finding{slideStart, "ungrounded-claim", "lead with a defect, shipped behavior, reader-visible outcome, or before/change/after evidence"})
	}
	return findings
}

func dividerOnly(lines []numberedLine) bool {
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line.s), "![") {
			return false
		}
	}
	return len(lines) > 0
}

func identifierFooter(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(lower, "identifiers:") || strings.HasPrefix(lower, "references:")
}

// LintOperatorPR validates the dedicated operator-summary section while leaving
// engineering detail below it intact.
func LintOperatorPR(src string, jargon []string) []Finding {
	lines := splitLines(src)
	start, end := operatorSection(lines)
	if start < 0 {
		return []Finding{{1, "missing-operator-summary", "add a ## Operator summary section with Before, Change, After, and Identifiers"}}
	}
	sections := map[string][]numberedLine{}
	order := []string{}
	current := ""
	headingLine := map[string]int{}
	for i := start + 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if name, ok := summaryHeading(trimmed); ok {
			current = name
			order = append(order, name)
			headingLine[name] = i + 1
			continue
		}
		if current != "" && visibleMarkdown(trimmed) != "" {
			sections[current] = append(sections[current], numberedLine{i + 1, visibleMarkdown(trimmed)})
		}
	}
	var findings []Finding
	seenJargon := map[string]bool{}
	want := []string{"before", "change", "after", "identifiers"}
	for _, name := range want {
		if len(sections[name]) == 0 {
			line := start + 1
			if n := headingLine[name]; n > 0 {
				line = n
			}
			findings = append(findings, Finding{line, "missing-" + name, fmt.Sprintf("operator summary needs a non-empty %s section", name)})
		}
	}
	if !orderedSubset(order, want) {
		findings = append(findings, Finding{start + 1, "summary-order", "order operator summary as Before, Change, After, then Identifiers footer"})
	}
	for _, name := range []string{"before", "change", "after"} {
		for _, line := range sections[name] {
			if issueRefRE.MatchString(line.s) || shaRE.MatchString(line.s) {
				findings = append(findings, Finding{line.n, "identifier-before-footer", "move issue, PR, and commit identifiers to the Identifiers footer"})
			}
			findings = append(findings, lintJargon(line.n, line.s, jargon, seenJargon)...)
		}
	}
	return sorted(findings)
}

func operatorSection(lines []string) (int, int) {
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "## Operator summary") {
			start = i
			continue
		}
		if start >= 0 && strings.HasPrefix(trimmed, "## ") {
			return start, i
		}
	}
	return start, len(lines)
}

func summaryHeading(line string) (string, bool) {
	line = strings.TrimSpace(strings.TrimLeft(line, "#"))
	line = strings.TrimSuffix(line, ":")
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "before", "change", "after", "identifiers":
		return strings.ToLower(strings.TrimSpace(line)), true
	}
	return "", false
}

func orderedSubset(got, want []string) bool {
	pos := -1
	for _, name := range want {
		found := -1
		for i := pos + 1; i < len(got); i++ {
			if got[i] == name {
				found = i
				break
			}
		}
		if found < 0 {
			return false
		}
		pos = found
	}
	return true
}

func lintJargon(line int, text string, jargon []string, seen map[string]bool) []Finding {
	lower := strings.ToLower(text)
	var out []Finding
	for _, raw := range jargon {
		term := strings.ToLower(strings.TrimSpace(raw))
		if term == "" || seen[term] {
			continue
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(term) + `\b`)
		loc := re.FindStringIndex(lower)
		if loc == nil {
			continue
		}
		seen[term] = true
		if glossed(lower, loc[1]) {
			continue
		}
		out = append(out, Finding{line, "unglossed-jargon", fmt.Sprintf("gloss %q on first use or move it to technical detail", term)})
	}
	return out
}

func glossed(line string, after int) bool {
	tail := strings.TrimSpace(line[after:])
	if len(tail) < 4 {
		return false
	}
	return strings.HasPrefix(tail, "—") || strings.HasPrefix(tail, "-") || strings.HasPrefix(tail, ":") || strings.HasPrefix(tail, "(")
}

func visibleMarkdown(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "<!--") {
		return ""
	}
	for mdPrefixRE.MatchString(line) {
		line = strings.TrimSpace(mdPrefixRE.ReplaceAllString(line, ""))
	}
	return line
}

func splitLines(src string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func sorted(in []Finding) []Finding {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Line == in[j].Line {
			return in[i].Code < in[j].Code
		}
		return in[i].Line < in[j].Line
	})
	return in
}
