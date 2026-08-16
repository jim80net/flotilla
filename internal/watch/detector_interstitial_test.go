package watch

import (
	"testing"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestDetectorTickInvokesInterstitialManagerWithoutSend(t *testing.T) {
	f := newFixture()
	f.set("backend", surface.StateIdle)
	cfg := f.config("backend", []string{"backend"}, 3, "normal")
	calls := 0
	dispatches := 0
	cfg.InterstitialOnTick = func() { calls++ }
	cfg.MirrorDispatch = func(run func()) {
		dispatches++
		run()
	}

	d := newDet(t, f, cfg)
	d.Tick()

	if calls != 1 || dispatches != 1 {
		t.Fatalf("watch tick: manager calls=%d dispatches=%d, want 1/1", calls, dispatches)
	}
}
