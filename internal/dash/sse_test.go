package dash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHubFanout: a registered client receives a broadcast; an unregistered one
// does not.
func TestHubFanout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHub()
	go h.run(ctx)

	c := &sseClient{events: make(chan string, 4)}
	if !h.add(c) {
		t.Fatal("add returned false under the cap")
	}
	if got := h.count(); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
	h.emit("tick")
	select {
	case msg := <-c.events:
		if msg != "tick" {
			t.Errorf("got %q, want tick", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("client did not receive the broadcast")
	}
}

// TestHubDeregister: a removed client is dropped and its channel closed; it no
// longer receives broadcasts.
func TestHubDeregister(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHub()
	go h.run(ctx)

	c := &sseClient{events: make(chan string, 4)}
	h.add(c)
	h.remove(c)
	if got := h.count(); got != 0 {
		t.Fatalf("count after remove = %d, want 0", got)
	}
	// The events channel must be closed on deregister (the handler's read loop
	// relies on this to exit).
	if _, ok := <-c.events; ok {
		t.Error("client channel should be closed after remove")
	}
}

// TestHubDropsSlowClient: a client whose buffer is full is dropped on broadcast,
// never blocking the hub.
func TestHubDropsSlowClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHub()
	go h.run(ctx)

	// Unbuffered channel with no reader → the non-blocking send misses and the
	// client is dropped.
	slow := &sseClient{events: make(chan string)}
	h.add(slow)
	h.emit("x")
	// The hub should have removed it; give run a moment to process.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if h.count() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("slow client was not dropped from the hub")
}

// TestHubCap: the connection cap refuses clients beyond maxSSEClients.
func TestHubCap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHub()
	go h.run(ctx)
	for i := 0; i < maxSSEClients; i++ {
		if !h.add(&sseClient{events: make(chan string, 1)}) {
			t.Fatalf("add %d refused under the cap", i)
		}
	}
	if h.add(&sseClient{events: make(chan string, 1)}) {
		t.Error("add should refuse a client over the cap")
	}
}

// TestHubContextCloses: cancelling the context closes all client channels.
func TestHubContextCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := newHub()
	go h.run(ctx)
	c := &sseClient{events: make(chan string, 1)}
	h.add(c)
	cancel()
	select {
	case _, ok := <-c.events:
		if ok {
			t.Error("expected a closed channel after ctx cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("client channel not closed after ctx cancel")
	}
}

// --- file signature change detection ---

func TestFileSigsChange(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap.json")
	ledger := filepath.Join(dir, "ledger.md")
	backlog := filepath.Join(dir, "backlog.md")
	paths := []string{snap, ledger, backlog}

	// All absent initially.
	s0 := fileSigs(paths)
	if s0 != fileSigs(paths) {
		t.Fatal("absent signature should be stable")
	}

	// Writing the snapshot changes the signature.
	if err := os.WriteFile(snap, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	s1 := fileSigs(paths)
	if s1 == s0 {
		t.Fatal("writing a file must change the signature")
	}

	// A size change (same path) changes the signature.
	if err := os.WriteFile(snap, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	s2 := fileSigs(paths)
	if s2 == s1 {
		t.Error("a size change must change the signature")
	}

	// Touching the ledger changes the combined signature too.
	if err := os.WriteFile(ledger, []byte("l"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fileSigs(paths) == s2 {
		t.Error("ledger change must change the combined signature")
	}
}

// TestPollEmitsOnChange: the poller emits an SSE update when a watched file
// changes, and a connected client receives it.
func TestPollEmitsOnChange(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.hub.run(ctx)
	go srv.poll(ctx)

	c := &sseClient{events: make(chan string, 4)}
	if !srv.hub.add(c) {
		t.Fatal("could not register client")
	}

	// Change the snapshot file → the poller should detect (mtime,size) change.
	snapPath := filepath.Join(dir, "flotilla-detector-state.json")
	time.Sleep(50 * time.Millisecond) // let the poller capture the initial (absent) sigs
	if err := os.WriteFile(snapPath, []byte(`{"desk_states":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.events:
		// got an update — success
	case <-time.After(3 * time.Second):
		t.Fatal("poller did not emit an update on a file change")
	}
}
