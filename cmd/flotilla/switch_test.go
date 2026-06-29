package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/surface"
)

// swRec records runSwitch's side effects and drives a phase-aware fake (the two-driver
// analogue of recycle_test.go's recRec): assess returns Idle through the FROM phases,
// Unknown after the FROM close (the claude-direct dead-pane window confirmed via paneDead),
// Idle after the TO relaunch (boot), and Working once the TO takeover is delivered — so one
// fake exercises every gate across BOTH drivers. The switch-only ops (phase records, the
// overlay write, the bundle write) are recorded in ORDER so the eager-durable + ordering
// invariants (P1-B, overlay-after-relaunch) are assertable.
type swRec struct {
	delivered         []string
	closed, respawned bool
	stamped           bool
	gen               string
	// ordered event log: "relaunching"/"overlay-pending"/"complete" (phase records),
	// "respawn", "overlay", "bundle" — interleaved so ordering is verifiable.
	events []string
	// knobs for the abort cases
	closeErr        error
	failPhase0      bool
	failDurable     bool
	failReverify    bool
	closeNeverShell bool
	overlay         bool
	markerGot       string
	genGot          string
	absentResult    bool
	absentErr       error
	lockedFlag      bool
	remainCalls     []bool
	// switch-only failure injections
	bundleErr  error            // writeBundle fails (Phase 2 — pre-close abort)
	overlayErr error            // writeOverlay fails (Phase 3b — half-switch, surfaced)
	recordErr  map[string]error // recordPhase fails for a named phase
}

func happySwitch() *swRec {
	return &swRec{markerGot: "the-key", absentResult: true, recordErr: map[string]error{}}
}

func (r *swRec) assess(string) surface.State {
	switch {
	case r.failPhase0:
		return surface.StateWorking
	case r.failReverify && r.lockedFlag && !r.closed:
		return surface.StateWorking // a turn started in the unlocked window
	case !r.closed:
		return surface.StateIdle // FROM phases 0, 1, re-verify
	case r.closed && !r.respawned:
		return surface.StateUnknown // claude-direct dead-pane window (confirmed via paneDead)
	case r.respawned && len(r.delivered) < 2:
		return surface.StateIdle // TO phase 4 boot (handoff delivered, takeover not yet)
	default:
		return surface.StateWorking // TO phase 4 resumption-confidence
	}
}

func (r *swRec) composer(string) surface.ComposerDisposition {
	if r.overlay {
		return surface.ComposerSubAgent
	}
	return surface.ComposerCleared
}

func fakeSwitchOps(r *swRec) switchOps {
	return switchOps{
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
		paneDead:     func(string) (bool, error) { return r.closed && !r.respawned && !r.closeNeverShell, nil },
		respawn: func(string, string, string) error {
			r.respawned = true
			r.events = append(r.events, "respawn")
			return nil
		},
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
		lock: func(string) (func(), error) { r.lockedFlag = true; return func() {}, nil },
		recordPhase: func(phase string) error {
			if err := r.recordErr[phase]; err != nil {
				return err
			}
			r.events = append(r.events, phase)
			return nil
		},
		writeOverlay: func() error {
			if r.overlayErr != nil {
				return r.overlayErr
			}
			r.events = append(r.events, "overlay")
			return nil
		},
		writeBundle: func() error {
			if r.bundleErr != nil {
				return r.bundleErr
			}
			r.events = append(r.events, "bundle")
			return nil
		},
		sleep: func(time.Duration) {},
	}
}

func testSwitchPlan() switchPlan {
	return switchPlan{
		agent: "research", key: "the-key", cwd: "/repo", launch: "grok --name research",
		token: "TOK", handoffPath: "/repo/.flotilla/handoffs/switch-TOK.md",
		fromSurface: "claude-code", toSurface: "grok",
		handoffText:  "HANDOFF(/repo/.flotilla/handoffs/switch-TOK.md)",
		takeoverText: "TAKEOVER(/repo/.flotilla/handoffs/switch-TOK.md)",
		ownPane:      "", minHandoffBytes: 200,
		timeouts: recycleTimeouts{handoff: 2 * recyclePollInterval, close_: 3 * recyclePollInterval, boot: 2 * recyclePollInterval, takeover: 2 * recyclePollInterval},
	}
}

// --- happy path: full two-driver pipeline; takeover exactly once ---

func TestRunSwitchHappyPath(t *testing.T) {
	r := happySwitch()
	msg, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err != nil {
		t.Fatalf("runSwitch: %v", err)
	}
	if !r.closed || !r.respawned || !r.stamped {
		t.Errorf("expected closed+respawned+stamped, got %+v", r)
	}
	if len(r.delivered) != 2 || r.delivered[0] != "HANDOFF(/repo/.flotilla/handoffs/switch-TOK.md)" || r.delivered[1] != "TAKEOVER(/repo/.flotilla/handoffs/switch-TOK.md)" {
		t.Errorf("delivered = %v, want [HANDOFF TAKEOVER] (takeover exactly once)", r.delivered)
	}
	if !strings.Contains(msg, "switched research") || !strings.Contains(msg, "claude-code") || !strings.Contains(msg, "grok") {
		t.Errorf("msg = %q, want a success line naming FROM→TO", msg)
	}
	if len(r.remainCalls) < 2 || r.remainCalls[0] != true || r.remainCalls[len(r.remainCalls)-1] != false {
		t.Errorf("remainCalls = %v, want first=on(true) last=off(false restore)", r.remainCalls)
	}
}

// --- GATE-2: the SAME neutral path is threaded into BOTH turns, never a driver-branded dir ---

func TestRunSwitchNeutralPathThreadedIntoBothTurns(t *testing.T) {
	r := happySwitch()
	p := testSwitchPlan()
	if _, err := runSwitch(fakeSwitchOps(r), p); err != nil {
		t.Fatalf("runSwitch: %v", err)
	}
	// Both delivered turns must name the EXACT neutral switch path (and nothing else).
	for i, want := range []string{"HANDOFF", "TAKEOVER"} {
		if !strings.Contains(r.delivered[i], p.handoffPath) {
			t.Errorf("delivered[%d] = %q, want it to name the neutral path %q", i, r.delivered[i], p.handoffPath)
		}
		_ = want
	}
	// The neutral path is product-owned (.flotilla/handoffs/…), never a claude/grok dir.
	if !strings.Contains(p.handoffPath, "/.flotilla/handoffs/switch-") {
		t.Errorf("handoffPath %q is not the harness-neutral product path", p.handoffPath)
	}
	for _, branded := range []string{".claude/", "/grok/", ".grok/"} {
		if strings.Contains(p.handoffPath, branded) {
			t.Errorf("handoffPath %q leaked a harness-branded dir %q (GATE-2)", p.handoffPath, branded)
		}
	}
}

// switchHandoffPath / switchBundlePath build the neutral, product-owned paths (GATE-2).
func TestSwitchNeutralPathBuilders(t *testing.T) {
	if got := switchHandoffPath("/proj", "TOK"); got != "/proj/.flotilla/handoffs/switch-TOK.md" {
		t.Errorf("switchHandoffPath = %q", got)
	}
	if got := switchBundlePath("/proj", "research", "TOK"); got != "/proj/.flotilla/switch/research/continuity-TOK.json" {
		t.Errorf("switchBundlePath = %q", got)
	}
	for _, branded := range []string{".claude", ".grok"} {
		if strings.Contains(switchHandoffPath("/proj", "TOK"), branded) || strings.Contains(switchBundlePath("/proj", "a", "TOK"), branded) {
			t.Errorf("neutral path builder leaked a branded dir %q", branded)
		}
	}
}

// --- 3.2: fail-closed refuse for a non-recycle-capable FROM or TO surface ---

// capableStub implements RecycleBridge + ComposerStateProbe (recycle-capable).
type capableStub struct{ name string }

func (c capableStub) Name() string                       { return c.name }
func (capableStub) Submit(string, string) error          { return nil }
func (capableStub) Assess(string) surface.State          { return surface.StateIdle }
func (capableStub) Rotate(string) error                  { return nil }
func (capableStub) RotateStrategy() surface.Strategy     { return surface.SlashCommand }
func (capableStub) Close(string) error                   { return nil }
func (capableStub) HandoffPath(cwd, token string) string { return cwd + "/branded/" + token }
func (capableStub) HandoffTurn(p string) string          { return "ho:" + p }
func (capableStub) TakeoverTurn(p string) string         { return "to:" + p }
func (capableStub) ComposerState(string) surface.ComposerDisposition {
	return surface.ComposerCleared
}

// bridgeNoProbe implements RecycleBridge but NOT ComposerStateProbe.
type bridgeNoProbe struct{}

func (bridgeNoProbe) Name() string                         { return "bridge-no-probe" }
func (bridgeNoProbe) Submit(string, string) error          { return nil }
func (bridgeNoProbe) Assess(string) surface.State          { return surface.StateIdle }
func (bridgeNoProbe) Rotate(string) error                  { return nil }
func (bridgeNoProbe) RotateStrategy() surface.Strategy     { return surface.SlashCommand }
func (bridgeNoProbe) Close(string) error                   { return nil }
func (bridgeNoProbe) HandoffPath(cwd, token string) string { return cwd + token }
func (bridgeNoProbe) HandoffTurn(p string) string          { return p }
func (bridgeNoProbe) TakeoverTurn(p string) string         { return p }

// noBridge implements neither (e.g. opencode/aider today).
type noBridge struct{}

func (noBridge) Name() string                     { return "no-bridge" }
func (noBridge) Submit(string, string) error      { return nil }
func (noBridge) Assess(string) surface.State      { return surface.StateIdle }
func (noBridge) Rotate(string) error              { return nil }
func (noBridge) RotateStrategy() surface.Strategy { return surface.SlashCommand }
func (noBridge) Close(string) error               { return nil }

func TestSwitchCapabilityRefusal(t *testing.T) {
	capable := capableStub{name: "claude-code"}
	cases := []struct {
		name     string
		from, to surface.Driver
		wantErr  bool
		wantSub  string
	}{
		{"both capable", capable, capableStub{name: "grok"}, false, ""},
		{"FROM no bridge", noBridge{}, capable, true, "FROM surface \"no-bridge\""},
		{"TO no bridge", capable, noBridge{}, true, "TO surface \"no-bridge\""},
		{"FROM no probe", bridgeNoProbe{}, capable, true, "FROM surface \"bridge-no-probe\""},
		{"TO no probe", capable, bridgeNoProbe{}, true, "TO surface \"bridge-no-probe\""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fb, tb, err := switchCapabilityRefusal("research", c.from, c.to)
			if c.wantErr {
				if err == nil || !strings.Contains(err.Error(), c.wantSub) {
					t.Fatalf("err = %v, want a refusal containing %q", err, c.wantSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if fb == nil || tb == nil {
				t.Fatalf("both bridges must be returned for a capable pair, got from=%v to=%v", fb, tb)
			}
		})
	}
}

// --- resolve / self-switch / copy-mode / git refusals (must not act) ---

func TestRunSwitchRefusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mut     func(o *switchOps)
		selfOwn string
		wantSub string
	}{
		{"none", func(o *switchOps) {
			o.resolve = func(string) (string, deliver.ResolveOutcome, error) { return "", deliver.ResolveNone, nil }
		}, "", "nothing to switch"},
		{"ambiguous", func(o *switchOps) {
			o.resolve = func(string) (string, deliver.ResolveOutcome, error) { return "", deliver.ResolveAmbiguous, nil }
		}, "", "mis-tagged"},
		{"self-switch", func(o *switchOps) {
			o.paneID = func(string) (string, error) { return "%9", nil }
		}, "%9", "own pane"},
		{"copy-mode", func(o *switchOps) {
			o.inMode = func(string) (bool, error) { return true, nil }
		}, "", "copy"},
		{"non-git", func(o *switchOps) {
			o.absent = func(string, string) (bool, error) { return false, errors.New("not a git work-tree") }
		}, "", "git work-tree"},
		{"pre-existing-blob", func(o *switchOps) {
			o.absent = func(string, string) (bool, error) { return false, nil }
		}, "", "already exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := happySwitch()
			ops := fakeSwitchOps(r)
			tc.mut(&ops)
			p := testSwitchPlan()
			if tc.selfOwn != "" {
				p.ownPane = tc.selfOwn
			}
			_, err := runSwitch(ops, p)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantSub)
			}
			if r.closed || r.respawned || len(r.delivered) != 0 {
				t.Errorf("refusal must not act: %+v", r)
			}
		})
	}
}

// --- phase aborts mirror recycle, leaving the desk on the FROM harness ---

func TestRunSwitchPhase0Abort(t *testing.T) {
	r := happySwitch()
	r.failPhase0 = true
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err == nil || !strings.Contains(err.Error(), "phase 0") {
		t.Fatalf("err = %v, want a phase-0 abort", err)
	}
	if len(r.delivered) != 0 {
		t.Errorf("phase-0 abort must not deliver the handoff turn (got %v)", r.delivered)
	}
}

func TestRunSwitchPhase1Abort(t *testing.T) {
	r := happySwitch()
	r.failDurable = true
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
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

// --- P1-C: under-lock re-verify abort (a turn started in the unlocked window) ---

func TestRunSwitchReverifyAbort(t *testing.T) {
	r := happySwitch()
	r.failReverify = true
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err == nil || !strings.Contains(err.Error(), "re-verify") {
		t.Fatalf("err = %v, want an under-lock re-verify abort", err)
	}
	if r.closed {
		t.Errorf("re-verify abort must not close the desk")
	}
	// Nothing irreversible/durable should have happened: no bundle, no relaunch record.
	for _, e := range r.events {
		if e == "respawn" || e == "overlay" || e == switchPhaseRelaunching {
			t.Errorf("re-verify abort recorded a post-lock irreversible event %q (events=%v)", e, r.events)
		}
	}
}

// --- close→confirm abort + ErrNoGracefulClose kill fallback (mirror recycle) ---

func TestRunSwitchCloseNeverConfirms(t *testing.T) {
	r := happySwitch()
	r.closeNeverShell = true
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err == nil || !strings.Contains(err.Error(), "resume research --force") {
		t.Fatalf("err = %v, want a dead-desk recovery copy naming --force", err)
	}
	if r.respawned {
		t.Errorf("a close that never confirms must NOT relaunch")
	}
}

func TestRunSwitchNoGracefulCloseFallsBackToKill(t *testing.T) {
	r := happySwitch()
	r.closeErr = surface.ErrNoGracefulClose
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err != nil {
		t.Fatalf("runSwitch (kill fallback): %v", err)
	}
	if !r.respawned {
		t.Errorf("ErrNoGracefulClose must fall back to the kill (respawn)")
	}
	if len(r.delivered) != 2 {
		t.Errorf("the kill-fallback path still takes over (got %v)", r.delivered)
	}
}

// --- P1-A invariant evidence: the takeover comes from the TO bridge, marker mismatch aborts ---

func TestRunSwitchMarkerMismatch(t *testing.T) {
	r := happySwitch()
	r.markerGot = "wrong-key"
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err == nil || !strings.Contains(err.Error(), "flotilla send") {
		t.Fatalf("err = %v, want a marker-mismatch abort naming the send escape hatch", err)
	}
}

// --- idempotency / supersede: a newer @flotilla_switch_gen aborts Phase-4 takeover (6.1) ---

func TestRunSwitchGenSuperseded(t *testing.T) {
	r := happySwitch()
	r.genGot = "OTHER-TOKEN"
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err == nil || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("err = %v, want a gen-superseded abort", err)
	}
	if len(r.delivered) != 1 {
		t.Errorf("a superseded switch must NOT deliver the takeover (got %v)", r.delivered)
	}
}

// --- P1-B: eager-durable phase records in order; overlay written ONLY after relaunch ---

func TestRunSwitchEagerDurablePhaseOrdering(t *testing.T) {
	r := happySwitch()
	if _, err := runSwitch(fakeSwitchOps(r), testSwitchPlan()); err != nil {
		t.Fatalf("runSwitch: %v", err)
	}
	// The recorded ordering proves the P1-B + overlay-after-relaunch invariants:
	//   bundle (pre-close) → relaunching → respawn → overlay-pending → overlay → complete
	want := []string{"bundle", switchPhaseRelaunching, "respawn", switchPhaseOverlayPending, "overlay", switchPhaseComplete}
	if strings.Join(r.events, ",") != strings.Join(want, ",") {
		t.Fatalf("event order = %v, want %v", r.events, want)
	}
	// Explicit ordering checks the join could miss if the sequence changed shape.
	idx := func(e string) int {
		for i, x := range r.events {
			if x == e {
				return i
			}
		}
		return -1
	}
	if idx(switchPhaseRelaunching) > idx("respawn") {
		t.Error("phase 'relaunching' must be recorded BEFORE the respawn (eager-durable intent)")
	}
	if idx("respawn") > idx("overlay") {
		t.Error("the overlay must be written AFTER the relaunch (never name a slot the pane isn't running)")
	}
	if idx(switchPhaseOverlayPending) > idx("overlay") {
		t.Error("'overlay-pending' must be recorded BEFORE the overlay write")
	}
	if idx("overlay") > idx(switchPhaseComplete) {
		t.Error("'complete' must be recorded AFTER the overlay write")
	}
}

// A pre-relaunch phase-record failure ABORTS before the relaunch (no recovery trail).
func TestRunSwitchRelaunchingRecordFailAborts(t *testing.T) {
	r := happySwitch()
	r.recordErr[switchPhaseRelaunching] = errors.New("disk full")
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err == nil || !strings.Contains(err.Error(), "before the relaunch") {
		t.Fatalf("err = %v, want an abort BEFORE the relaunch on a failed durable record", err)
	}
	if r.respawned {
		t.Error("a failed pre-relaunch durable record must NOT relaunch (no recovery trail)")
	}
}

// A failed overlay write AFTER a good relaunch is a surfaced half-switch (not a silent loss),
// and the overlay was NOT written — last-switch.json stays overlay-pending until --repair.
func TestRunSwitchOverlayWriteFailIsSurfacedHalfSwitch(t *testing.T) {
	r := happySwitch()
	r.overlayErr = errors.New("rename failed")
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err == nil || !strings.Contains(err.Error(), "--repair") {
		t.Fatalf("err = %v, want a half-switch surfaced with the --repair recovery path", err)
	}
	if !r.respawned {
		t.Error("the relaunch DID happen (the desk is live on the TO harness)")
	}
	for _, e := range r.events {
		if e == "overlay" {
			t.Error("the overlay must NOT be recorded when its write failed")
		}
	}
	// The durable trail must be at overlay-pending (recorded), not complete.
	var sawPending, sawComplete bool
	for _, e := range r.events {
		switch e {
		case switchPhaseOverlayPending:
			sawPending = true
		case switchPhaseComplete:
			sawComplete = true
		}
	}
	if !sawPending || sawComplete {
		t.Errorf("durable trail should be overlay-pending (got events=%v)", r.events)
	}
}

// --- GATE-3 + GATE-1: the bundle carries a BARE-STRING hint only; `from` is optional ---

// These exercise the continuityBundle shape directly (the cmdSwitch wiring that constructs
// it is group 4; the WRITE-SIDE schema is frozen here at P0). A real-world consumer (memex,
// P4) must find a bare-string hint and an optional `from`.

func TestContinuityBundleBareStringHintAndOptionalFrom(t *testing.T) {
	// GATE-3: memex_injection_hint is a BARE STRING (a JSON string), never an object/prose blob.
	b := continuityBundle{
		BundleVersion:      switchBundleVersion,
		ContinuityKind:     "switch",
		FlotillaAgent:      "research",
		ProjectRoot:        "/repo",
		To:                 bundleEndpoint{Surface: "grok", Provider: "xai", SubscriptionID: "team"},
		SwitchToken:        "TOK",
		HandoffPath:        "/repo/.flotilla/handoffs/switch-TOK.md",
		HintVersion:        switchHintVersion,
		MemexInjectionHint: "mode=switch;agent=research", // bare pointer string — NOT corpus text
		// From left nil → fresh launch (GATE-1)
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal bundle: %v", err)
	}
	// GATE-3: the hint must serialize as a JSON string (bare), never a nested object.
	var hint string
	if err := json.Unmarshal(generic["memex_injection_hint"], &hint); err != nil {
		t.Fatalf("memex_injection_hint is not a bare string: %v", err)
	}
	if hint != "mode=switch;agent=research" {
		t.Errorf("hint = %q, want the bare pointer", hint)
	}
	// GATE-1: `from` must be ABSENT (omitempty) on a fresh launch.
	if _, present := generic["from"]; present {
		t.Errorf("fresh-launch bundle must omit `from` (GATE-1); got %s", string(raw))
	}
	if _, present := generic["flotilla_agent"]; !present {
		t.Error("flotilla_agent is REQUIRED (the desk binding)")
	}
	// continuity_kind must be the literal "switch".
	var kind string
	_ = json.Unmarshal(generic["continuity_kind"], &kind)
	if kind != "switch" {
		t.Errorf("continuity_kind = %q, want \"switch\"", kind)
	}
}

func TestContinuityBundleWithFromIsPresent(t *testing.T) {
	b := continuityBundle{
		BundleVersion:  switchBundleVersion,
		ContinuityKind: "switch",
		FlotillaAgent:  "research",
		From:           &bundleEndpoint{Surface: "claude-code", Provider: "anthropic"},
		To:             bundleEndpoint{Surface: "grok", Provider: "xai"},
		SwitchToken:    "TOK",
		HintVersion:    switchHintVersion,
	}
	raw, _ := json.Marshal(b)
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := generic["from"]; !present {
		t.Errorf("a switch with a FROM harness must serialize `from`; got %s", string(raw))
	}
}

// The bundle is written BEFORE the irreversible close; a bundle-write failure aborts there.
func TestRunSwitchBundleWriteFailAbortsBeforeClose(t *testing.T) {
	r := happySwitch()
	r.bundleErr = errors.New("bundle disk full")
	_, err := runSwitch(fakeSwitchOps(r), testSwitchPlan())
	if err == nil || !strings.Contains(err.Error(), "continuity bundle") {
		t.Fatalf("err = %v, want a bundle-write abort", err)
	}
	if r.closed {
		t.Error("a bundle-write failure must abort BEFORE the irreversible close")
	}
}

// --- P1-A: runRecycle still resolves ONE driver and passes the recycle tests (untouched) ---

// This guards the load-bearing invariant that adding switch did NOT alter the single-driver
// recycle core. recycle's own tests (TestRunRecycleHappyPath et al.) live in recycle_test.go;
// here we assert the structural invariant directly: a recycleOps value drives runRecycle to a
// successful single-driver recycle, byte-unchanged by switch's arrival. (If switch had reached
// into recycle.go, this — and the recycle suite — would fail to compile or pass.)
func TestRecycleSingleDriverCoreUntouchedBySwitch(t *testing.T) {
	r := happyRec()
	msg, err := runRecycle(fakeRecycleOps(r), testPlan())
	if err != nil {
		t.Fatalf("runRecycle (single-driver core) regressed: %v", err)
	}
	if !r.closed || !r.respawned || !r.stamped {
		t.Errorf("the single-driver recycle core must still complete: %+v", r)
	}
	if len(r.delivered) != 2 {
		t.Errorf("recycle still uses ONE bridge for BOTH turns (got %v)", r.delivered)
	}
	if !strings.Contains(msg, "recycled backend") {
		t.Errorf("recycle success line changed: %q", msg)
	}
}
