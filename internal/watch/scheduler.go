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
	// LastFired[name] is the latest occurrence TRIGGERED. It is retained for
	// backward compatibility and due-slot calculation; it never implies delivery
	// or artifact success.
	LastFired map[string]string `json:"last_fired"`
	// Occurrences holds the latest durable lifecycle for each schedule.
	Occurrences map[string]ScheduleOccurrenceState `json:"occurrences,omitempty"`
}

// ScheduleOccurrenceState separates clock trigger, enqueue, confirmed surface
// delivery, artifact production, and escalation. Timestamps are RFC3339.
type ScheduleOccurrenceState struct {
	ScheduleName              string `json:"schedule_name"`
	Occurrence                string `json:"occurrence"`
	Target                    string `json:"target"`
	TriggeredAt               string `json:"triggered_at"`
	EnqueuedAt                string `json:"enqueued_at,omitempty"`
	DeliveryAttemptAt         string `json:"delivery_attempt_at,omitempty"`
	DeliveryAttemptPhase      string `json:"delivery_attempt_phase,omitempty"`
	DeliveryUncertainAt       string `json:"delivery_uncertain_at,omitempty"`
	EscalationUncertainAt     string `json:"escalation_uncertain_at,omitempty"`
	DeliveredAt               string `json:"delivered_at,omitempty"`
	ExpectedArtifact          string `json:"expected_artifact,omitempty"`
	ProductionWindow          string `json:"production_window,omitempty"`
	ArtifactDeadline          string `json:"artifact_deadline,omitempty"`
	ArtifactConfirmedAt       string `json:"artifact_confirmed_at,omitempty"`
	FailureAt                 string `json:"failure_at,omitempty"`
	FailureReason             string `json:"failure_reason,omitempty"`
	EscalationEnqueueIntentAt string `json:"escalation_enqueue_intent_at,omitempty"`
	EscalationEnqueuedAt      string `json:"escalation_enqueued_at,omitempty"`
	EscalatedAt               string `json:"escalated_at,omitempty"`
	LastAttemptFailedAt       string `json:"last_attempt_failed_at,omitempty"`
	LastDeliveryError         string `json:"last_delivery_error,omitempty"`
	Instruction               string `json:"instruction,omitempty"`
}

// LoadScheduleState is the inspection/test convenience loader. Production uses
// loadScheduleState directly and disables scheduling on unreadable/corrupt state
// rather than treating prior occurrences as never fired.
func LoadScheduleState(path string) ScheduleState {
	s, err := loadScheduleState(path)
	if err != nil {
		log.Printf("flotilla watch: schedule sidecar load failed for %q: %v", path, err)
		return newScheduleState()
	}
	return s
}

func loadScheduleState(path string) (ScheduleState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newScheduleState(), nil
		}
		return ScheduleState{}, fmt.Errorf("read: %w", err)
	}
	var s ScheduleState
	if err := json.Unmarshal(raw, &s); err != nil {
		return ScheduleState{}, fmt.Errorf("decode: %w", err)
	}
	if s.LastFired == nil {
		s.LastFired = map[string]string{}
	}
	if s.Occurrences == nil {
		s.Occurrences = map[string]ScheduleOccurrenceState{}
	}
	// Migrate the first lifecycle schema, which keyed one occurrence by schedule
	// name, into occurrence-addressed records. This prevents one unfinished day
	// from suppressing the next day's ceremony.
	for key, occurrence := range s.Occurrences {
		if occurrence.ScheduleName == "" {
			occurrence.ScheduleName = key
		}
		canonical := scheduleOccurrenceKey(occurrence.ScheduleName, occurrence.Occurrence)
		if canonical != key {
			delete(s.Occurrences, key)
			s.Occurrences[canonical] = occurrence
		} else {
			s.Occurrences[key] = occurrence
		}
	}
	return s, nil
}

func newScheduleState() ScheduleState {
	return ScheduleState{LastFired: map[string]string{}, Occurrences: map[string]ScheduleOccurrenceState{}}
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
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync schedule sidecar temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close schedule sidecar temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename schedule sidecar into place: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

type parsedSchedule struct {
	name                string
	hour                int
	minute              int
	loc                 *time.Location
	to                  string
	prompt              string
	expectedArtifact    string
	productionWindowRaw string
}

// Scheduler fires roster schedules on daily wall-clock cadence inside flotilla watch.
type Scheduler struct {
	entries           []parsedSchedule
	statePath         string
	rosterDir         string
	state             ScheduleState
	enqueue           func(Job)
	now               func() time.Time
	mu                sync.Mutex
	inflight          map[string]bool
	owningCoordinator func(string) string
	disabled          bool
	loadErr           error
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
			expectedArtifact:    sch.ExpectedArtifact,
			productionWindowRaw: sch.ProductionWindow,
		})
	}
	state, loadErr := loadScheduleState(statePath)
	if loadErr != nil {
		log.Printf("flotilla watch: REFUSING scheduled delivery because sidecar %q is unreadable: %v", statePath, loadErr)
		state = newScheduleState()
	}
	return &Scheduler{
		entries:   entries,
		statePath: statePath,
		rosterDir: rosterDir,
		state:     state,
		enqueue:   enqueue,
		now:       time.Now,
		inflight:  map[string]bool{},
		disabled:  loadErr != nil,
		loadErr:   loadErr,
	}
}

// SetOwningCoordinator resolves the layer that must receive a missing-artifact
// failure. Coordinator targets should resolve to themselves.
func (sc *Scheduler) SetOwningCoordinator(resolve func(string) string) {
	sc.owningCoordinator = resolve
}

// CatchUp runs one scheduling pass immediately — the restart catch-up for slots
// missed while the daemon was down (fires at most once per schedule with a late
// marker when appropriate).
func (sc *Scheduler) CatchUp() { sc.Tick() }

// Tick evaluates every schedule against the current wall clock and enqueues any
// due dispatches. Safe to call from multiple goroutines (e.g. the poll loop and
// the detector's ScheduleOnTick hook); last_fired prevents double-fire.
func (sc *Scheduler) Tick() {
	if len(sc.entries) == 0 || sc.enqueue == nil || sc.disabled {
		return
	}
	now := sc.now()
	sc.mu.Lock()
	jobs := make([]Job, 0)
	dirty := false
	for _, ent := range sc.entries {
		for occurrenceKey, current := range sc.state.Occurrences {
			if current.ScheduleName != ent.name || scheduleOccurrenceComplete(current) {
				continue
			}
			var changed bool
			jobs, changed = sc.reconcileOccurrence(jobs, occurrenceKey, ent, current, now)
			dirty = dirty || changed
		}
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
		artifact := resolveExpectedArtifact(sc.rosterDir, ent.expectedArtifact, occ)
		body = scheduleInstructionBody(body, artifact, ent.productionWindowRaw)
		occurrence := scheduleTimestamp(occ)
		log.Printf("flotilla watch: schedule %q → %s (occurrence %s)", ent.name, ent.to, occurrence)
		sc.state.LastFired[ent.name] = occurrence
		sc.state.Occurrences[scheduleOccurrenceKey(ent.name, occurrence)] = ScheduleOccurrenceState{
			ScheduleName: ent.name, Occurrence: occurrence, Target: ent.to,
			TriggeredAt:      scheduleTimestamp(now),
			ExpectedArtifact: artifact, ProductionWindow: ent.productionWindowRaw,
			Instruction: body,
		}
		key := scheduleInflightKey(ent.name, occurrence, "instruction")
		sc.inflight[key] = true
		jobs = append(jobs, scheduledInstructionJob(ent, occ, body))
		dirty = true
	}
	if dirty {
		if err := sc.state.Save(sc.statePath); err != nil {
			for _, job := range jobs {
				delete(sc.inflight, scheduleInflightKey(job.ScheduleName, job.ScheduleOccurrence, job.SchedulePhase))
			}
			sc.mu.Unlock()
			log.Printf("flotilla watch: schedule sidecar persist failed: %v (no scheduled delivery submitted)", err)
			return
		}
	}
	sc.mu.Unlock()
	for _, job := range jobs {
		sc.enqueue(job)
		sc.DeliveryEnqueued(job)
	}
}

func (sc *Scheduler) reconcileOccurrence(jobs []Job, occurrenceKey string, ent parsedSchedule, current ScheduleOccurrenceState, now time.Time) ([]Job, bool) {
	if current.DeliveryAttemptAt != "" {
		if current.DeliveryAttemptPhase == "escalation" {
			current.EscalationUncertainAt = scheduleTimestamp(now)
			current.DeliveryUncertainAt = scheduleTimestamp(now)
			current.LastDeliveryError = fmt.Sprintf("escalation outcome is uncertain after an interrupted attempt at %s; retrying the stable failure ID until owner confirmation", current.DeliveryAttemptAt)
			current.DeliveryAttemptAt = ""
			current.DeliveryAttemptPhase = ""
			sc.state.Occurrences[occurrenceKey] = current
			log.Printf("flotilla watch: schedule %q escalation outcome uncertain: %s", ent.name, current.LastDeliveryError)
			return sc.prepareEscalation(jobs, occurrenceKey, ent, current, now)
		}
		current.DeliveryUncertainAt = scheduleTimestamp(now)
		current.FailureAt = scheduleTimestamp(now)
		current.FailureReason = fmt.Sprintf("delivery outcome is uncertain after an interrupted attempt at %s; automatic replay refused to prevent duplicate ceremony delivery", current.DeliveryAttemptAt)
		current.DeliveryAttemptAt = ""
		current.DeliveryAttemptPhase = ""
		sc.state.Occurrences[occurrenceKey] = current
		log.Printf("flotilla watch: schedule %q FAILED CLOSED: %s", ent.name, current.FailureReason)
		return sc.prepareEscalation(jobs, occurrenceKey, ent, current, now)
	}
	if current.FailureAt != "" {
		return sc.prepareEscalation(jobs, occurrenceKey, ent, current, now)
	}
	if current.DeliveredAt == "" {
		key := scheduleInflightKey(ent.name, current.Occurrence, "instruction")
		if sc.inflight[key] {
			return jobs, false
		}
		occ, err := time.Parse(time.RFC3339, current.Occurrence)
		if err != nil {
			return jobs, false
		}
		body := current.Instruction
		instructionChanged := false
		if body == "" {
			body, err = resolveSchedulePrompt(sc.rosterDir, ent.prompt)
			if err != nil {
				log.Printf("flotilla watch: schedule %q pending prompt resolve failed: %v", ent.name, err)
				return jobs, false
			}
			if now.Sub(occ) > scheduleLateGrace {
				body = scheduleLatePrefix(ent.name, occ) + body
			}
			body = scheduleInstructionBody(body, current.ExpectedArtifact, current.ProductionWindow)
			current.Instruction = body
			instructionChanged = true
		}
		sc.state.Occurrences[occurrenceKey] = current
		sc.inflight[key] = true
		return append(jobs, scheduledInstructionJob(ent, occ, body)), instructionChanged
	}
	if current.ExpectedArtifact == "" {
		return jobs, false
	}
	deliveredAt, deliveredErr := time.Parse(time.RFC3339, current.DeliveredAt)
	deadline, deadlineErr := time.Parse(time.RFC3339, current.ArtifactDeadline)
	if deliveredErr == nil && deadlineErr == nil && artifactProducedWithin(current.ExpectedArtifact, deliveredAt, deadline) {
		current.ArtifactConfirmedAt = scheduleTimestamp(now)
		sc.state.Occurrences[occurrenceKey] = current
		log.Printf("flotilla watch: schedule %q artifact confirmed: %s", ent.name, current.ExpectedArtifact)
		return jobs, true
	}
	if deadlineErr != nil || now.Before(deadline) {
		return jobs, false
	}
	current.FailureAt = scheduleTimestamp(now)
	current.FailureReason = fmt.Sprintf("expected non-empty artifact %s was not produced by %s", current.ExpectedArtifact, deadline.Format(time.RFC3339))
	sc.state.Occurrences[occurrenceKey] = current
	log.Printf("flotilla watch: schedule %q FAILED: %s", ent.name, current.FailureReason)
	return sc.prepareEscalation(jobs, occurrenceKey, ent, current, now)
}

func (sc *Scheduler) prepareEscalation(jobs []Job, occurrenceKey string, ent parsedSchedule, current ScheduleOccurrenceState, now time.Time) ([]Job, bool) {
	if current.EscalatedAt != "" {
		return jobs, false
	}
	key := scheduleInflightKey(ent.name, current.Occurrence, "escalation")
	if sc.inflight[key] {
		return jobs, false
	}
	owner := current.Target
	if sc.owningCoordinator != nil {
		if resolved := sc.owningCoordinator(current.Target); resolved != "" {
			owner = resolved
		}
	}
	current.EscalationEnqueueIntentAt = scheduleTimestamp(now)
	sc.state.Occurrences[occurrenceKey] = current
	sc.inflight[key] = true
	failureID := scheduleOccurrenceKey(ent.name, current.Occurrence)
	message := fmt.Sprintf("[scheduled ceremony failure id=%s] %s occurrence %s failed: %s. Confirmed delivery: %s. Owning layer %s: repair the ceremony path and any expected artifact; this stable failure ID may repeat until confirmed and remains durable in the schedule sidecar.", failureID, ent.name, current.Occurrence, current.FailureReason, current.DeliveredAt, owner)
	job := Job{
		Agent: owner, Message: message, Kind: KindScheduled, DirectToOwner: true,
		ScheduleName: ent.name, ScheduleOccurrence: current.Occurrence, SchedulePhase: "escalation",
	}
	return append(jobs, job), true
}

// DeliveryConfirmed advances only the matching durable occurrence and phase.
func (sc *Scheduler) DeliveryConfirmed(job Job) {
	if job.Kind != KindScheduled || job.ScheduleName == "" || job.ScheduleOccurrence == "" {
		return
	}
	now := sc.now().UTC()
	sc.mu.Lock()
	defer sc.mu.Unlock()
	occurrenceKey := scheduleOccurrenceKey(job.ScheduleName, job.ScheduleOccurrence)
	current, ok := sc.state.Occurrences[occurrenceKey]
	if !ok {
		return
	}
	delete(sc.inflight, scheduleInflightKey(job.ScheduleName, job.ScheduleOccurrence, job.SchedulePhase))
	switch job.SchedulePhase {
	case "instruction":
		if current.DeliveredAt != "" {
			return
		}
		current.DeliveredAt = scheduleTimestamp(now)
		if current.ExpectedArtifact != "" {
			window, err := time.ParseDuration(current.ProductionWindow)
			if err != nil || window <= 0 {
				return
			}
			current.ArtifactDeadline = scheduleTimestamp(now.Add(window))
		}
	case "escalation":
		current.EscalatedAt = scheduleTimestamp(now)
	default:
		return
	}
	current.LastDeliveryError = ""
	current.LastAttemptFailedAt = ""
	current.DeliveryAttemptAt = ""
	current.DeliveryAttemptPhase = ""
	sc.state.Occurrences[occurrenceKey] = current
	if err := sc.state.Save(sc.statePath); err != nil {
		log.Printf("flotilla watch: schedule %q delivery confirmation persist failed: %v", job.ScheduleName, err)
	}
}

// DeliveryFailed releases the in-process claim but deliberately retains any
// pre-send attempt receipt. Post-mutation failures can be ambiguous
// (ErrUnconfirmed and post-paste panel failures), so the next tick must apply
// the instruction no-replay / escalation at-least-once policy.
func (sc *Scheduler) DeliveryFailed(job Job, reason string) {
	if job.Kind != KindScheduled || job.ScheduleName == "" || job.ScheduleOccurrence == "" {
		return
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	occurrenceKey := scheduleOccurrenceKey(job.ScheduleName, job.ScheduleOccurrence)
	current, ok := sc.state.Occurrences[occurrenceKey]
	if !ok {
		return
	}
	delete(sc.inflight, scheduleInflightKey(job.ScheduleName, job.ScheduleOccurrence, job.SchedulePhase))
	current.LastAttemptFailedAt = scheduleTimestamp(sc.now())
	current.LastDeliveryError = reason
	sc.state.Occurrences[occurrenceKey] = current
	if err := sc.state.Save(sc.statePath); err != nil {
		log.Printf("flotilla watch: schedule %q failed-attempt persist failed: %v", job.ScheduleName, err)
	}
}

// DeliveryAttemptStarted commits a receipt before touching the target surface.
// If the process dies after this point, restart refuses an automatic replay:
// the prior attempt may have landed, so retrying would risk a duplicate.
func (sc *Scheduler) DeliveryAttemptStarted(job Job) error {
	if job.Kind != KindScheduled || job.ScheduleName == "" || job.ScheduleOccurrence == "" {
		return nil
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	occurrenceKey := scheduleOccurrenceKey(job.ScheduleName, job.ScheduleOccurrence)
	current, ok := sc.state.Occurrences[occurrenceKey]
	if !ok {
		return fmt.Errorf("scheduled occurrence %s not found", occurrenceKey)
	}
	previous := current
	current.DeliveryAttemptAt = scheduleTimestamp(sc.now())
	current.DeliveryAttemptPhase = job.SchedulePhase
	sc.state.Occurrences[occurrenceKey] = current
	if err := sc.state.Save(sc.statePath); err != nil {
		sc.state.Occurrences[occurrenceKey] = previous
		return fmt.Errorf("persist delivery-attempt receipt: %w", err)
	}
	return nil
}

// DeliveryDeferred clears the pre-attempt receipt only after a typed busy or
// transient result proves that the target surface did not accept the prompt.
func (sc *Scheduler) DeliveryDeferred(job Job, reason string) error {
	if job.Kind != KindScheduled {
		return nil
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	occurrenceKey := scheduleOccurrenceKey(job.ScheduleName, job.ScheduleOccurrence)
	current, ok := sc.state.Occurrences[occurrenceKey]
	if !ok {
		return fmt.Errorf("scheduled occurrence %s not found", occurrenceKey)
	}
	previous := current
	current.DeliveryAttemptAt = ""
	current.DeliveryAttemptPhase = ""
	current.LastAttemptFailedAt = scheduleTimestamp(sc.now())
	current.LastDeliveryError = reason
	sc.state.Occurrences[occurrenceKey] = current
	if err := sc.state.Save(sc.statePath); err != nil {
		sc.state.Occurrences[occurrenceKey] = previous
		return fmt.Errorf("persist deferred delivery result: %w", err)
	}
	return nil
}

// DeliveryEnqueued records queue acceptance only after the enqueue call
// returns; trigger persistence deliberately precedes this transition.
func (sc *Scheduler) DeliveryEnqueued(job Job) {
	if job.Kind != KindScheduled {
		return
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	occurrenceKey := scheduleOccurrenceKey(job.ScheduleName, job.ScheduleOccurrence)
	current, ok := sc.state.Occurrences[occurrenceKey]
	if !ok {
		return
	}
	switch job.SchedulePhase {
	case "instruction":
		if current.EnqueuedAt != "" {
			return
		}
		current.EnqueuedAt = scheduleTimestamp(sc.now())
	case "escalation":
		if current.EscalationEnqueuedAt != "" {
			return
		}
		current.EscalationEnqueuedAt = scheduleTimestamp(sc.now())
	default:
		return
	}
	sc.state.Occurrences[occurrenceKey] = current
	if err := sc.state.Save(sc.statePath); err != nil {
		log.Printf("flotilla watch: schedule %q enqueue confirmation persist failed: %v", job.ScheduleName, err)
	}
}

func scheduledInstructionJob(ent parsedSchedule, occ time.Time, body string) Job {
	return Job{
		Agent: ent.to, Message: body, Kind: KindScheduled,
		ScheduleName: ent.name, ScheduleOccurrence: scheduleTimestamp(occ), SchedulePhase: "instruction",
	}
}

func scheduleInflightKey(name, occurrence, phase string) string {
	return name + "\x00" + occurrence + "\x00" + phase
}

func scheduleOccurrenceKey(name, occurrence string) string { return name + "@" + occurrence }

func scheduleTimestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func scheduleOccurrenceComplete(current ScheduleOccurrenceState) bool {
	if current.FailureAt != "" {
		return current.EscalatedAt != ""
	}
	if current.DeliveredAt == "" {
		return false
	}
	if current.ExpectedArtifact == "" {
		return true
	}
	return current.ArtifactConfirmedAt != ""
}

func artifactProducedWithin(path string, deliveredAt, deadline time.Time) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0 && st.ModTime().After(deliveredAt) && !st.ModTime().After(deadline)
}

func resolveExpectedArtifact(rosterDir, pattern string, occurrence time.Time) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}
	date := occurrence.Format("2006-01-02")
	pattern = strings.ReplaceAll(pattern, "<date>", date)
	pattern = strings.ReplaceAll(pattern, "{date}", date)
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(rosterDir, pattern)
	}
	return filepath.Clean(pattern)
}

func scheduleInstructionBody(body, artifact, window string) string {
	if artifact == "" {
		return body
	}
	return fmt.Sprintf("[scheduled ceremony] Expected non-empty artifact: %s\nProduction window after confirmed delivery: %s\n\n%s", artifact, window, body)
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
