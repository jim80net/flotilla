package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/surface"
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
