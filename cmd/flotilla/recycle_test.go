package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/surface"
)

// recRec records runRecycle's side effects and drives a phase-aware fake: Assess returns
// Idle until the close, Shell after the close (until the relaunch), Idle after the relaunch
// (boot), and Working once the takeover is delivered — so a single fake exercises every gate.
type recRec struct {
	delivered         []string
	closed, respawned bool
	stamped           bool
	gen               string
	// knobs for the abort cases
	closeErr        error
	failPhase0      bool // Assess never idle → phase-0 abort
	failDurable     bool // durable never true → phase-1 abort
	failReverify    bool // a turn "starts" in the unlocked window → under-lock re-verify abort
	closeNeverShell bool // close confirms but the pane never becomes Shell → phase-2 abort
	overlay         bool // composer is on an overlay (not cleared)
	markerGot       string
	genGot          string // overrides what readGen returns at phase 4 (supersede simulation)
	absentResult    bool
	absentErr       error
	lockedFlag      bool   // set when the txn lock is taken (Phase 1 → close window)
	remainCalls     []bool // records SetRemainOnExit(on) calls (expect on then off-restore)
}

func happyRec() *recRec { return &recRec{markerGot: "the-key", absentResult: true} }

func (r *recRec) assess(string) surface.State {
	switch {
	case r.failPhase0:
		return surface.StateWorking
	case r.failReverify && r.locked() && !r.closed:
		return surface.StateWorking // a turn started in the unlocked window
	case !r.closed:
		return surface.StateIdle // phases 0, 1, re-verify
	case r.closed && !r.respawned:
		// A claude-direct fleet desk never becomes a Shell on /exit (the pane goes DEAD — see
		// paneDead below); a transient glitch reads Unknown. Either way pollClosed must not abort
		// early; it confirms via paneDead. Shell is only for a shell-backed desk (not modeled here).
		return surface.StateUnknown
	case r.respawned && len(r.delivered) < 2:
		return surface.StateIdle // phase 4 boot (handoff delivered, takeover not yet)
	default:
		return surface.StateWorking // phase 4 resumption-confidence
	}
}

// locked is true once the txn lock has been taken (between Phase 1 and the close), used to
// simulate failReverify (a turn starting in the unlocked window, caught under the lock).
func (r *recRec) locked() bool { return r.lockedFlag }

func (r *recRec) composer(string) surface.ComposerDisposition {
	if r.overlay {
		return surface.ComposerSubAgent
	}
	return surface.ComposerCleared
}

func fakeRecycleOps(r *recRec) recycleOps {
	return recycleOps{
		resolve:      func(string) (string, deliver.ResolveOutcome, error) { return "sess:0.1", deliver.ResolveUnique, nil },
		paneID:       func(string) (string, error) { return "%5", nil },
		inMode:       func(string) (bool, error) { return false, nil },
		assess:       r.assess,
		composer:     r.composer,
		absent:       func(string, string) (bool, error) { return r.absentResult, r.absentErr },
		durable:      func(string, string, int) (bool, error) { return !r.failDurable, nil },
		deliver:      func(_, text string) error { r.delivered = append(r.delivered, text); return nil },
		closeFn:      func(string) error { r.closed = true; return r.closeErr },
		remainOnExit: func(_ string, on bool) error { r.remainCalls = append(r.remainCalls, on); return nil },
		// A claude-direct desk: after /exit the pane is DEAD (until the respawn). closeNeverShell
		// models a close that never confirms (the process never exits / a stuck read) → pollClosed
		// must retry then abort.
		paneDead: func(string) (bool, error) { return r.closed && !r.respawned && !r.closeNeverShell, nil },
		respawn:  func(string, string, string) error { r.respawned = true; return nil },
		readMarker: func(string) (string, error) {
			if r.markerGot == "" {
				return "the-key", nil
			}
			return r.markerGot, nil
		},
		stampGen: func(_, tok string) error { r.stamped = true; r.gen = tok; return nil },
		readGen: func(string) (string, error) {
			if r.genGot != "" {
				return r.genGot, nil
			}
			return r.gen, nil
		},
		lock:  func(string) (func(), error) { r.lockedFlag = true; return func() {}, nil },
		sleep: func(time.Duration) {},
	}
}

func testPlan() recyclePlan {
	return recyclePlan{
		agent: "desk-a", key: "the-key", cwd: "/repo", launch: "claude --name desk-a",
		token: "TOK", designatedPath: "/repo/.claude/handoffs/recycle-TOK.md",
		handoffText: "HANDOFF", takeoverText: "TAKEOVER",
		ownPane: "", minHandoffBytes: 200,
		// tiny timeouts so abort loops terminate immediately (sleep is a no-op)
		timeouts: recycleTimeouts{handoff: 2 * recyclePollInterval, close_: 3 * recyclePollInterval, boot: 2 * recyclePollInterval, takeover: 2 * recyclePollInterval},
	}
}

// --- happy path (I4: takeover exactly once; full pipeline) ---

func TestRunRecycleHappyPath(t *testing.T) {
	r := happyRec()
	msg, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err != nil {
		t.Fatalf("runRecycle: %v", err)
	}
	if !r.closed || !r.respawned || !r.stamped {
		t.Errorf("expected closed+respawned+stamped, got %+v", r)
	}
	if len(r.delivered) != 2 || r.delivered[0] != "HANDOFF" || r.delivered[1] != "TAKEOVER" {
		t.Errorf("delivered = %v, want [HANDOFF TAKEOVER] (takeover exactly once)", r.delivered)
	}
	if !strings.Contains(msg, "recycled desk-a") {
		t.Errorf("msg = %q, want a success line", msg)
	}
	// remain-on-exit must be set ON before the close and restored OFF after (the claude-direct
	// close mechanism). The close was confirmed via pane_dead (assess returns Unknown post-close,
	// so reaching respawn proves pane_dead is the confirm signal).
	if len(r.remainCalls) < 2 || r.remainCalls[0] != true || r.remainCalls[len(r.remainCalls)-1] != false {
		t.Errorf("remainCalls = %v, want first=on(true) last=off(false restore)", r.remainCalls)
	}
}

// TestRunRecycleAbortRestoresRemainOnExit: a Phase-2 close that never confirms must STILL restore
// remain-on-exit off (the defer), so an aborted recycle doesn't change the desk's crash behaviour.
func TestRunRecycleAbortRestoresRemainOnExit(t *testing.T) {
	r := happyRec()
	r.closeNeverShell = true
	_, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err == nil {
		t.Fatal("expected a close abort")
	}
	if len(r.remainCalls) == 0 || r.remainCalls[len(r.remainCalls)-1] != false {
		t.Errorf("remainCalls = %v, want the restore (off) even on abort", r.remainCalls)
	}
}

// --- resolve / self-recycle / copy-mode / git refusals (4.1) ---

func TestRunRecycleResolveRefusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mut     func(o *recycleOps)
		wantSub string
	}{
		{"none", func(o *recycleOps) {
			o.resolve = func(string) (string, deliver.ResolveOutcome, error) { return "", deliver.ResolveNone, nil }
		}, "nothing to recycle"},
		{"ambiguous", func(o *recycleOps) {
			o.resolve = func(string) (string, deliver.ResolveOutcome, error) { return "", deliver.ResolveAmbiguous, nil }
		}, "mis-tagged"},
		{"self-recycle", func(o *recycleOps) {
			o.paneID = func(string) (string, error) { return "%9", nil }
		}, "own pane"},
		{"copy-mode", func(o *recycleOps) {
			o.inMode = func(string) (bool, error) { return true, nil }
		}, "copy"},
		{"non-git", func(o *recycleOps) {
			o.absent = func(string, string) (bool, error) { return false, errors.New("not a git work-tree") }
		}, "git work-tree"},
		{"pre-existing-blob", func(o *recycleOps) {
			o.absent = func(string, string) (bool, error) { return false, nil }
		}, "already exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := happyRec()
			ops := fakeRecycleOps(r)
			tc.mut(&ops)
			p := testPlan()
			if tc.name == "self-recycle" {
				p.ownPane = "%9"
			}
			_, err := runRecycle(ops, p)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantSub)
			}
			if r.closed || r.respawned || len(r.delivered) != 0 {
				t.Errorf("refusal must not act: %+v", r)
			}
		})
	}
}

// --- I6: phase-0 idle precondition abort (4.2) ---

func TestRunRecyclePhase0Abort(t *testing.T) {
	r := happyRec()
	r.failPhase0 = true
	_, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err == nil || !strings.Contains(err.Error(), "phase 0") {
		t.Fatalf("err = %v, want a phase-0 abort", err)
	}
	if len(r.delivered) != 0 {
		t.Errorf("phase-0 abort must not deliver the handoff turn (got %v)", r.delivered)
	}
}

// --- I1: phase-1 handoff gate abort (4.3) ---

func TestRunRecyclePhase1Abort(t *testing.T) {
	r := happyRec()
	r.failDurable = true
	_, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err == nil || !strings.Contains(err.Error(), "phase 1") {
		t.Fatalf("err = %v, want a phase-1 abort", err)
	}
	if r.closed || r.respawned {
		t.Errorf("phase-1 abort must not close or relaunch (%+v)", r)
	}
	if len(r.delivered) != 1 {
		t.Errorf("the handoff turn is delivered once; takeover never (got %v)", r.delivered)
	}
}

// --- I1+I7: under-lock re-verify abort (4.4) ---

func TestRunRecycleReverifyAbort(t *testing.T) {
	r := happyRec()
	r.failReverify = true // a turn starts in the unlocked window
	_, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err == nil || !strings.Contains(err.Error(), "re-verify") {
		t.Fatalf("err = %v, want an under-lock re-verify abort", err)
	}
	if r.closed {
		t.Errorf("re-verify abort must not close the desk")
	}
}

// --- I2: close→Shell abort + retry-on-Unknown (4.6) ---

func TestRunRecycleCloseNeverShell(t *testing.T) {
	r := happyRec()
	r.closeNeverShell = true // assess returns Unknown after the close (transient glitch)
	_, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err == nil || !strings.Contains(err.Error(), "resume desk-a --force") {
		t.Fatalf("err = %v, want a state-aware dead-desk recovery copy naming --force", err)
	}
	if r.respawned {
		t.Errorf("a close that never confirms a shell must NOT relaunch")
	}
}

// --- close fallback: ErrNoGracefulClose → handoff-gated kill (respawn) (4.7) ---

func TestRunRecycleNoGracefulCloseFallsBackToKill(t *testing.T) {
	r := happyRec()
	r.closeErr = surface.ErrNoGracefulClose
	msg, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err != nil {
		t.Fatalf("runRecycle (kill fallback): %v", err)
	}
	if !r.respawned {
		t.Errorf("ErrNoGracefulClose must fall back to the kill (respawn)")
	}
	if len(r.delivered) != 2 {
		t.Errorf("the kill-fallback path still takes over (got %v)", r.delivered)
	}
	_ = msg
}

// --- I3: marker mismatch → abort with the live-fresh-desk recovery copy (4.8) ---

func TestRunRecycleMarkerMismatch(t *testing.T) {
	r := happyRec()
	r.markerGot = "wrong-key"
	_, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err == nil || !strings.Contains(err.Error(), "flotilla send") {
		t.Fatalf("err = %v, want a marker-mismatch abort naming `flotilla send ... take over`", err)
	}
}

// --- I4: gen superseded → abort without delivering the takeover (4.9) ---

func TestRunRecycleGenSuperseded(t *testing.T) {
	r := happyRec()
	r.genGot = "OTHER-TOKEN" // a superseding recycle re-stamped the pane
	_, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err == nil || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("err = %v, want a gen-superseded abort", err)
	}
	if len(r.delivered) != 1 {
		t.Errorf("a superseded recycle must NOT deliver the takeover (got %v)", r.delivered)
	}
}

// --- samePaneAsSelf: canonical %N comparison (the self-recycle guard's core) ---

func TestSamePaneAsSelf(t *testing.T) {
	cases := []struct {
		target, own string
		want        bool
	}{
		{"%5", "%5", true},  // same canonical id → self
		{"%5", "%9", false}, // different pane
		{"%5", "", false},   // empty ownPane (watch host / cron) → not self
		{"", "%5", false},   // empty target id → not self
	}
	for _, c := range cases {
		if got := samePaneAsSelf(c.target, c.own); got != c.want {
			t.Errorf("samePaneAsSelf(%q,%q) = %v, want %v", c.target, c.own, got, c.want)
		}
	}
}

// --- parseRecycleArgs: ordering, dry-run, errors ---

func TestParseRecycleArgs(t *testing.T) {
	t.Setenv("FLOTILLA_LAUNCH", "")
	cases := []struct {
		name      string
		args      []string
		wantAgent string
		wantDry   bool
		wantErr   bool
	}{
		{"agent only", []string{"desk-a"}, "desk-a", false, false},
		{"agent then dry", []string{"desk-a", "--dry-run"}, "desk-a", true, false},
		{"dry then agent", []string{"--dry-run", "desk-a"}, "desk-a", true, false},
		{"agent then launch", []string{"desk-a", "--launch", "/tmp/l.json"}, "desk-a", false, false},
		{"no agent", []string{"--dry-run"}, "", false, true},
		{"empty", []string{}, "", false, true},
		{"extra positional", []string{"a", "b"}, "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent, _, _, dry, err := parseRecycleArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseRecycleArgs(%v) = nil error, want error", c.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRecycleArgs(%v): %v", c.args, err)
			}
			if agent != c.wantAgent || dry != c.wantDry {
				t.Errorf("got (agent=%q dry=%v), want (%q %v)", agent, dry, c.wantAgent, c.wantDry)
			}
		})
	}
}
