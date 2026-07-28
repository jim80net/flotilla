package harnessquality

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendLoadAndAggregate(t *testing.T) {
	dir := t.TempDir()
	base := Event{Seat: "builder", Surface: "grok", Model: "grok-4", HarnessVersion: "1.2.3", FlotillaVersion: "0.0.1", WorkClass: WorkStrategic}
	for _, event := range []Event{
		withEvent(base, KindGate, OutcomeBounced, 1, 0),
		withEvent(base, KindGate, OutcomePassed, 0, 0),
		withEvent(base, KindCompletion, OutcomeMerged, 0, 1),
	} {
		if _, err := Append(dir, event); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(LedgerPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("ledger mode = %o, want 600", info.Mode().Perm())
	}
	events, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	summary := BuildSummary(events, time.Date(2026, 7, 18, 20, 0, 0, 0, time.UTC))
	if summary.TotalEvents != 3 || summary.BounceRatePercent != 50 || summary.ReworkRatePercent != 100 || summary.TaggingCoveragePercent != 100 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(summary.Groups) != 1 || len(summary.Groups[0].HarnessVersions) != 1 || summary.Groups[0].TotalBounces != 1 {
		t.Fatalf("groups = %+v", summary.Groups)
	}
}

func TestContextRoundTripAndMalformedLedgerFailsClosed(t *testing.T) {
	dir := t.TempDir()
	ctx := Context{Seat: "builder", WorkClass: WorkKTLO, WorkRef: "repo#1", UpdatedAt: "2026-07-18T20:00:00Z"}
	if err := WriteContext(dir, ctx); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadContext(dir, "builder")
	if err != nil || !ok || got.WorkClass != WorkKTLO {
		t.Fatalf("ReadContext = %+v, %v, %v", got, ok, err)
	}
	if err := os.WriteFile(filepath.Join(dir, LedgerName), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if summary := LoadSummary(dir, time.Now()); summary.State != "unavailable" || summary.Diagnostic != "quality_ledger_invalid" {
		t.Fatalf("malformed summary = %+v", summary)
	}
}

func TestValidationRejectsInventedOrIncoherentFacts(t *testing.T) {
	base := Event{Schema: EventSchema, ID: "hq-1", TS: "2026-07-18T20:00:00Z", Seat: "builder", Surface: "codex", Model: "unknown", FlotillaVersion: "0.0.1", WorkClass: WorkUnclassified}
	tests := []Event{
		withEvent(base, KindGate, OutcomeBounced, 0, 0),
		withEvent(base, KindCompletion, OutcomePassed, 0, 0),
	}
	for _, event := range tests {
		if err := event.Validate(); err == nil {
			t.Fatalf("Validate(%+v) = nil", event)
		}
	}
}

func withEvent(base Event, kind EventKind, outcome Outcome, bounces, rework int) Event {
	base.Kind, base.Outcome = kind, outcome
	base.BounceCount, base.ReworkCount = bounces, rework
	return base
}
