package watch

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

const (
	pressureSelectedOne = `Approaching rate limits
Use gpt-5.4-mini?
› 1. Try later
  2. Keep current model
  3. Switch model`
	pressureSelectedTwo = `Approaching rate limits
Use gpt-5.4-mini?
  1. Try later
› 2. Keep current model
  3. Switch model`
	pressureSelectedThree = `Approaching rate limits
Use gpt-5.4-mini?
  1. Try later
  2. Keep current model
› 3. Switch model`
)

type pressureHarness struct {
	screens []string
	nav     []string
	enters  int
	events  []ThroughputPressureEvent
	now     time.Time
	ledger  string
}

func (h *pressureHarness) monitor(t *testing.T) *CodexPressureMonitor {
	t.Helper()
	m, err := NewCodexPressureMonitor(CodexPressureConfig{
		Seats:   []string{"codex-seat"},
		Ledger:  h.ledger,
		Now:     func() time.Time { return h.now },
		Resolve: func(string) (string, error) { return "f:0.1", nil },
		Capture: func(string) (string, bool, error) {
			if len(h.screens) == 0 {
				return "", false, fmt.Errorf("unexpected capture")
			}
			s := h.screens[0]
			h.screens = h.screens[1:]
			return s, false, nil
		},
		Assess:   func(string) surface.State { return surface.StateIdle },
		Lock:     func(string) (func(), error) { return func() {}, nil },
		Navigate: func(_ string, key string) error { h.nav = append(h.nav, key); return nil },
		Enter:    func(string) error { h.enters++; return nil },
		Append:   func(e ThroughputPressureEvent) error { h.events = append(h.events, e); return nil },
		Sleep:    func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCodexPressurePositiveNavigatesAndVerifiesOptionTwo(t *testing.T) {
	h := &pressureHarness{now: time.Date(2026, 8, 7, 20, 0, 0, 0, time.UTC), screens: []string{pressureSelectedOne, pressureSelectedTwo, "normal composer"}}
	h.monitor(t).Tick()
	if fmt.Sprint(h.nav) != "[Down]" {
		t.Fatalf("navigation = %v, want [Down]", h.nav)
	}
	if h.enters != 1 {
		t.Fatalf("Enter calls = %d, want 1", h.enters)
	}
	if len(h.events) != 2 || h.events[0].Event != "dialog_verified" || h.events[1].Event != "dialog_cleared" || !h.events[1].ProcessAlive || h.events[0].ID != h.events[1].ID {
		t.Fatalf("events = %#v, want durable pre-action event completed under one ID", h.events)
	}
}

func TestCodexPressureStaleScrollbackWithoutActiveMarkerIsIgnored(t *testing.T) {
	stale := `Earlier: Approaching rate limits
Use gpt-5.4-mini?
  1. Try later
  2. Keep current model
  3. Switch model
current composer`
	h := &pressureHarness{now: time.Now(), screens: []string{stale}}
	h.monitor(t).Tick()
	if len(h.nav) != 0 || h.enters != 0 || len(h.events) != 0 {
		t.Fatalf("stale scrollback acted on: %#v", h)
	}
}

func TestCodexPressureWrongSelectionAfterNavigationFailsClosed(t *testing.T) {
	h := &pressureHarness{now: time.Now(), screens: []string{pressureSelectedOne, pressureSelectedOne}}
	h.monitor(t).Tick()
	if fmt.Sprint(h.nav) != "[Down]" || h.enters != 0 || len(h.events) != 0 {
		t.Fatalf("wrong-selection result nav=%v enters=%d events=%v", h.nav, h.enters, h.events)
	}
}

func TestCodexPressureOptionThreeNeverSubmitsWithoutVerifiedOptionTwo(t *testing.T) {
	h := &pressureHarness{now: time.Now(), screens: []string{pressureSelectedThree, pressureSelectedThree}}
	h.monitor(t).Tick()
	if fmt.Sprint(h.nav) != "[Up]" {
		t.Fatalf("navigation = %v, want only [Up]", h.nav)
	}
	if h.enters != 0 || len(h.events) != 0 {
		t.Fatalf("option 3 was submitted: enters=%d events=%v", h.enters, h.events)
	}
}

func TestCodexPressureRecurrenceWithinThirtyMinutesTrips(t *testing.T) {
	now := time.Date(2026, 8, 7, 20, 20, 0, 0, time.UTC)
	h := &pressureHarness{now: now, screens: []string{pressureSelectedTwo, pressureSelectedTwo, "normal composer"}}
	m := h.monitor(t)
	m.lastClear["codex-seat"] = now.Add(-29 * time.Minute)
	m.Tick()
	if len(h.events) != 2 || !h.events[0].TripConditionMet || h.events[0].TripReason != "verified_recurrence_within_30_minutes" || h.events[1].Event != "dialog_cleared" {
		t.Fatalf("events = %#v, want durable recurrence trip before clear", h.events)
	}
}

func TestCodexPressureCopyModeFailsClosed(t *testing.T) {
	var enters int
	m, err := NewCodexPressureMonitor(CodexPressureConfig{
		Seats: []string{"codex-seat"}, Now: time.Now,
		Resolve:  func(string) (string, error) { return "f:0.1", nil },
		Capture:  func(string) (string, bool, error) { return pressureSelectedTwo, true, nil },
		Assess:   func(string) surface.State { return surface.StateIdle },
		Lock:     func(string) (func(), error) { return func() {}, nil },
		Navigate: func(string, string) error { return nil }, Enter: func(string) error { enters++; return nil },
		Append: func(ThroughputPressureEvent) error { t.Fatal("unexpected event"); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Tick()
	if enters != 0 {
		t.Fatalf("copy-mode Enter calls = %d, want 0", enters)
	}
}

func TestCodexPressureSeatErrorTripsOncePerEpisode(t *testing.T) {
	var events []ThroughputPressureEvent
	state := surface.StateErrored
	m, err := NewCodexPressureMonitor(CodexPressureConfig{
		Seats: []string{"codex-seat"}, Now: time.Now,
		Resolve: func(string) (string, error) { return "f:0.1", nil },
		Assess:  func(string) surface.State { return state },
		Lock:    func(string) (func(), error) { return func() {}, nil },
		Append:  func(e ThroughputPressureEvent) error { events = append(events, e); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Tick()
	m.Tick()
	if len(events) != 1 || events[0].TripReason != "seat_errored" || !events[0].TripConditionMet {
		t.Fatalf("events = %#v", events)
	}
}

func TestAppendThroughputPressureEventCarriesHarvesterContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex-throughput-events.json")
	at := time.Date(2026, 8, 7, 20, 20, 0, 0, time.UTC)
	verified := ThroughputPressureEvent{ID: "one", DetectedAt: at, Seat: "codex-seat", Event: "dialog_verified", SelectedOption: 2, SelectedLabel: "Keep current model"}
	if err := AppendThroughputPressureEvent(path, verified); err != nil {
		t.Fatal(err)
	}
	verified.Event = "dialog_cleared"
	verified.ClearedAt = at.Add(time.Second)
	verified.DialogGone = true
	verified.ProcessAlive = true
	if err := AppendThroughputPressureEvent(path, verified); err != nil {
		t.Fatal(err)
	}
	store, err := readPressureStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.SchemaVersion != 1 || store.Axis != ThroughputPressureAxis || store.EventCount != 1 || len(store.Events) != 1 || store.Events[0].Class != ThroughputPressureWarn || store.Events[0].ClearedAt.IsZero() {
		t.Fatalf("harvester contract = %#v", store)
	}
}

func TestCodexPressureDuplicateOptionTwoIsAmbiguous(t *testing.T) {
	screen := pressureSelectedTwo + "\n  2. Keep current model"
	if selected, live := classifyPressureDialog(screen); live {
		t.Fatalf("duplicate option 2 classified live with selection %d", selected)
	}
}

func TestCodexPressureModeChangeBeforeKeyFailsClosed(t *testing.T) {
	h := &pressureHarness{now: time.Now(), screens: []string{pressureSelectedOne}}
	m := h.monitor(t)
	m.cfg.InMode = func(string) (bool, error) { return true, nil }
	m.Tick()
	if len(h.nav) != 0 || h.enters != 0 || len(h.events) != 0 {
		t.Fatalf("mode transition acted on: nav=%v enters=%d events=%v", h.nav, h.enters, h.events)
	}
}

func TestCodexPressureModeChangeDuringDurableWriteBlocksEnter(t *testing.T) {
	h := &pressureHarness{now: time.Now(), screens: []string{pressureSelectedTwo, pressureSelectedTwo}}
	m := h.monitor(t)
	checks := 0
	m.cfg.InMode = func(string) (bool, error) {
		checks++
		return checks == 2, nil
	}
	m.Tick()
	if h.enters != 0 {
		t.Fatalf("Enter calls = %d after mode changed during durable write", h.enters)
	}
	if len(h.events) != 1 || h.events[0].Event != "dialog_verified" {
		t.Fatalf("durable sensor evidence = %#v, want one verified warning", h.events)
	}
}

func TestCodexPressureRetriesFailedClearCompletion(t *testing.T) {
	h := &pressureHarness{now: time.Now(), screens: []string{pressureSelectedTwo, pressureSelectedTwo, "normal composer"}}
	m := h.monitor(t)
	calls := 0
	m.cfg.Append = func(e ThroughputPressureEvent) error {
		calls++
		if calls == 2 {
			return fmt.Errorf("disk transient")
		}
		return nil
	}
	m.Tick()
	if len(m.pending) != 1 {
		t.Fatalf("pending completions = %d, want 1", len(m.pending))
	}
	m.Tick()
	if calls < 3 || len(m.pending) != 0 {
		t.Fatalf("retry calls=%d pending=%d", calls, len(m.pending))
	}
}

func TestCodexPressureRequiresDurableWarningBeforeEnter(t *testing.T) {
	h := &pressureHarness{now: time.Now(), screens: []string{pressureSelectedTwo, pressureSelectedTwo}}
	m := h.monitor(t)
	m.cfg.Append = func(ThroughputPressureEvent) error { return fmt.Errorf("disk unavailable") }
	m.Tick()
	if h.enters != 0 {
		t.Fatalf("Enter calls = %d despite failed durable precondition", h.enters)
	}
}

func TestCodexPressureRetriesErrorTripUntilDurable(t *testing.T) {
	state := surface.StateErrored
	calls := 0
	m, err := NewCodexPressureMonitor(CodexPressureConfig{
		Seats: []string{"codex-seat"}, Now: time.Now,
		Resolve: func(string) (string, error) { return "f:0.1", nil },
		Assess:  func(string) surface.State { return state }, Lock: func(string) (func(), error) { return func() {}, nil },
		Append: func(ThroughputPressureEvent) error {
			calls++
			if calls == 1 {
				return fmt.Errorf("disk transient")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Tick()
	m.Tick()
	m.Tick()
	if calls != 2 {
		t.Fatalf("append calls = %d, want failed attempt + retry only", calls)
	}
}

func TestCodexPressureReloadsPriorClearForRecurrence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-throughput-events.json")
	now := time.Date(2026, 8, 7, 20, 20, 0, 0, time.UTC)
	if err := AppendThroughputPressureEvent(path, ThroughputPressureEvent{ID: "prior", DetectedAt: now.Add(-20 * time.Minute), ClearedAt: now.Add(-20 * time.Minute), Seat: "codex-seat", Event: "dialog_cleared"}); err != nil {
		t.Fatal(err)
	}
	h := &pressureHarness{now: now, ledger: path, screens: []string{pressureSelectedTwo, pressureSelectedTwo, "normal composer"}}
	m := h.monitor(t)
	m.Tick()
	if len(h.events) < 1 || !h.events[0].TripConditionMet {
		t.Fatalf("restart did not retain recurrence: %#v", h.events)
	}
}
