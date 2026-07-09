// Package chapterend detects when a desk's current chapter of work is complete
// so the watch daemon (or adjutant evaluate path) can recycle the session
// (#443). Detection is pure: turn-final text + backlog markdown only — no I/O.
//
// Suppression rule (stacked-PR / mid-lane): if the backlog still has unblocked
// ([in-flight]/[next]/malformed) items, this is NOT chapter-end — recycle would
// destroy worktree context the remaining stack depends on. Lane-done requires
// zero unblocked actionable items.
package chapterend

import (
	"regexp"
	"strings"
	"sync"

	"github.com/jim80net/flotilla/internal/backlog"
)

// Signal names the matched chapter-end class (empty when not chapter-end).
type Signal string

const (
	SignalNone            Signal = ""
	SignalLaneDone        Signal = "lane-done"         // backlog drained + settled turn-final
	SignalCoordinatorMark Signal = "coordinator-mark"  // explicit "lane closed" / chapter complete
	SignalPRMergedSettled Signal = "pr-merged-settled" // turn-final names merged PR + settlement (only with empty unblocked)
)

// Result is the pure verdict for one finish edge.
type Result struct {
	ChapterEnd bool
	Signal     Signal
	// SuppressReason is non-empty when a candidate signal was seen but recycle is
	// suppressed (e.g. mid-stack unblocked items remain).
	SuppressReason string
}

// settled patterns — desk self-reports the chapter closed.
var settledPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:work\s+here\s+is\s+done|my\s+work\s+here\s+is\s+done|nothing\s+further)\b`),
	regexp.MustCompile(`(?i)\blane\s+closed\b`),
	regexp.MustCompile(`(?i)\bchapter\s+(?:closed|complete|done|ended)\b`),
	regexp.MustCompile(`(?i)\bready\s+for\s+(?:COS\s+)?(?:merge|gate)\b`),
	regexp.MustCompile(`(?i)(?:^|\n)\s*idle\s*$`),
	regexp.MustCompile(`(?i)\bsettle(?:d)?\s+(?:here|now)\b`),
}

// coordinatorMarkPatterns — explicit chapter-close language from coordinator or desk.
var coordinatorMarkPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:mark(?:ing|ed)?|close(?:d|s)?)\s+(?:the\s+)?lane\b`),
	regexp.MustCompile(`(?i)\blane\s+(?:is\s+)?(?:closed|done|complete)\b`),
	regexp.MustCompile(`(?i)\bchapter[- ]end\b`),
	regexp.MustCompile(`(?i)\brecycle\s+(?:me|this\s+desk|after\s+this)\b`),
}

// prMergedPatterns — strong PR-merge evidence in the turn-final.
var prMergedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:PR|pull\s+request)\s*#?\d+\b.{0,80}\bmerged\b`),
	regexp.MustCompile(`(?i)\bmerged\b.{0,80}\b(?:PR|pull\s+request)\s*#?\d+\b`),
	regexp.MustCompile(`(?i)\bgh\s+pr\s+merge\b`),
	regexp.MustCompile(`(?i)\bsquash[- ]merged\b`),
}

// Check classifies one turn-final against optional per-desk backlog markdown.
// backlogMD may be empty (missing ledger → cannot prove lane-done from backlog;
// only coordinator-mark with settlement can still fire).
func Check(turnFinal, backlogMD string) Result {
	text := strings.TrimSpace(turnFinal)
	if text == "" {
		return Result{}
	}

	st := backlog.Parse(backlogMD)
	unblockedN := len(st.Unblocked)

	// Mid-lane suppression: any actionable item remaining is NOT chapter-end.
	if unblockedN > 0 {
		if matchAny(text, prMergedPatterns) || matchAny(text, settledPatterns) {
			return Result{
				SuppressReason: "unblocked-items-remain",
			}
		}
		return Result{}
	}

	settled := matchAny(text, settledPatterns)
	coordMark := matchAny(text, coordinatorMarkPatterns)
	prMerged := matchAny(text, prMergedPatterns)

	// Lane-done: backlog found, no unblocked, at least one done (or empty found
	// section with coordinator mark), plus settlement signal.
	if st.Found && unblockedN == 0 && (st.Done > 0 || st.Items == 0) && (settled || coordMark || prMerged) {
		sig := SignalLaneDone
		if coordMark {
			sig = SignalCoordinatorMark
		} else if prMerged && !settled {
			sig = SignalPRMergedSettled
		}
		return Result{ChapterEnd: true, Signal: sig}
	}

	// No backlog ledger: only explicit coordinator mark + settlement (fail-closed
	// against false PR-merge mid-stack when we cannot see the lane).
	if !st.Found && coordMark && settled {
		return Result{ChapterEnd: true, Signal: SignalCoordinatorMark}
	}

	// Backlog present, fully drained, PR-merged + settled prose.
	if st.Found && unblockedN == 0 && st.Done > 0 && prMerged && settled {
		return Result{ChapterEnd: true, Signal: SignalPRMergedSettled}
	}

	return Result{}
}

func matchAny(text string, pats []*regexp.Regexp) bool {
	for _, re := range pats {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// Tracker dedupes chapter-end recycle dispatch per agent (one auto-recycle per
// chapter signal until the desk is re-armed by non-chapter activity or a new
// unblocked backlog item appears).
type Tracker struct {
	mu         sync.Mutex
	fired      map[string]Signal // agent → last fired signal
	suppressed map[string]string
}

// NewTracker returns an empty chapter-end dispatch tracker.
func NewTracker() *Tracker {
	return &Tracker{fired: map[string]Signal{}, suppressed: map[string]string{}}
}

// Record returns true when the chapter-end should dispatch recycle (edge).
// Non-chapter results clear the fired latch so a later chapter can fire again.
func (t *Tracker) Record(agent string, r Result) bool {
	if t == nil || agent == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if r.SuppressReason != "" {
		t.suppressed[agent] = r.SuppressReason
		return false
	}
	if !r.ChapterEnd {
		delete(t.fired, agent)
		delete(t.suppressed, agent)
		return false
	}
	if prev, ok := t.fired[agent]; ok && prev == r.Signal {
		return false // already dispatched this chapter signal
	}
	t.fired[agent] = r.Signal
	delete(t.suppressed, agent)
	return true
}

// NudgePrompt is the suggest-mode message when auto-recycle is off.
func NudgePrompt(agent string, r Result) string {
	return "[flotilla chapter-end] Desk " + agent + " appears to have finished a chapter (" +
		string(r.Signal) + "). Prefer: flotilla recycle " + agent +
		" (handoff→close→relaunch→takeover) before starting the next body of work — " +
		"do not accumulate context across chapters (#443)."
}

// RecycleDispatchPrompt is injected when auto-recycle is deferred (busy) or for adjutant.
func RecycleDispatchPrompt(agent string, r Result) string {
	return "[flotilla chapter-end] Lane chapter ended for " + agent + " (signal=" + string(r.Signal) +
		"). Mechanical act: flotilla recycle " + agent +
		" — fresh context before the next dispatch. Do not bare-/clear; use recycle (#443/#437)."
}
