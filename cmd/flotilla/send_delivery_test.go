package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestNextSendRetryWait(t *testing.T) {
	if got := nextSendRetryWait(sendRetryInitial); got != 10*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := nextSendRetryWait(40 * time.Second); got != sendRetryMax {
		t.Fatalf("cap got %v", got)
	}
}

func TestErrRetryableBusyUnwrap(t *testing.T) {
	err := fmt.Errorf("%w", errRetryableBusy{agent: "cos"})
	if !errors.Is(err, surface.ErrBusy) {
		t.Fatal("should unwrap to ErrBusy")
	}
}

// Acceptance (#484): repeated bounce of the same send dedups to the existing outbox id.
// Drives the production path: stamp → bounce → enqueue (cmdSend stamps before enqueue).
func TestBouncedSendDedupesIdenticalPending(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[{"name":"xo"},{"name":"alpha"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	base := "deploy complete"
	busy := errRetryableBusy{agent: "xo"}
	msg1, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := enqueueOrFailSend(rosterPath, "alpha", "xo", msg1, busy); err != nil {
		t.Fatal(err)
	}
	msg2, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	if msg1 == msg2 {
		t.Fatal("probe requires distinct stamps")
	}
	if err := enqueueOrFailSend(rosterPath, "alpha", "xo", msg2, busy); err != nil {
		t.Fatal(err)
	}
	path, err := outbox.Path(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	got := outbox.NewStore(path).Load()
	if len(got) != 1 {
		t.Fatal("duplicate bounce must not append a second pending entry")
	}
	if got[0].Message != msg1 {
		t.Fatalf("surviving queued send keeps first stamp, got %q", got[0].Message)
	}
}

// Acceptance (#475): a bounced send lands in the sender's durable outbox instead of failing the turn.
func TestBouncedSendLandsInOutbox(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[{"name":"xo"},{"name":"alpha"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	busy := errRetryableBusy{agent: "xo"}
	if err := enqueueOrFailSend(rosterPath, "alpha", "xo", "deploy complete", busy); err != nil {
		t.Fatalf("enqueueOrFailSend = %v, want success (queued)", err)
	}
	path, err := outbox.Path(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	got := outbox.NewStore(path).Load()
	if len(got) != 1 || got[0].Recipient != "xo" || got[0].Message != "deploy complete" {
		t.Fatalf("outbox = %+v, want one pending send to xo", got)
	}
	if got[0].EnqueuedAt.IsZero() {
		t.Fatal("enqueued_at must be set")
	}
}

// Acceptance (#494): CLI direct-delivery success writes the inbound ledger (not injector-only).
func TestCLIDirectDeliveryTracksInboundE2E(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{
		"xo_agent":"cos",
		"agents":[
			{"name":"cos"},
			{"name":"memex"},
			{"name":"codex-harness-dev"}
		],
		"channels":[{"channel_id":"1","xo_agent":"cos","members":["codex-harness-dev"]}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}

	msg, nonce, err := inbound.AppendDispatchNonce("continue dispatch")
	if err != nil {
		t.Fatal(err)
	}
	recordDirectInboundTrack(cfg, rosterPath, "memex", "codex-harness-dev", msg)

	path, err := inbound.Path(dir, "codex-harness-dev")
	if err != nil {
		t.Fatal(err)
	}
	got := inbound.NewStore(path).Load()
	if len(got) != 1 || got[0].Nonce != nonce || got[0].Sender != "memex" {
		t.Fatalf("inbound ledger = %+v, want memex dispatch with nonce %q", got, nonce)
	}

	var reinjected []watch.Job
	hook := watch.DroppedDispatchFinishHook(dir, func(string) (string, bool, error) {
		return "done without nonce echo", true, nil
	}, func(j watch.Job) { reinjected = append(reinjected, j) }, nil)
	hook("codex-harness-dev")

	if len(reinjected) != 1 {
		t.Fatalf("miss without nonce echo: want 1 reinject, got %d", len(reinjected))
	}
	if !strings.Contains(reinjected[0].Message, "dropped-dispatch resume") {
		t.Fatalf("reinject message = %q", reinjected[0].Message)
	}
}

func TestCLIDirectDeliverySkipsCoordinatorInbound(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"xo_agent":"cos","agents":[{"name":"cos"},{"name":"memex"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	msg, _, err := inbound.AppendDispatchNonce("nudge")
	if err != nil {
		t.Fatal(err)
	}
	recordDirectInboundTrack(cfg, rosterPath, "memex", "cos", msg)
	path, _ := inbound.Path(dir, "cos")
	if len(inbound.NewStore(path).Load()) != 0 {
		t.Fatal("coordinator inbound must not be tracked on CLI path")
	}
}
