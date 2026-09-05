package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

const (
	ThroughputPressureAxis   = "short_window_request_throughput"
	ThroughputPressureWarn   = "THROUGHPUT_PRESSURE_WARN"
	pressureRecurrenceWindow = 30 * time.Minute
)

var pressureSelectionRE = regexp.MustCompile(`^›\s*([0-9]+)\.\s*(.+?)\s*$`)

type ThroughputPressureEvent struct {
	ID                        string    `json:"id"`
	Class                     string    `json:"class"`
	Axis                      string    `json:"axis"`
	Seat                      string    `json:"seat"`
	PaneID                    string    `json:"pane_id,omitempty"`
	Event                     string    `json:"event"`
	DetectedAt                time.Time `json:"detected_at"`
	ClearedAt                 time.Time `json:"cleared_at,omitempty"`
	SelectedOption            int       `json:"selected_option,omitempty"`
	SelectedLabel             string    `json:"selected_label,omitempty"`
	PreEnterSelectionMarker   string    `json:"pre_enter_active_selection_marker,omitempty"`
	PreEnterSelectionProven   bool      `json:"pre_enter_selection_proven,omitempty"`
	DialogGone                bool      `json:"dialog_gone,omitempty"`
	ProcessAlive              bool      `json:"process_alive,omitempty"`
	RecurrenceWithin30Minutes bool      `json:"recurrence_within_30_minutes,omitempty"`
	TripConditionMet          bool      `json:"trip_condition_met,omitempty"`
	TripReason                string    `json:"trip_reason,omitempty"`
}

type throughputPressureStore struct {
	SchemaVersion int                       `json:"schema_version"`
	Axis          string                    `json:"axis"`
	EventCount    int                       `json:"event_count"`
	Events        []ThroughputPressureEvent `json:"events"`
	UpdatedAt     time.Time                 `json:"updated_at,omitempty"`
}

// AppendThroughputPressureEvent durably upserts one event into the exact JSON
// store consumed by telemetry's runtime-pressure harvester. A detected event is
// written before Enter and completed under the same ID after recovery.
func AppendThroughputPressureEvent(path string, event ThroughputPressureEvent) error {
	if event.Seat == "" || event.Event == "" || event.DetectedAt.IsZero() {
		return fmt.Errorf("throughput pressure event missing seat, event, or timestamp")
	}
	event.Axis = ThroughputPressureAxis
	event.Class = ThroughputPressureWarn
	if event.ID == "" {
		event.ID = fmt.Sprintf("%s-%s-%d", event.Seat, event.Event, event.DetectedAt.UnixNano())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck -- close also unlocks

	store, err := readPressureStore(path)
	if err != nil {
		return err
	}
	replaced := false
	for i := range store.Events {
		if store.Events[i].ID == event.ID {
			store.Events[i] = event
			replaced = true
			break
		}
	}
	if !replaced {
		store.Events = append(store.Events, event)
	}
	store.SchemaVersion = 1
	store.Axis = ThroughputPressureAxis
	store.EventCount = len(store.Events)
	store.UpdatedAt = event.DetectedAt
	if !event.ClearedAt.IsZero() {
		store.UpdatedAt = event.ClearedAt
	}
	body, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".codex-throughput-events-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(append(body, '\n'))
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err == nil {
		if dir, openErr := os.Open(filepath.Dir(path)); openErr == nil {
			err = dir.Sync()
			_ = dir.Close()
		}
	}
	return err
}

func readPressureStore(path string) (throughputPressureStore, error) {
	store := throughputPressureStore{SchemaVersion: 1, Axis: ThroughputPressureAxis, Events: []ThroughputPressureEvent{}}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if err := json.Unmarshal(body, &store); err != nil {
		return store, fmt.Errorf("refusing to overwrite unreadable throughput event store: %w", err)
	}
	return store, nil
}

func loadPressureClears(path string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	if path == "" {
		return out, nil
	}
	store, err := readPressureStore(path)
	if err != nil {
		return nil, err
	}
	for _, e := range store.Events {
		if !e.ClearedAt.IsZero() && e.ClearedAt.After(out[e.Seat]) {
			out[e.Seat] = e.ClearedAt
		}
	}
	return out, nil
}

type CodexPressureConfig struct {
	Seats    []string
	Ledger   string
	Now      func() time.Time
	Resolve  func(string) (string, error)
	Capture  func(string) (screen string, inMode bool, err error)
	InMode   func(string) (bool, error)
	Assess   func(string) surface.State
	Lock     func(string) (release func(), err error)
	Navigate func(string, string) error
	Enter    func(string) error
	Append   func(ThroughputPressureEvent) error
	Sleep    func(time.Duration)
	Logf     func(string, ...any)
}

type CodexPressureMonitor struct {
	cfg       CodexPressureConfig
	mu        sync.Mutex
	running   bool
	lastClear map[string]time.Time
	errorOpen map[string]bool
	pending   map[string]ThroughputPressureEvent
}

func NewCodexPressureMonitor(cfg CodexPressureConfig) (*CodexPressureMonitor, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.InMode == nil {
		cfg.InMode = func(string) (bool, error) { return false, nil }
	}
	if cfg.Append == nil {
		cfg.Append = func(e ThroughputPressureEvent) error { return AppendThroughputPressureEvent(cfg.Ledger, e) }
	}
	clears, err := loadPressureClears(cfg.Ledger)
	if err != nil {
		return nil, err
	}
	return &CodexPressureMonitor{cfg: cfg, lastClear: clears, errorOpen: map[string]bool{}, pending: map[string]ThroughputPressureEvent{}}, nil
}

// Tick is a recurring watch-level scan. Overlapping asynchronous detector ticks
// coalesce; the next regular tick retries anything that was not safely handled.
func (m *CodexPressureMonitor) Tick() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.running = false; m.mu.Unlock() }()
	m.flushPending()
	for _, seat := range m.cfg.Seats {
		m.scanSeat(seat)
	}
}

func (m *CodexPressureMonitor) flushPending() {
	m.mu.Lock()
	pending := make([]ThroughputPressureEvent, 0, len(m.pending))
	for _, event := range m.pending {
		pending = append(pending, event)
	}
	m.mu.Unlock()
	for _, event := range pending {
		if err := m.cfg.Append(event); err != nil {
			m.cfg.Logf("codex pressure: retry durable completion %s: %v", event.ID, err)
			continue
		}
		m.mu.Lock()
		delete(m.pending, event.ID)
		m.mu.Unlock()
	}
}

func (m *CodexPressureMonitor) scanSeat(seat string) {
	pane, err := m.cfg.Resolve(seat)
	if err != nil {
		m.tripError(seat, "seat_resolve_error")
		return
	}
	state := m.cfg.Assess(pane)
	if state == surface.StateErrored || state == surface.StateShell || state == surface.StateUnknown {
		m.tripError(seat, "seat_"+strings.ToLower(state.String()))
		return
	}
	m.mu.Lock()
	m.errorOpen[seat] = false
	m.mu.Unlock()

	release, err := m.cfg.Lock(pane)
	if err != nil {
		m.cfg.Logf("codex pressure: %s lock: %v", seat, err)
		return
	}
	defer release()
	screen, inMode, err := m.cfg.Capture(pane)
	if err != nil || inMode {
		return
	}
	selected, live := classifyPressureDialog(screen)
	if !live {
		return
	}

	switch selected {
	case 1:
		if blocked, checkErr := m.cfg.InMode(pane); checkErr != nil || blocked {
			return
		}
		err = m.cfg.Navigate(pane, "Down")
	case 2:
		// Already on the only permitted answer.
	case 3:
		if blocked, checkErr := m.cfg.InMode(pane); checkErr != nil || blocked {
			return
		}
		err = m.cfg.Navigate(pane, "Up")
	default:
		return
	}
	if err != nil {
		return
	}
	if selected != 2 {
		m.cfg.Sleep(100 * time.Millisecond)
	}
	screen, inMode, err = m.cfg.Capture(pane)
	if err != nil || inMode || !selectedOptionTwoExactly(screen) {
		return
	}
	if blocked, checkErr := m.cfg.InMode(pane); checkErr != nil || blocked {
		return
	}
	detectedAt := m.cfg.Now().UTC()
	m.mu.Lock()
	prior := m.lastClear[seat]
	m.mu.Unlock()
	recurrence := !prior.IsZero() && detectedAt.Sub(prior) >= 0 && detectedAt.Sub(prior) <= pressureRecurrenceWindow
	event := ThroughputPressureEvent{
		ID: fmt.Sprintf("%s-dialog-%d", seat, detectedAt.UnixNano()), Class: ThroughputPressureWarn,
		Axis: ThroughputPressureAxis, Seat: seat, PaneID: pane, Event: "dialog_verified",
		DetectedAt: detectedAt, SelectedOption: 2, SelectedLabel: "Keep current model",
		PreEnterSelectionMarker: "› 2. Keep current model", PreEnterSelectionProven: true,
		RecurrenceWithin30Minutes: recurrence, TripConditionMet: recurrence,
	}
	if recurrence {
		event.TripReason = "verified_recurrence_within_30_minutes"
	}
	// Durability is a PRECONDITION for mutation: if the harvester store cannot
	// record this verified live sensor, Enter is forbidden and the dialog remains.
	if err := m.cfg.Append(event); err != nil {
		m.cfg.Logf("codex pressure: %s pre-action durable event: %v", seat, err)
		return
	}
	// The durable fsync/rename above can take long enough for tmux mode to
	// change. Recheck at the final key boundary; Enter in copy mode is forbidden.
	if blocked, checkErr := m.cfg.InMode(pane); checkErr != nil || blocked {
		return
	}
	if err := m.cfg.Enter(pane); err != nil {
		return
	}

	for i := 0; i < 5; i++ {
		m.cfg.Sleep(100 * time.Millisecond)
		screen, inMode, err = m.cfg.Capture(pane)
		alive := m.cfg.Assess(pane)
		if alive == surface.StateErrored {
			m.tripError(seat, "seat_errored")
			return
		}
		if err == nil && !inMode && !pressureDialogVisible(screen) && alive != surface.StateShell && alive != surface.StateUnknown {
			m.recordClear(event)
			return
		}
	}
}

func classifyPressureDialog(screen string) (int, bool) {
	lines := strings.Split(screen, "\n")
	matchedBlocks := 0
	matchedSelection := 0
	for start, raw := range lines {
		if !strings.Contains(raw, "Approaching rate limits") {
			continue
		}
		end := start + 10
		if end > len(lines) {
			end = len(lines)
		}
		selected := 0
		markers := 0
		hasPrompt := false
		optionTwoRows := 0
		for _, blockRaw := range lines[start:end] {
			line := strings.TrimSpace(blockRaw)
			if strings.Contains(line, "gpt-5.4-mini") {
				hasPrompt = true
			}
			if line == "2. Keep current model" || line == "› 2. Keep current model" {
				optionTwoRows++
			}
			if match := pressureSelectionRE.FindStringSubmatch(line); match != nil {
				markers++
				fmt.Sscanf(match[1], "%d", &selected)
			}
		}
		if hasPrompt && optionTwoRows == 1 && markers == 1 {
			matchedBlocks++
			matchedSelection = selected
		}
	}
	return matchedSelection, matchedBlocks == 1
}

func pressureDialogVisible(screen string) bool {
	return strings.Contains(screen, "Approaching rate limits") && strings.Contains(screen, "gpt-5.4-mini")
}

func selectedOptionTwoExactly(screen string) bool {
	selected, live := classifyPressureDialog(screen)
	if !live || selected != 2 {
		return false
	}
	for _, raw := range strings.Split(screen, "\n") {
		if strings.TrimSpace(raw) == "› 2. Keep current model" {
			return true
		}
	}
	return false
}

func (m *CodexPressureMonitor) recordClear(event ThroughputPressureEvent) {
	now := m.cfg.Now().UTC()
	event.Event = "dialog_cleared"
	event.ClearedAt = now
	event.DialogGone = true
	event.ProcessAlive = true
	if err := m.cfg.Append(event); err != nil {
		m.cfg.Logf("codex pressure: %s durable clear completion: %v (verified event remains durable)", event.Seat, err)
		m.mu.Lock()
		m.pending[event.ID] = event
		m.lastClear[event.Seat] = now
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	m.lastClear[event.Seat] = now
	m.mu.Unlock()
}

func (m *CodexPressureMonitor) tripError(seat, reason string) {
	m.mu.Lock()
	if m.errorOpen[seat] {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	now := m.cfg.Now().UTC()
	if err := m.cfg.Append(ThroughputPressureEvent{ID: fmt.Sprintf("%s-error-%d", seat, now.UnixNano()), DetectedAt: now, Seat: seat, Event: "trip", TripConditionMet: true, TripReason: reason}); err != nil {
		m.cfg.Logf("codex pressure: %s durable trip event: %v", seat, err)
		return
	}
	m.mu.Lock()
	m.errorOpen[seat] = true
	m.mu.Unlock()
}
