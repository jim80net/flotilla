package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
)

// schedulePollInterval is how often the scheduler re-checks wall-clock slots.
// One minute is coarse enough to avoid hot-looping yet tight enough for daily
// parade-style prompts (the detector tick alone may be 20m+).
const schedulePollInterval = time.Minute

// scheduleLateGrace is how far past the scheduled instant a fire still counts as
// on-time (no [late] marker). Beyond this, catch-up fires prefix the prompt.
const scheduleLateGrace = 90 * time.Second

// ScheduleState is the DURABLE last-fired snapshot for daemon-native schedules
// (#413): per schedule name, the RFC3339 instant of the last occurrence that was
// committed. It is a DISK SIDECAR — separate from the detector snapshot — so a
// daemon restart does not double-fire or silently skip a missed slot.
type ScheduleState struct {
	// LastFired[name] = RFC3339 occurrence instant last committed.
	LastFired map[string]string `json:"last_fired"`
}

// LoadScheduleState reads the schedule sidecar fail-safe. A missing or corrupt
// file returns an empty state (every schedule is due until first commit).
func LoadScheduleState(path string) ScheduleState {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("flotilla watch: schedule sidecar read failed for %q: %v (treating as never-fired)", path, err)
		}
		return ScheduleState{LastFired: map[string]string{}}
	}
	var s ScheduleState
	if err := json.Unmarshal(raw, &s); err != nil {
		log.Printf("flotilla watch: schedule sidecar at %q is corrupt: %v (treating as never-fired)", path, err)
		return ScheduleState{LastFired: map[string]string{}}
	}
	if s.LastFired == nil {
		s.LastFired = map[string]string{}
	}
	return s
}

// Save writes the schedule sidecar atomically (temp file + rename), modeled on
// SynthState.Save, so a crash mid-write never leaves a torn sidecar.
func (s ScheduleState) Save(path string) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal schedule sidecar: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create schedule sidecar temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write schedule sidecar temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close schedule sidecar temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename schedule sidecar into place: %w", err)
	}
	return nil
}

type parsedSchedule struct {
	name   string
	hour   int
	minute int
	loc    *time.Location
	to     string
	prompt string
}

// Scheduler fires roster schedules on daily wall-clock cadence inside flotilla watch.
type Scheduler struct {
	entries   []parsedSchedule
	statePath string
	rosterDir string
	state     ScheduleState
	enqueue   func(Job)
	now       func() time.Time
	mu        sync.Mutex
}

// NewScheduler builds a scheduler from roster schedules. statePath is the durable
// sidecar (<roster-dir>/flotilla-schedule-state.json in production); rosterDir
// resolves relative prompt file paths.
func NewScheduler(schedules []roster.Schedule, statePath, rosterDir string, enqueue func(Job)) *Scheduler {
	entries := make([]parsedSchedule, 0, len(schedules))
	for _, sch := range schedules {
		h, m, loc, err := roster.ParseDailyAt(sch.At)
		if err != nil {
			// Validated at roster load; skip defensively rather than crash the daemon.
			log.Printf("flotilla watch: schedule %q skipped: %v", sch.Name, err)
			continue
		}
		entries = append(entries, parsedSchedule{
			name: sch.Name, hour: h, minute: m, loc: loc, to: sch.To, prompt: sch.Prompt,
		})
	}
	return &Scheduler{
		entries:   entries,
		statePath: statePath,
		rosterDir: rosterDir,
		state:     LoadScheduleState(statePath),
		enqueue:   enqueue,
		now:       time.Now,
	}
}

// CatchUp runs one scheduling pass immediately — the restart catch-up for slots
// missed while the daemon was down (fires at most once per schedule with a late
// marker when appropriate).
func (sc *Scheduler) CatchUp() { sc.Tick() }

// Tick evaluates every schedule against the current wall clock and enqueues any
// due dispatches. Safe to call from multiple goroutines (e.g. the poll loop and
// the detector's ScheduleOnTick hook); last_fired prevents double-fire.
func (sc *Scheduler) Tick() {
	if len(sc.entries) == 0 || sc.enqueue == nil {
		return
	}
	now := sc.now()
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for _, ent := range sc.entries {
		occ, ok := dueOccurrence(now, ent.hour, ent.minute, ent.loc, sc.lastFiredInstant(ent.name))
		if !ok {
			continue
		}
		body, err := resolveSchedulePrompt(sc.rosterDir, ent.prompt)
		if err != nil {
			log.Printf("flotilla watch: schedule %q SKIP: resolve prompt: %v", ent.name, err)
			continue
		}
		if now.Sub(occ) > scheduleLateGrace {
			body = scheduleLatePrefix(ent.name, occ) + body
		}
		log.Printf("flotilla watch: schedule %q → %s (occurrence %s)", ent.name, ent.to, occ.Format(time.RFC3339))
		sc.state.LastFired[ent.name] = occ.Format(time.RFC3339)
		if err := sc.state.Save(sc.statePath); err != nil {
			log.Printf("flotilla watch: schedule sidecar persist failed: %v (continuing — at worst one extra fire after restart)", err)
		}
		sc.enqueue(Job{Agent: ent.to, Message: body, Kind: KindDetector})
	}
}

// Run is the scheduler poll loop: an immediate catch-up sweep on start, then a
// tick every schedulePollInterval until ctx is cancelled.
func (sc *Scheduler) Run(ctx context.Context) {
	sc.CatchUp()
	ticker := time.NewTicker(schedulePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sc.Tick()
		}
	}
}

func (sc *Scheduler) lastFiredInstant(name string) time.Time {
	raw, ok := sc.state.LastFired[name]
	if !ok || raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

// dueOccurrence returns the scheduled instant to fire now, if any. At most one
// occurrence per call — a multi-day outage catches up only the latest missed slot
// ("fire once"). An empty lastFired (first boot) never backfills yesterday: the
// daemon waits for today's slot to pass before the first fire.
func dueOccurrence(now time.Time, hour, minute int, loc *time.Location, lastFired time.Time) (time.Time, bool) {
	nowInLoc := now.In(loc)
	todayOcc := time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), hour, minute, 0, 0, loc)
	if !now.Before(todayOcc) {
		if lastFired.IsZero() || todayOcc.After(lastFired) {
			return todayOcc, true
		}
		return time.Time{}, false
	}
	if lastFired.IsZero() {
		return time.Time{}, false
	}
	yesterdayOcc := todayOcc.AddDate(0, 0, -1)
	if yesterdayOcc.After(lastFired) {
		return yesterdayOcc, true
	}
	return time.Time{}, false
}

func scheduleLatePrefix(name string, occ time.Time) string {
	return fmt.Sprintf("[schedule late: %s due %s]\n\n", name, occ.Format(time.RFC3339))
}

// resolveSchedulePrompt returns the delivery body: if prompt names an existing file
// (absolute, or relative to rosterDir), read it; otherwise treat prompt as inline.
func resolveSchedulePrompt(rosterDir, prompt string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("empty prompt")
	}
	candidates := []string{prompt}
	if !filepath.IsAbs(prompt) && rosterDir != "" {
		candidates = append([]string{filepath.Join(rosterDir, prompt)}, candidates...)
	}
	for _, p := range candidates {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("read prompt file %q: %w", p, err)
		}
		return string(raw), nil
	}
	return prompt, nil
}
