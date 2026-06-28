package watch

import (
	"os"
	"testing"
)

// TestSettledMarkerSet covers #183 group 2: the per-agent settle marker. Each desk the recursive
// heartbeat re-engages signals "nothing to advance" by touching <dir>/flotilla-<agent>-settled and
// replying idle; the detector Consume()s it to suppress further heartbeats to THAT desk until it is
// re-armed. The markers are per-agent path-scoped, so one desk's settle never consumes another's
// (or the XO's), and an absent/unconfigured marker fails toward NOT-settled (keeps the desk
// responsive) — the same safe direction as the XO marker.
func TestSettledMarkerSet(t *testing.T) {
	dir := t.TempDir()
	s := NewSettledMarkerSet(dir)

	if s.Consume("backend") {
		t.Error("absent marker should be not-settled")
	}

	// present → consumed once, then removed (each settle is a fresh, explicit signal)
	if err := os.WriteFile(s.Path("backend"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if !s.Consume("backend") {
		t.Error("present marker should consume true")
	}
	if s.Consume("backend") {
		t.Error("marker should be removed after consume")
	}

	// per-agent isolation: backend's marker must not satisfy frontend
	if err := os.WriteFile(s.Path("backend"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if s.Consume("frontend") {
		t.Error("frontend must not consume backend's marker")
	}
	if !s.Consume("backend") {
		t.Error("backend's own marker should still consume true")
	}

	// unconfigured (empty dir) or empty agent → false
	if (&SettledMarkerSet{}).Consume("backend") {
		t.Error("empty dir → not settled")
	}
	if s.Consume("") {
		t.Error("empty agent → not settled")
	}

	// paths are per-agent distinct and namespaced
	if s.Path("backend") == s.Path("frontend") {
		t.Error("per-agent settle paths must differ")
	}
	if (&SettledMarkerSet{}).Path("backend") != "" {
		t.Error("unconfigured set should resolve an empty path")
	}
}
