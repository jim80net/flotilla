package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAwaitingMarkerAbsentAllowsRotate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "flotilla-xo-awaiting")
	if NewAwaitingMarker(p).Present() {
		t.Error("absent marker must report not-present (rotate allowed)")
	}
}

func TestAwaitingMarkerPresentVetoesRotate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "flotilla-xo-awaiting")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !NewAwaitingMarker(p).Present() {
		t.Error("present marker must report present (rotate vetoed)")
	}
}

func TestAwaitingMarkerEmptyPathIsNoVeto(t *testing.T) {
	if NewAwaitingMarker("").Present() {
		t.Error("unconfigured (empty path) marker must report not-present")
	}
}

func TestAwaitingMarkerUnreadableFailsSafeToPresent(t *testing.T) {
	// A path whose PARENT is not a directory makes Stat return a non-IsNotExist
	// error (ENOTDIR), modeling an unreadable marker. Fail-safe → treat present.
	notDir := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(notDir, "marker") // afile/marker — afile is not a dir
	if !NewAwaitingMarker(p).Present() {
		t.Error("an unreadable marker must fail safe to present (veto rotate)")
	}
}
