package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/chapterend"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

const (
	defaultCoordinatorRecycleTenure = 7 * 24 * time.Hour
	coordinatorRecycleRetryBackoff  = time.Hour
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
	rosterPath string,
	coordinatorTenure time.Duration,
	tracker *chapterend.Tracker,
	enqueue func(watch.Job),
	tryBeginFlight func(agent string) bool,
	endFlight func(agent string),
) func(agent string) {
	if tracker == nil {
		return nil
	}
	rosterDir := filepath.Dir(rosterPath)
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
		if cfg.IsCoordinator(agent) {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				log.Printf("flotilla watch: coordinator tenure %q suppressed: resolve home: %v", agent, homeErr)
				return
			}
			tenureDue, age, tenureErr := coordinatorTenureDue(
				rosterDir, home, agent, coordinatorTenure, time.Now(),
			)
			if tenureErr != nil {
				log.Printf("flotilla watch: coordinator tenure %q suppressed: %v", agent, tenureErr)
				return
			}
			r = chapterend.CheckCoordinator(text, backlogMD, tenureDue)
			if tenureDue {
				log.Printf("flotilla watch: coordinator tenure %q due after %s", agent, age.Round(time.Minute))
			}
		}
		if r.SuppressReason != "" {
			log.Printf("flotilla watch: chapter-end suppressed for %q: %s", agent, r.SuppressReason)
			_ = tracker.Record(agent, r) // record suppress; no dispatch
			return
		}
		if r.Signal == chapterend.SignalCoordinatorTenure {
			// The durable attempt marker is the tenure retry latch. Clear the
			// chapter signal after its backoff so a failed child can retry.
			tracker.Reset(agent)
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
		if r.Signal == chapterend.SignalCoordinatorTenure {
			if err := recordCoordinatorRecycleAttempt(rosterDir, agent, time.Now()); err != nil {
				log.Printf("flotilla watch: coordinator tenure %q suppressed: record attempt: %v", agent, err)
				if endFlight != nil {
					endFlight(agent)
				}
				return
			}
		}
		go dispatchChapterEndRecycle(agent, self, rosterPath, endFlight)
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
func dispatchChapterEndRecycle(agent string, self bool, rosterPath string, endFlight func(string)) {
	if endFlight != nil {
		defer endFlight(agent)
	}
	bin := "flotilla"
	if exe, err := os.Executable(); err == nil {
		bin = exe
	}
	args := chapterEndRecycleArgs(agent, self, rosterPath)
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("flotilla watch: chapter-end recycle %q failed: %v\n%s", agent, err, out)
		return
	}
	log.Printf("flotilla watch: chapter-end recycle %q completed\n%s", agent, out)
}

func chapterEndRecycleArgs(agent string, self bool, rosterPath string) []string {
	if abs, err := filepath.Abs(rosterPath); err == nil {
		rosterPath = abs
	}
	args := []string{"recycle", agent, "--roster", rosterPath}
	if self {
		args = append(args, "--self")
	}
	return args
}

func parseCoordinatorRecycleTenure(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultCoordinatorRecycleTenure, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("coordinator recycle tenure %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("coordinator recycle tenure must be >= 0")
	}
	return d, nil
}

// coordinatorTenureDue measures a standing coordinator from the newest
// successful recycle/switch or its first-seen context marker. A failed attempt
// is a one-hour retry backoff, not a new context baseline.
func coordinatorTenureDue(
	rosterDir, homeDir, agent string,
	tenure time.Duration,
	now time.Time,
) (bool, time.Duration, error) {
	if tenure == 0 {
		return false, 0, nil
	}
	marker := filepath.Join(rosterDir, "flotilla-"+agent+"-context-start")
	baseline, err := markerModTime(marker)
	if err != nil && !os.IsNotExist(err) {
		return false, 0, err
	}
	haveBaseline := err == nil

	statusDir := filepath.Join(homeDir, ".flotilla", agent)
	for _, name := range []string{"last-recycle.json", "last-switch.json"} {
		if at, ok := successfulStatusTime(filepath.Join(statusDir, name)); ok && (!haveBaseline || at.After(baseline)) {
			baseline = at
			haveBaseline = true
		}
	}
	if !haveBaseline {
		if err := writeTimeMarker(marker, now); err != nil {
			return false, 0, err
		}
		return false, 0, nil
	}
	attempt := filepath.Join(rosterDir, "flotilla-"+agent+"-coordinator-recycle-attempt")
	if at, err := markerModTime(attempt); err == nil && now.Sub(at) < coordinatorRecycleRetryBackoff {
		return false, now.Sub(baseline), nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, 0, err
	}
	age := now.Sub(baseline)
	return age >= tenure, age, nil
}

func successfulStatusTime(path string) (time.Time, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	var rec struct {
		OK bool   `json:"ok"`
		At string `json:"at"`
	}
	if json.Unmarshal(raw, &rec) != nil || !rec.OK {
		return time.Time{}, false
	}
	if rec.At != "" {
		if at, err := time.Parse(time.RFC3339Nano, rec.At); err == nil {
			return at, true
		}
	}
	at, err := markerModTime(path)
	return at, err == nil
}

func markerModTime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

func recordCoordinatorRecycleAttempt(rosterDir, agent string, now time.Time) error {
	return writeTimeMarker(
		filepath.Join(rosterDir, "flotilla-"+agent+"-coordinator-recycle-attempt"),
		now,
	)
}

func writeTimeMarker(path string, at time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := fmt.Fprintln(tmp, at.UTC().Format(time.RFC3339Nano)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chtimes(path, at, at)
}
