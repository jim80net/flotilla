// Package delegatenudge detects when a coordinator (any XO or Chief of Staff) is
// IC-ing — personally executing multi-step build work instead of routing it to desks.
// An IC-ing coordinator goes quiet and cannot communicate; the operator's standing
// rule is that coordinators delegate to preserve bandwidth (#232).
//
// The detector is PURE (pattern matching only) so the watch daemon can run it on
// every coordinator turn-final without coupling to a surface driver.
package delegatenudge

import (
	"regexp"
	"strings"
	"sync"
)

// StrikeThreshold is how many consecutive IC-ing turn-finals on the same coordinator
// trigger the dispatch nudge. One slip might be urgent hands-on; two is a pattern.
const StrikeThreshold = 2

// ManagementHarness is the coordinator-seat surface (Claude). Execution work belongs
// on grok workhorse desks — the nudge only fires for management-harness coordinators.
const ManagementHarness = "claude-code"

// Result is the verdict for one turn-final body.
type Result struct {
	// InlineBuild is true when the body shows hands-on build/ship work without a
	// delegation signal — the coordinator was IC-ing, not coordinating.
	InlineBuild bool
	// Signal names the matched IC class for logging (empty when InlineBuild is false).
	Signal string
}

var (
	icPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:implemented|fixed the bug|patched|refactored|wrote tests|added tests)\b`),
		regexp.MustCompile(`(?i)\b(?:committed|pushed|merged|opened)\b.{0,40}\b(?:PR|pull request|branch)\b`),
		regexp.MustCompile(`(?i)\bPR\s*#?\d+\b.{0,80}\b(?:ready|merged|opened|green|CI)\b`),
		regexp.MustCompile(`(?i)\b(?:go test|npm test|pytest|cargo test)\b.{0,80}\b(?:pass|green|ok)\b`),
		regexp.MustCompile(`(?i)\b(?:StrReplace|Write|EditNotebook|ApplyPatch)\b`),
		regexp.MustCompile(`(?i)\b(?:created|updated|modified)\b.{0,60}\b(?:file|module|package)\b`),
		regexp.MustCompile(`(?i)\b(?:inline build|hands-on|personally (?:fixed|implemented|shipped))\b`),
	}

	delegationPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bflotilla send\b`),
		regexp.MustCompile(`(?i)\b(?:dispatched?|routed?|delegated?)\b`),
		regexp.MustCompile(`(?i)\b(?:spawned?|woke|resumed?)\b.{0,60}\b(?:desk|agent|pane)\b`),
		regexp.MustCompile(`(?i)\b@\w[\w-]*\b.{0,120}\b(?:please|take|own|handle|implement)\b`),
		regexp.MustCompile(`(?i)\b(?:dispatch(?:ed)?|route(?:d)?)\b.{0,40}\bto\b`),
	}

	// coordinationOnly exempts a turn that is purely synthesis/brief/routing prose with
	// no IC signal — coordinators legitimately finish those turns without delegating.
	coordinationOnlyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:executive (?:summary|brief)|synthesis rollup|visibility synthesis)\b`),
		regexp.MustCompile(`(?i)\b(?:checkpoint only|routing (?:question|checkpoint)|fleet (?:status|report))\b`),
		regexp.MustCompile(`(?i)\b(?:liveness check|reply with a one-word)\b`),
	}
)

// IsManagementHarness reports whether surface is a coordinator (management) seat.
// Empty surface defaults to claude-code per roster/surface conventions.
func IsManagementHarness(surface string) bool {
	return surface == "" || surface == ManagementHarness
}

// Check classifies one turn-final for a coordinator on the given harness surface.
// InlineBuild is true only on management (Claude) seats, when an IC signal matches,
// no delegation signal is present, and the turn is not a coordination-only carve-out.
func Check(text string, surface string) Result {
	if text == "" || !IsManagementHarness(surface) {
		return Result{}
	}
	if showsDelegation(text) {
		return Result{}
	}
	if coordinationOnly(text) && !hasICSignal(text) {
		return Result{}
	}
	for _, re := range icPatterns {
		if re.MatchString(text) {
			return Result{InlineBuild: true, Signal: re.String()}
		}
	}
	return Result{}
}

func showsDelegation(text string) bool {
	for _, re := range delegationPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func coordinationOnly(text string) bool {
	for _, re := range coordinationOnlyPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func hasICSignal(text string) bool {
	for _, re := range icPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// Tracker accrues IC-ing strikes per coordinator agent.
type Tracker struct {
	mu      sync.Mutex
	strikes map[string]int
}

// NewTracker builds an empty per-agent strike counter.
func NewTracker() *Tracker {
	return &Tracker{strikes: make(map[string]int)}
}

// Record applies one Check result. Non-matches leave strikes unchanged. When the
// threshold is met, strikes reset after reporting thresholdMet=true.
func (t *Tracker) Record(agent string, r Result) (thresholdMet bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !r.InlineBuild {
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

// NudgePrompt is injected when the strike threshold is met.
func NudgePrompt(agent string) string {
	var b strings.Builder
	b.WriteString("[flotilla coordinator-delegation nudge] You are a coordinator")
	if agent != "" {
		b.WriteString(" (")
		b.WriteString(agent)
		b.WriteString(")")
	}
	b.WriteString(". Your recent turn(s) show hands-on build/ship work — IC-ing — ")
	b.WriteString("instead of routing to desks. An IC-ing coordinator goes quiet and ")
	b.WriteString("cannot communicate; the operator cannot see the fleet move.\n\n")
	b.WriteString("Harness allocation: coordinator seats (Claude) are for judgment — ")
	b.WriteString("dispatch, gate bars, review/verify, merge authority, operator comms. ")
	b.WriteString("Execution (code, builds, migrations, sweeps) belongs on grok workhorse desks.\n\n")
	b.WriteString("Preserve your bandwidth: route implementation via `flotilla send @<desk> \"…\"` ")
	b.WriteString("(prefer a grok execution desk). Stay on synthesis, routing, and gate decisions.\n\n")
	b.WriteString("On this turn: stop the inline build-loop — dispatch to a grok desk, then ")
	b.WriteString("report what you routed and what you are holding for the operator.")
	return b.String()
}
