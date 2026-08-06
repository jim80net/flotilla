package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestSnapshotRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "detector-state.json")
	observed := time.Date(2026, 8, 6, 16, 8, 0, 0, time.UTC)
	want := Snapshot{
		DeskStates: map[string]surface.State{
			"xo":      surface.StateIdle,
			"backend": surface.StateWorking,
		},
		DeskObservations: map[string]DeskStateObservation{
			"xo":      {State: surface.StateIdle, ObservedAt: observed, Reason: "surface-transition:working->idle"},
			"backend": {State: surface.StateWorking, ObservedAt: observed.Add(-time.Second), Reason: "surface-refresh:working"},
		},
		SignalHash: "abc123",
		XOSettled:  true,
		Usage: map[string]UsageObservation{
			"backend": {RemainingPercent: 8, Window: "weekly", Scope: "account-side", ObservedAt: time.Date(2026, 7, 13, 6, 0, 0, 0, time.UTC)},
		},
	}
	if err := want.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok := LoadSnapshot(p)
	if !ok {
		t.Fatal("LoadSnapshot ok=false after a successful Save")
	}
	if got.SignalHash != want.SignalHash || got.XOSettled != want.XOSettled {
		t.Errorf("round-trip scalar mismatch: got %+v want %+v", got, want)
	}
	if got.DeskStates["xo"] != surface.StateIdle || got.DeskStates["backend"] != surface.StateWorking {
		t.Errorf("round-trip desk states mismatch: %+v", got.DeskStates)
	}
	if got.DeskObservations["xo"].Reason != "surface-transition:working->idle" || !got.DeskObservations["backend"].ObservedAt.Equal(observed.Add(-time.Second)) {
		t.Errorf("round-trip desk observations mismatch: %+v", got.DeskObservations)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"state":3`, `"observed_at":"2026-08-06T16:08:00Z"`, `"reason":`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("persisted snapshot missing structured desk field %s: %s", want, raw)
		}
	}
	if got.Usage["backend"].RemainingPercent != 8 || got.Usage["backend"].Window != "weekly" {
		t.Errorf("round-trip usage mismatch: %+v", got.Usage)
	}
}

func TestLoadSnapshotLegacyTrackerHashShape(t *testing.T) {
	// Back-compat: this PR renamed the on-disk field tracker_hash → signal_hash. A
	// daemon upgraded over a snapshot written by the OLD binary must load safely. We
	// CONSTRUCT the legacy on-disk shape (the field is "tracker_hash", there is no
	// "signal_hash") — NOT a round-trip of the current struct, per the
	// backward-compat-test-builds-old-shape discipline.
	p := filepath.Join(t.TempDir(), "legacy.json")
	legacy := `{"desk_states":{"backend":3},"tracker_hash":"DEADBEEF","xo_settled":true}`
	if err := os.WriteFile(p, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy snapshot: %v", err)
	}
	legacyAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p, legacyAt, legacyAt); err != nil {
		t.Fatalf("date legacy snapshot: %v", err)
	}
	got, ok := LoadSnapshot(p)
	if !ok {
		t.Fatal("legacy snapshot must load (ok=true), not cold-start")
	}
	// The unknown legacy tracker_hash is ignored → SignalHash empty → non-material on
	// the first post-upgrade tick (no spurious wake from a stale tracker value).
	if got.SignalHash != "" {
		t.Errorf("legacy tracker_hash must NOT populate SignalHash; got %q", got.SignalHash)
	}
	// The fields that DID survive the rename are preserved.
	if !got.XOSettled {
		t.Error("xo_settled must survive the load")
	}
	if got.DeskStates["backend"] != surface.StateIdle { // 3 == StateIdle
		t.Errorf("desk_states must survive the load, got %+v", got.DeskStates)
	}
	if observation := got.DeskObservations["backend"]; observation.Reason != "legacy-snapshot" || !observation.ObservedAt.Equal(legacyAt) {
		t.Errorf("legacy desk state needs honest file-age provenance, got %+v", observation)
	}
}

func TestObserveDeskStatesRefreshesAndTransitions(t *testing.T) {
	priorAt := time.Date(2026, 8, 6, 16, 0, 0, 0, time.UTC)
	now := priorAt.Add(8 * time.Minute)
	prev := Snapshot{
		DeskStates: map[string]surface.State{"alpha": surface.StateAwaitingInput, "beta": surface.StateAwaitingInput},
		DeskObservations: map[string]DeskStateObservation{
			"alpha": {State: surface.StateAwaitingInput, ObservedAt: priorAt, Reason: "surface-refresh:awaiting-input"},
			"beta":  {State: surface.StateAwaitingInput, ObservedAt: priorAt, Reason: "surface-refresh:awaiting-input"},
		},
	}
	states := map[string]surface.State{"alpha": surface.StateIdle, "beta": surface.StateAwaitingInput}
	observations := ObserveDeskStates(prev, states, now)

	if got := observations["alpha"]; got.Reason != "surface-transition:awaiting-input->idle" || !got.ObservedAt.Equal(now) {
		t.Errorf("cleared premise observation = %+v", got)
	}
	if got := observations["beta"]; got.Reason != "surface-refresh:awaiting-input" || !got.ObservedAt.Equal(now) {
		t.Errorf("steady blocked refresh observation = %+v", got)
	}
}

func TestLoadSnapshotMissingIsColdStart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.json")
	if _, ok := LoadSnapshot(p); ok {
		t.Error("missing snapshot must return ok=false (cold-start / treat-as-all-changed)")
	}
}

func TestLoadSnapshotCorruptIsColdStart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ok := LoadSnapshot(p)
	if ok {
		t.Error("corrupt snapshot must return ok=false (cold-start), not a parsed value")
	}
	if s.DeskStates == nil {
		// A zero Snapshot is fine; we just must not panic and must be usable.
		s.DeskStates = map[string]surface.State{}
	}
}

func TestLoadSnapshotNilMapNormalized(t *testing.T) {
	// A valid snapshot whose desk_states is JSON null must load with a non-nil
	// map so the detector can write into it without a nil-map panic.
	p := filepath.Join(t.TempDir(), "nullmap.json")
	if err := os.WriteFile(p, []byte(`{"desk_states":null,"signal_hash":"x","xo_settled":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ok := LoadSnapshot(p)
	if !ok {
		t.Fatal("a valid (if null-map) snapshot should load ok")
	}
	if s.DeskStates == nil {
		t.Error("DeskStates must be normalized to a non-nil map on load")
	}
}

func TestSnapshotSaveIsAtomicNoTempLeft(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "detector-state.json")
	s := Snapshot{DeskStates: map[string]surface.State{"x": surface.StateIdle}}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("atomic Save left a temp file behind: %q", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1 (only the snapshot)", len(entries))
	}
}

func TestSnapshotSaveWriteErrorDoesNotPanic(t *testing.T) {
	// Target a path whose parent directory does not exist → CreateTemp fails →
	// Save returns an error (the detector degrades on this; it must never panic).
	p := filepath.Join(t.TempDir(), "no-such-dir", "state.json")
	err := Snapshot{DeskStates: map[string]surface.State{}}.Save(p)
	if err == nil {
		t.Error("Save into a missing directory should return an error, not silently succeed")
	}
}
