package dispatch

import (
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/messagebuffer"
)

func TestLookupNonceShowsBufferedThenPulled(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("new pull path")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := messagebuffer.Enqueue(dir, "xo", "build", msg, messagebuffer.EnqueueOptions{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if got := LookupNonce(dir, nonce, now); got.Disposition != DispositionBuffered {
		t.Fatalf("before pull = %+v", got)
	}
	if _, err := messagebuffer.Pull(dir, "build", now); err != nil {
		t.Fatal(err)
	}
	if got := LookupNonce(dir, nonce, now); got.Disposition != DispositionPulled {
		t.Fatalf("after pull = %+v", got)
	}
}

func TestLookupNonceSelectsNewestForwardedBufferHop(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("forward this body verbatim")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 7, 1, 0, 0, 0, time.UTC)
	if _, _, err := messagebuffer.Enqueue(dir, "xo", "alpha", msg, messagebuffer.EnqueueOptions{
		ID: "older", EnqueuedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := messagebuffer.Enqueue(dir, "xo", "zulu", msg, messagebuffer.EnqueueOptions{
		ID: "current", EnqueuedAt: base.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	got := LookupNonce(dir, nonce, base.Add(2*time.Minute))
	if got.Recipient != "zulu" || got.ID != "current" {
		t.Fatalf("status selected stale filename-first hop: %+v", got)
	}
}
