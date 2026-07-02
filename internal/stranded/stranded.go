// Package stranded detects when a desk finishes gate-obligation work without
// reporting back to the gate-holder (COS/XO) — the dropped/stranded-handoff class
// logged as #216 evidence. Pure pattern matching; no I/O.
package stranded

import (
	"regexp"
	"strings"
	"sync"
)

// StrikeThreshold fires on the first stranded turn-final — one dropped gate report
// is already a fleet coordination failure.
const StrikeThreshold = 1

// Result is the verdict for one turn-final body.
type Result struct {
	Stranded bool
	Signal   string
}

var (
	gateObligationPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bno\s+self[- ]merge\b`),
		regexp.MustCompile(`(?i)\bsurface\s+to\s+COS\b`),
		regexp.MustCompile(`(?i)\bCOS\s+(?:re[- ]?)?gate\b`),
		regexp.MustCompile(`(?i)\bmerge\s+gate\b`),
		regexp.MustCompile(`(?i)\bgate[- ]holder\b`),
		regexp.MustCompile(`(?i)\btrio\s*\+\s*cubic\b`),
		regexp.MustCompile(`(?i)\bPR\s*#?\d+\b.{0,120}\b(?:cubic|CI|merge)\b`),
	}

	gateReportPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bflotilla\s+send\b.{0,100}\bcos\b`),
		regexp.MustCompile(`(?i)\b(?:surfaced|delivered|reported)\s+to\s+cos\b`),
		regexp.MustCompile(`(?i)\bturn\s+confirmed\b.{0,60}\bcos\b`),
		regexp.MustCompile(`(?i)\bgh\s+pr\s+comment\b`),
		regexp.MustCompile(`(?i)\bgate\s+report\b`),
		regexp.MustCompile(`(?i)\bsurface\s+for\s+(?:COS\s+)?merge\b`),
		regexp.MustCompile(`(?i)\bCOS\s+re[- ]?gate\b.{0,60}\b(?:posted|comment|report|complete)\b`),
	}

	openFindingsPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:unresolved|open)\s+(?:inline|thread|cubic)\b`),
		regexp.MustCompile(`(?i)\bcubic\b.{0,160}\b(?:unresolved|open)\b`),
		regexp.MustCompile(`(?i)\bP[123]\b.{0,100}\b(?:unresolved|unaddressed|open)\b`),
		regexp.MustCompile(`(?i)\bNEW\s+P[123]\b`),
	}

	settledPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:work\s+here\s+is\s+done|my\s+work\s+here\s+is\s+done|nothing\s+further)\b`),
		regexp.MustCompile(`(?i)(?:^|\n)\s*idle\s*$`),
		regexp.MustCompile(`(?i)\blane\s+closed\b`),
		regexp.MustCompile(`(?i)\bready\s+for\s+(?:COS\s+)?(?:merge|gate)\b`),
	}
)

// Check classifies one turn-final. Stranded is true when the body shows gate
// obligation or open review findings, signals settlement, and lacks evidence of
// reporting to the gate-holder.
func Check(text string) Result {
	if text == "" || gateReported(text) || !settled(text) {
		return Result{}
	}
	if openFindings(text) {
		return Result{Stranded: true, Signal: "open-findings-settled"}
	}
	if gateObligation(text) {
		return Result{Stranded: true, Signal: "gate-obligation-unreported"}
	}
	return Result{}
}

func gateObligation(text string) bool {
	for _, re := range gateObligationPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func gateReported(text string) bool {
	for _, re := range gateReportPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func openFindings(text string) bool {
	for _, re := range openFindingsPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func settled(text string) bool {
	for _, re := range settledPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// Tracker accrues stranded strikes per agent.
type Tracker struct {
	mu      sync.Mutex
	strikes map[string]int
}

// NewTracker builds an empty per-agent strike counter.
func NewTracker() *Tracker {
	return &Tracker{strikes: make(map[string]int)}
}

// Record applies one Check result. When the threshold is met, strikes reset.
func (t *Tracker) Record(agent string, r Result) (thresholdMet bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !r.Stranded {
		return false
	}
	t.strikes[agent]++
	if t.strikes[agent] >= StrikeThreshold {
		delete(t.strikes, agent)
		return true
	}
	return false
}

// Strikes returns the current strike count (for tests).
func (t *Tracker) Strikes(agent string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.strikes[agent]
}

// NudgePrompt is injected when a stranded handoff is detected.
func NudgePrompt(agent string) string {
	var b strings.Builder
	b.WriteString("[flotilla stranded-handoff break] Your turn-final shows gate-obligation work ")
	b.WriteString("settled WITHOUT reporting to the gate-holder")
	if agent != "" {
		b.WriteString(" (")
		b.WriteString(agent)
		b.WriteString(")")
	}
	b.WriteString(".\n\n")
	b.WriteString("The COS/XO gate-holder cannot merge or re-gate what it cannot see. ")
	b.WriteString("Before you go idle:\n")
	b.WriteString("- If cubic/review findings remain OPEN, fix them OR escalate a concrete blocker.\n")
	b.WriteString("- If work is merge-ready, surface NOW: `flotilla send --no-mirror cos \"<gate report>\"` ")
	b.WriteString("with head SHA, CI/cubic status, trio verdicts, and PR link.\n")
	b.WriteString("- Never end on \"idle\" / \"work done\" while the gate-holder is still blind.\n\n")
	b.WriteString("Execute the report on this turn; do not wait for a ping.")
	return b.String()
}
