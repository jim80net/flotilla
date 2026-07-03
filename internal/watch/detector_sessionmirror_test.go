package watch

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/sessionmirror"
	"github.com/jim80net/flotilla/internal/surface"
)

// Integration: Working→Idle finish edge invokes MirrorOnFinish; the callback can fan out
// to Discord (info body) and session-mirror ledger without a second turn-final read.
func TestDetectorFinish_MirrorCallbackCanAppendSessionLedger(t *testing.T) {
	dir := t.TempDir()
	verbose := "operator-visible report"
	info := "operator-visible report"
	var posted string
	var mu sync.Mutex

	cfg := mirrorConfig("xo", []string{"xo", "backend"}, func(agent string) {
		mu.Lock()
		defer mu.Unlock()
		if agent != "backend" {
			t.Fatalf("mirror agent = %q, want backend", agent)
		}
		posted = info
		rec := sessionmirror.NewRecord(sessionmirror.Input{
			Agent:   agent,
			At:      time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
			Verbose: verbose,
			Info:    info,
		})
		if err := sessionmirror.Append(dir, agent, rec, sessionmirror.AppendOptions{}); err != nil {
			t.Fatalf("append: %v", err)
		}
	})
	cfg.Assess = func(string) surface.State { return surface.StateIdle }
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "snap.json"))
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	d.Tick()

	if posted != info {
		t.Errorf("posted = %q, want info body %q", posted, info)
	}
	path, err := sessionmirror.LedgerPath(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := sessionmirror.BuildHistory("backend", raw, 0)
	if len(doc.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(doc.Entries))
	}
	if doc.Entries[0].Verbose != verbose {
		t.Errorf("verbose = %q", doc.Entries[0].Verbose)
	}
	if doc.Entries[0].Info != info {
		t.Errorf("info = %q", doc.Entries[0].Info)
	}
}
