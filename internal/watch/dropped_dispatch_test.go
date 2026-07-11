package watch

import (
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

func TestUndeliveredDispatchSweep_ExactlyOnce(t *testing.T) {
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
	n1 := UndeliveredDispatchSweep(dir, time.Now, func(s string) { alerts = append(alerts, s) }, set)
	n2 := UndeliveredDispatchSweep(dir, time.Now, func(s string) { alerts = append(alerts, s) }, set)
	if n1 != 1 || n2 != 0 {
		t.Fatalf("sweep counts n1=%d n2=%d, want 1 then 0", n1, n2)
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "dispatch undelivered") {
		t.Fatalf("alerts = %v", alerts)
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
