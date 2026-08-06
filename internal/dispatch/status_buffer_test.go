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
