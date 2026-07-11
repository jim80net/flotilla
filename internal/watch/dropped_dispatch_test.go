package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

func droppedDispatchConfig(desks []string, onFinish func(string)) DetectorConfig {
	return DetectorConfig{
		XOAgent:                 "xo",
		Desks:                   desks,
		Interval:                time.Minute,
		AckAge:                  func() time.Duration { return 0 },
		Wake:                    func(WakeKind, []string) {},
		Persist:                 func(Snapshot) error { return nil },
		DroppedDispatchOnFinish: onFinish,
	}
}

// TestDetectorDroppedDispatchOnFinish_FiresOnWorkingIdle locks the #472 detector seam:
// same Working→Idle trigger as IdleHold/StrandedHandoff.
func TestDetectorDroppedDispatchOnFinish_FiresOnWorkingIdle(t *testing.T) {
	var (
		mu    sync.Mutex
		calls []string
	)
	cfg := droppedDispatchConfig([]string{"xo", "codex-harness-dev"}, func(agent string) {
		mu.Lock()
		calls = append(calls, agent)
		mu.Unlock()
	})
	cfg.Assess = func(string) surface.State { return surface.StateIdle }
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "snap.json"))
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "codex-harness-dev": surface.StateWorking}, "h0")
	d.Tick()
	mu.Lock()
	got := calls
	mu.Unlock()
	if len(got) != 1 || got[0] != "codex-harness-dev" {
		t.Fatalf("DroppedDispatchOnFinish calls = %v, want [codex-harness-dev]", got)
	}
}

// TestDroppedDispatchEndToEnd confirms inbound track + finish hook reinject (#472).
func TestDroppedDispatchEndToEnd(t *testing.T) {
	dir := t.TempDir()
	var reinjected []Job
	enqueue := func(j Job) { reinjected = append(reinjected, j) }

	in := NewInjector(func(string, string) error { return nil }, 0)
	in.rosterDir = dir
	in.SetInboundTrack(InboundTrackHook(dir, nil))

	msg, nonce, err := inbound.AppendDispatchNonce("Phase-2 wave: implement portable-location for hermes adapter")
	if err != nil {
		t.Fatal(err)
	}
	in.deliver(Job{
		Agent: "codex-harness-dev", Message: msg, Kind: KindSend,
		Sender: "memex", MessageID: "m1",
	})

	path, err := inbound.Path(dir, "codex-harness-dev")
	if err != nil {
		t.Fatal(err)
	}
	if got := inbound.NewStore(path).Load(); len(got) != 1 || got[0].Nonce != nonce {
		t.Fatalf("inbound ledger = %+v, want nonce %q", got, nonce)
	}

	hook := DroppedDispatchFinishHook(dir, func(string) (string, bool, error) {
		return "Visibility synthesis complete. Fleet map updated.", true, nil
	}, enqueue, nil)
	hook("codex-harness-dev")

	if len(reinjected) != 1 {
		t.Fatalf("want one reinject, got %d", len(reinjected))
	}
	if reinjected[0].Agent != "codex-harness-dev" || reinjected[0].Kind != KindDetector {
		t.Fatalf("reinject job = %+v", reinjected[0])
	}
	if !strings.Contains(reinjected[0].Message, "dropped-dispatch resume") {
		t.Fatalf("reinject message missing preamble: %q", reinjected[0].Message)
	}
	got := inbound.NewStore(path).Load()
	if len(got) != 1 || got[0].Deferrals != 0 {
		t.Fatalf("deferrals before confirmed reinject: %+v, want 0", got)
	}
	if reinjected[0].ClaimKey != inbound.ReinjectClaimKey("codex-harness-dev", "m1") {
		t.Fatalf("reinject claim key = %q", reinjected[0].ClaimKey)
	}
}

// #614: consumed registry suppresses reinject of an already-settled nonce.
func TestDroppedDispatch_ConsumedSuppressesReinject(t *testing.T) {
	dir := t.TempDir()
	var reinjected []Job
	msg, nonce, err := inbound.AppendDispatchNonce("implement already-gated work that must not reinject")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "m1", Sender: "memex", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.Consume(dir, dispatch.ConsumeFromInbound(nonce, msg, dispatch.ReasonTurnFinalAck, "memex", "desk")); err != nil {
		t.Fatal(err)
	}
	hook := DroppedDispatchFinishHook(dir, func(string) (string, bool, error) {
		return "unrelated turn final without the nonce", true, nil
	}, func(j Job) { reinjected = append(reinjected, j) }, nil)
	hook("desk")
	if len(reinjected) != 0 {
		t.Fatalf("consumed nonce must not reinject, got %+v", reinjected)
	}
	path, _ := inbound.Path(dir, "desk")
	if n := len(inbound.NewStore(path).Load()); n != 0 {
		t.Fatalf("pending inbound after consume suppress = %d, want 0", n)
	}
}

// #614: turn-final ack writes durable consume so a later miss cannot reinject.
func TestDroppedDispatch_AckConsumesIdempotent(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("phase work that the desk will acknowledge in turn-final")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "m2", Sender: "xo", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var reinjected []Job
	hook := DroppedDispatchFinishHook(dir, func(string) (string, bool, error) {
		return "done. nonce " + nonce + " addressed.", true, nil
	}, func(j Job) { reinjected = append(reinjected, j) }, nil)
	hook("desk")
	if len(reinjected) != 0 {
		t.Fatalf("acked dispatch must not reinject: %+v", reinjected)
	}
	if !dispatch.IsConsumed(dir, nonce, dispatch.PayloadHash(msg)) {
		t.Fatal("ack must durable-consume nonce")
	}
	// Re-record and finish without nonce — still suppressed by registry.
	if err := inbound.Record(dir, inbound.Entry{
		ID: "m2b", Sender: "xo", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	hook = DroppedDispatchFinishHook(dir, func(string) (string, bool, error) {
		return "unrelated idle finish", true, nil
	}, func(j Job) { reinjected = append(reinjected, j) }, nil)
	hook("desk")
	if len(reinjected) != 0 {
		t.Fatalf("post-ack resume must not reinject consumed nonce: %+v", reinjected)
	}
}

// #616: MERGED-state suppress auto-consumes and skips reinject.
func TestDroppedDispatch_MergedSuppressesReinject(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("Resume implement work for PR #614 after cubic findings")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "m3", Sender: "adj", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var reinjected []Job
	hook := DroppedDispatchFinishHookWithMerged(dir, func(string) (string, bool, error) {
		return "idle without addressing", true, nil
	}, func(j Job) { reinjected = append(reinjected, j) }, nil, func(pr int) bool { return pr == 614 })
	hook("desk")
	if len(reinjected) != 0 {
		t.Fatalf("merged PR must not reinject: %+v", reinjected)
	}
	if !dispatch.IsConsumed(dir, nonce, dispatch.PayloadHash(msg)) {
		t.Fatal("merged suppress must consume")
	}
}

// #616: chapter HOLD leaves pending without reinject or consume.
func TestDroppedDispatch_ChapterHoldDefersReinject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, dispatch.ChapterHoldFile), []byte("hold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, nonce, err := inbound.AppendDispatchNonce("non-urgent resume after chapter for hermes port work")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "m4", Sender: "xo", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var reinjected []Job
	hook := DroppedDispatchFinishHook(dir, func(string) (string, bool, error) {
		return "chapter still active", true, nil
	}, func(j Job) { reinjected = append(reinjected, j) }, nil)
	hook("desk")
	if len(reinjected) != 0 {
		t.Fatalf("chapter HOLD must not reinject: %+v", reinjected)
	}
	path, _ := inbound.Path(dir, "desk")
	if n := len(inbound.NewStore(path).Load()); n != 1 {
		t.Fatalf("HOLD must keep pending, got %d", n)
	}
	if dispatch.IsConsumed(dir, nonce, dispatch.PayloadHash(msg)) {
		t.Fatal("HOLD must not consume")
	}
}

// #628: sweep with turn-final ack heals inbound before scanning (no false-positive alert).
func TestUndeliveredDispatchSweep_ReconcileTurnFinalSuppressesFalsePositive(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("specimen FO dispatch already acked in turn-final pad")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "spec1", Sender: "xo", Recipient: "backend", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	var alerts []string
	set := NewUndeliveredAlertSet()
	n := UndeliveredDispatchSweep(dir, UndeliveredHooks{
		AlertOperator: func(s string) { alerts = append(alerts, s) },
		Fired:         set,
		ReadTurnFinal: func(agent string) (string, bool, error) {
			return "completed work; " + nonce, true, nil
		},
	})
	if n != 0 || len(alerts) != 0 {
		t.Fatalf("false-positive: journal=%d alerts=%v", n, alerts)
	}
	path, _ := inbound.Path(dir, "backend")
	if len(inbound.NewStore(path).Load()) != 0 {
		t.Fatal("inbound must be cleared after turn-final reconcile")
	}
}

func TestUndeliveredDispatchSweep_NoAdjutant_AlertsOperatorOnce(t *testing.T) {
	dir := t.TempDir()
	msg, _, err := inbound.AppendDispatchNonce("completion report that sat in outbox too long pad")
	if err != nil {
		t.Fatal(err)
	}
	path, err := outbox.Path(dir, "xo")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-45 * time.Minute)
	if _, _, err := outbox.NewStore(path).Insert(outbox.Entry{
		Sender: "xo", Recipient: "cos", Message: msg, EnqueuedAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	var alerts []string
	set := NewUndeliveredAlertSet()
	hooks := UndeliveredHooks{
		AlertOperator: func(s string) { alerts = append(alerts, s) },
		Fired:         set,
	}
	n1 := UndeliveredDispatchSweep(dir, hooks)
	n2 := UndeliveredDispatchSweep(dir, hooks)
	if n1 != 1 || n2 != 0 {
		t.Fatalf("sweep counts n1=%d n2=%d, want 1 then 0", n1, n2)
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "dispatch undelivered") {
		t.Fatalf("alerts = %v", alerts)
	}
}

// #628: with AdjutantFor resolved, first age crossing enqueues adj and does NOT call alert.
func TestUndeliveredDispatchSweep_AdjutantFirst_NoOperatorOnL1(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("implement portable work that was delivered but unacked pad")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "in1", Sender: "xo", Recipient: "backend", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-20 * time.Minute), // past UndeliveredInboundAge (15m)
	}); err != nil {
		t.Fatal(err)
	}
	var alerts, adjMsgs []string
	var adjAgent string
	set := NewUndeliveredAlertSet()
	hooks := UndeliveredHooks{
		ResolveAdjutant: func(recipient string) string {
			if recipient == "backend" {
				return "xo-adj"
			}
			return ""
		},
		EnqueueAdjutant: func(adj, message string) {
			adjAgent = adj
			adjMsgs = append(adjMsgs, message)
		},
		AlertOperator: func(s string) { alerts = append(alerts, s) },
		Fired:         set,
	}
	n := UndeliveredDispatchSweep(dir, hooks)
	if n != 1 {
		t.Fatalf("journal count = %d, want 1", n)
	}
	if adjAgent != "xo-adj" || len(adjMsgs) != 1 {
		t.Fatalf("adj enqueue = agent %q msgs %v", adjAgent, adjMsgs)
	}
	if !strings.Contains(adjMsgs[0], "undelivered-dispatch triage") || !strings.Contains(adjMsgs[0], nonce) {
		t.Fatalf("adj message missing triage/nonce:\n%s", adjMsgs[0])
	}
	if len(alerts) != 0 {
		t.Fatalf("operator must not fire on L1 when adjutant set: %v", alerts)
	}
	// Exactly-once L1.
	UndeliveredDispatchSweep(dir, hooks)
	if len(adjMsgs) != 1 || len(alerts) != 0 {
		t.Fatalf("second L1-age tick must not re-fire: adj=%d alerts=%v", len(adjMsgs), alerts)
	}
}

// #628 / storm guard: already past L2 on first observation → L1 only, grandfather L2 (no operator).
func TestUndeliveredDispatchSweep_ColdStartGrandfatherNoL2Storm(t *testing.T) {
	dir := t.TempDir()
	// Seed many stale inbound past L2 wall age.
	for i := 0; i < 5; i++ {
		msg, nonce, err := inbound.AppendDispatchNonce(fmt.Sprintf("stale inbound backlog item number %02d pad enough", i))
		if err != nil {
			t.Fatal(err)
		}
		if err := inbound.Record(dir, inbound.Entry{
			ID: fmt.Sprintf("stale%d", i), Sender: "xo", Recipient: "backend", Message: msg, Nonce: nonce,
			DeliveredAt: time.Now().UTC().Add(-2 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	var alerts, adjMsgs []string
	set := NewUndeliveredAlertSet()
	hooks := UndeliveredHooks{
		ResolveAdjutant: func(string) string { return "xo-adj" },
		EnqueueAdjutant: func(_, message string) { adjMsgs = append(adjMsgs, message) },
		AlertOperator:   func(s string) { alerts = append(alerts, s) },
		Fired:           set,
	}
	// Tick 1 (cold start): L1 adjutant only; grandfather L2.
	UndeliveredDispatchSweep(dir, hooks)
	if len(adjMsgs) != 5 {
		t.Fatalf("L1 adj = %d want 5", len(adjMsgs))
	}
	if len(alerts) != 0 {
		t.Fatalf("cold-start must not operator-storm: %v", alerts)
	}
	// Tick 2: still no L2 (grandfathered + watched window).
	UndeliveredDispatchSweep(dir, hooks)
	if len(alerts) != 0 {
		t.Fatalf("grandfathered L2 must stay quiet: %v", alerts)
	}
}

func TestUndeliveredDispatchSweep_L2WatchedWindowAndRateLimit(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	// 4 entries delivered 20m before t0 → L1 age met, L2 wall (45m) not yet.
	for i := 0; i < 4; i++ {
		msg, nonce, err := inbound.AppendDispatchNonce(fmt.Sprintf("watch-window inbound item number %02d pad enough chars", i))
		if err != nil {
			t.Fatal(err)
		}
		if err := inbound.Record(dir, inbound.Entry{
			ID: fmt.Sprintf("w%d", i), Sender: "xo", Recipient: "backend", Message: msg, Nonce: nonce,
			DeliveredAt: t0.Add(-20 * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	var alerts, adjMsgs []string
	set := NewUndeliveredAlertSet()
	clock := t0
	hooks := UndeliveredHooks{
		Now:             func() time.Time { return clock },
		ResolveAdjutant: func(string) string { return "xo-adj" },
		EnqueueAdjutant: func(_, _ string) { adjMsgs = append(adjMsgs, "x") },
		AlertOperator:   func(s string) { alerts = append(alerts, s) },
		Fired:           set,
		MaxL2PerTick:    2,
	}
	// Tick 1: L1 only.
	UndeliveredDispatchSweep(dir, hooks)
	if len(adjMsgs) != 4 || len(alerts) != 0 {
		t.Fatalf("tick1 adj=%d alerts=%d", len(adjMsgs), len(alerts))
	}
	// Tick 2: jump 16m (past MinWatchBeforeL2=15m) and age wall to 20+16=36m still < 45m L2 wall.
	clock = t0.Add(16 * time.Minute)
	UndeliveredDispatchSweep(dir, hooks)
	if len(alerts) != 0 {
		t.Fatalf("watched but wall age not L2 yet: %v", alerts)
	}
	// Tick 3: jump so wall age ≥ 45m (delivered was t0-20m; need now >= t0+25m).
	clock = t0.Add(30 * time.Minute) // age = 50m ≥ 45m; watched since L1 = 30m ≥ 15m
	UndeliveredDispatchSweep(dir, hooks)
	// Rate limit 2 + 1 summary for deferred.
	l2 := 0
	summary := 0
	for _, a := range alerts {
		if strings.Contains(a, "rate-limited") {
			summary++
		} else if strings.Contains(a, "second-layer") {
			l2++
		}
	}
	if l2 != 2 {
		t.Fatalf("L2 per-tick want 2, got %d alerts=%v", l2, alerts)
	}
	if summary != 1 {
		t.Fatalf("want one rate-limit summary, got %d alerts=%v", summary, alerts)
	}
	// Tick 4: drain remaining 2 L2.
	alerts = nil
	UndeliveredDispatchSweep(dir, hooks)
	l2 = 0
	for _, a := range alerts {
		if strings.Contains(a, "second-layer") {
			l2++
		}
	}
	if l2 != 2 {
		t.Fatalf("tick4 drain L2 want 2, got %d alerts=%v", l2, alerts)
	}
}

func TestResolveUndeliveredAdjutant_Order(t *testing.T) {
	// Prefer owning-XO adjutant over primary.
	got := ResolveUndeliveredAdjutant(
		func(c string) string {
			if c == "alpha-xo" {
				return "alpha-adj"
			}
			if c == "meta" {
				return "meta-adj"
			}
			return ""
		},
		func(agent string) string {
			if agent == "backend" {
				return "alpha-xo"
			}
			return "meta"
		},
		"meta",
		"backend",
	)
	if got != "alpha-adj" {
		t.Fatalf("got %q, want alpha-adj", got)
	}
	// Fall back to primary adjutant when owner has none.
	got = ResolveUndeliveredAdjutant(
		func(c string) string {
			if c == "meta" {
				return "meta-adj"
			}
			return ""
		},
		func(string) string { return "alpha-xo" },
		"meta",
		"backend",
	)
	if got != "meta-adj" {
		t.Fatalf("fallback got %q", got)
	}
	if ResolveUndeliveredAdjutant(nil, nil, "xo", "desk") != "" {
		t.Fatal("nil adjutantFor")
	}
}

func TestInboundTrackHook_SkipsCoordinators(t *testing.T) {
	dir := t.TempDir()
	hook := InboundTrackHook(dir, func(agent string) bool { return agent == "cos" })
	msg, _, _ := inbound.AppendDispatchNonce("hi")
	hook(Job{Agent: "cos", Message: msg, Kind: KindSend, Sender: "memex", MessageID: "1"})
	hook(Job{Agent: "backend", Message: msg, Kind: KindSend, Sender: "memex", MessageID: "2"})
	path, _ := inbound.Path(dir, "backend")
	if len(inbound.NewStore(path).Load()) != 1 {
		t.Fatal("coordinator inbound must not be tracked")
	}
}

func TestInjectorInboundTrack_OnConfirmedKindSend(t *testing.T) {
	dir := t.TempDir()
	in := NewInjector(func(string, string) error { return nil }, 0)
	in.rosterDir = dir
	in.SetInboundTrack(InboundTrackHook(dir, nil))

	msg, _, err := inbound.AppendDispatchNonce("status")
	if err != nil {
		t.Fatal(err)
	}
	in.deliver(Job{Agent: "backend", Message: msg, Kind: KindSend, Sender: "xo", MessageID: "id1"})

	path, _ := inbound.Path(dir, "backend")
	if len(inbound.NewStore(path).Load()) != 1 {
		t.Fatal("confirmed KindSend must record inbound pending dispatch")
	}
}

// #498 walk: confirmed KindSend through injector + real IsCoordinator on desk-home
// channel shape (xo_agent=backend, members=[meta-xo]) must write inbound ledger.
func TestInjectorInboundTrack_WalkDeskHomeChannel498(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	body := `{
	  "operator_user_id":"U","xo_agent":"meta-xo","cos_agent":"meta-xo",
	  "agents":[{"name":"meta-xo"},{"name":"backend"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command","members":["meta-xo","backend"]},
	    {"channel_id":"C_BE","xo_agent":"backend","members":["meta-xo"]}
	  ]
	}`
	if err := os.WriteFile(rosterPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsCoordinator("backend") {
		t.Fatal("backend must not be coordinator on desk-home walk shape")
	}

	in := NewInjector(func(string, string) error { return nil }, 0)
	in.rosterDir = dir
	in.SetInboundTrack(InboundTrackHook(dir, cfg.IsCoordinator))

	msg, nonce, err := inbound.AppendDispatchNonce("ORG dispatch: harness work")
	if err != nil {
		t.Fatal(err)
	}
	in.deliver(Job{
		Agent: "backend", Message: msg, Kind: KindSend,
		Sender: "meta-xo", MessageID: "walk-e2e-1",
	})

	path, err := inbound.Path(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	got := inbound.NewStore(path).Load()
	if len(got) != 1 || got[0].Nonce != nonce || got[0].Sender != "meta-xo" {
		t.Fatalf("inbound ledger = %+v, want nonce %q from meta-xo", got, nonce)
	}
}
