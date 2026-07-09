package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jim80net/flotilla/internal/chapterend"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

// chapterEndRecycleEnabled reports whether detector-enqueued chapter-end recycle is on.
// DEFAULTS ON (#443). Disable with FLOTILLA_CHAPTER_END_RECYCLE=0/false/no/off.
func chapterEndRecycleEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FLOTILLA_CHAPTER_END_RECYCLE"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// chapterEndOnFinish builds the #443 finish-edge: read turn-final + per-desk backlog,
// detect lane-done (stacked-PR suppressed), then either auto-dispatch flotilla recycle
// or nudge the desk/adjutant. Coordinators use recycle --self (handoff+rotate+takeover).
func chapterEndOnFinish(
	cfg *roster.Config,
	rosterDir string,
	tracker *chapterend.Tracker,
	enqueue func(watch.Job),
	tryBeginFlight func(agent string) bool,
	endFlight func(agent string),
) func(agent string) {
	if tracker == nil {
		return nil
	}
	return func(agent string) {
		if agent == "" {
			return
		}
		// Approval-sensitive desks never auto-recycle (GATE-4 analogue for lifecycle).
		if a, err := cfg.Agent(agent); err == nil && a.ApprovalSensitive {
			return
		}
		text, ok, err := readDeskTurnFinal(cfg, agent)
		if err != nil || !ok {
			return
		}
		backlogMD := readDeskBacklogMarkdown(rosterDir, agent)
		r := chapterend.Check(text, backlogMD)
		if r.SuppressReason != "" {
			log.Printf("flotilla watch: chapter-end suppressed for %q: %s", agent, r.SuppressReason)
			_ = tracker.Record(agent, r) // record suppress; no dispatch
			return
		}
		if !tracker.Record(agent, r) {
			return
		}
		log.Printf("flotilla watch: chapter-end %q signal=%s", agent, r.Signal)

		// Adjutant evaluate path: notify layer adjutant when configured (policy co-owner).
		if owner := cfg.OwningXO(agent, cfg.XOAgent); owner != "" {
			if adj := cfg.AdjutantFor(owner); adj != "" {
				enqueue(watch.Job{
					Agent:   adj,
					Message: chapterend.RecycleDispatchPrompt(agent, r),
					Kind:    watch.KindDetector,
				})
			}
		}

		if !chapterEndRecycleEnabled() {
			enqueue(watch.Job{Agent: agent, Message: chapterend.NudgePrompt(agent, r), Kind: watch.KindDetector})
			return
		}

		if tryBeginFlight != nil && !tryBeginFlight(agent) {
			log.Printf("flotilla watch: chapter-end recycle %q skipped — flight already in progress", agent)
			return
		}
		// Coordinators: recycle --self (handoff+rotate+takeover), never process-kill from own seat.
		self := cfg.IsCoordinator(agent)
		go dispatchChapterEndRecycle(agent, self, endFlight)
	}
}

func readDeskBacklogMarkdown(rosterDir, agent string) string {
	if rosterDir == "" || agent == "" {
		return ""
	}
	path := filepath.Join(rosterDir, "flotilla-"+agent+"-backlog.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}

// dispatchChapterEndRecycle execs flotilla recycle off the tick path (side-channel).
func dispatchChapterEndRecycle(agent string, self bool, endFlight func(string)) {
	if endFlight != nil {
		defer endFlight(agent)
	}
	bin := "flotilla"
	if exe, err := os.Executable(); err == nil {
		bin = exe
	}
	args := []string{"recycle", agent}
	if self {
		args = append(args, "--self")
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("flotilla watch: chapter-end recycle %q failed: %v\n%s", agent, err, out)
		return
	}
	log.Printf("flotilla watch: chapter-end recycle %q completed\n%s", agent, out)
}
