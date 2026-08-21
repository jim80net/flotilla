package dispatch

import (
	"strings"
	"testing"
	"time"
)

func TestLookupNonceNegativePrintsConsumedRetainedWindowBounds(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 8, 19, 5, 31, 0, 0, time.UTC)
	end := start.Add(54 * time.Hour)
	registry := NewRegistry(dir)
	for i, at := range []time.Time{end, start} {
		if _, err := registry.Consume(ConsumedEntry{
			Nonce:       "flotilla-dispatch-retained-" + string(rune('a'+i)),
			PayloadHash: "hash",
			ConsumedAt:  at,
			Reason:      ReasonDurableAck,
		}); err != nil {
			t.Fatal(err)
		}
	}

	status := LookupNonce(dir, "flotilla-dispatch-unknown", end.Add(time.Hour))
	if status.Disposition != DispositionUnknown {
		t.Fatalf("disposition = %s, want unknown", status.Disposition)
	}
	want := "consumed-retained-window=[2026-08-19T05:31:00Z,2026-08-21T11:31:00Z] entries=2 capacity=2048"
	if !strings.Contains(FormatStatus(status), want) {
		t.Fatalf("status = %q, want %q", FormatStatus(status), want)
	}
}

func TestLookupNonceNegativePrintsEmptyRetainedWindow(t *testing.T) {
	status := LookupNonce(t.TempDir(), "flotilla-dispatch-unknown", time.Now())
	want := "consumed-retained-window=empty entries=0 capacity=2048"
	if !strings.Contains(FormatStatus(status), want) {
		t.Fatalf("status = %q, want %q", FormatStatus(status), want)
	}
}
