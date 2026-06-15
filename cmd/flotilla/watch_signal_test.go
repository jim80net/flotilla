package main

import (
	"os"
	"path/filepath"
	"testing"
)

// contentHasher is the external-signal wake source for the change-detector. It must
// (1) hash a present file's content, (2) report a DIFFERENT hash when the content
// changes (so a real external signal wakes the XO exactly once), and (3) fail SAFE —
// an absent or unreadable file reports ok=false so the detector carries the prior
// hash forward and treats it as unchanged (no wake-storm), the same posture as the
// snapshot/marker reads.
func TestContentHasherFailSafe(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "signal")
	h := contentHasher(p)

	// Absent → ok=false (no signal), never a spurious hash.
	if got, ok := h(); ok || got != "" {
		t.Fatalf("absent signal file: got (%q,%v), want (\"\",false)", got, ok)
	}

	if err := os.WriteFile(p, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	h1, ok := h()
	if !ok || h1 == "" {
		t.Fatalf("present file: got (%q,%v), want a hash + ok=true", h1, ok)
	}

	// Same content → same hash (stable; not material).
	if h1b, ok := h(); !ok || h1b != h1 {
		t.Errorf("stable content changed hash: %q vs %q", h1, h1b)
	}

	// Changed content → different hash (a material external signal).
	if err := os.WriteFile(p, []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h2, ok := h(); !ok || h2 == h1 {
		t.Errorf("changed content did not change hash: %q stayed %q", h2, h1)
	}
}

// A directory at the signal path is unreadable-as-a-file → must fail safe (ok=false),
// never panic or wake-storm.
func TestContentHasherDirIsNotASignal(t *testing.T) {
	dir := t.TempDir()
	if got, ok := contentHasher(dir)(); ok || got != "" {
		t.Errorf("a directory signal path: got (%q,%v), want (\"\",false)", got, ok)
	}
}
