package dash

import (
	"context"
	"os"
	"path/filepath"
	"sync"
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

// TestHubCapAtomicUnderBurst: concurrent connects must NOT overshoot the cap —
// the count is decided by run, not a check-then-register race in the caller.
func TestHubCapAtomicUnderBurst(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHub()
	go h.run(ctx)

	const burst = maxSSEClients + 32
	results := make(chan bool, burst)
	var wg sync.WaitGroup
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- h.add(&sseClient{events: make(chan string, 1)})
		}()
	}
	wg.Wait()
	close(results)
	admitted := 0
	for ok := range results {
		if ok {
			admitted++
		}
	}
	if admitted != maxSSEClients {
		t.Errorf("admitted %d clients under a burst, want exactly the cap %d (no overshoot)", admitted, maxSSEClients)
	}
	if got := h.count(); got != maxSSEClients {
		t.Errorf("hub holds %d clients, want %d", got, maxSSEClients)
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

// TestHubShutdownNoBlock: once run() exits on ctx cancel, every producer
// (add/remove/emit/count) must return promptly instead of blocking forever on a
// receiver-less channel — otherwise a handler parked mid-register, or its
// deferred remove, leaks a goroutine and stalls srv.Shutdown.
func TestHubShutdownNoBlock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := newHub()
	go h.run(ctx)
	c := &sseClient{events: make(chan string, 1)}
	h.add(c)

	cancel()
	<-h.done // run has exited and closed done

	// All producers must return well within the deadline (they select on done).
	got := make(chan struct{})
	go func() {
		h.remove(c) // the deferred-remove path
		_ = h.add(&sseClient{events: make(chan string, 1)})
		h.emit("post-shutdown") // the poller racing shutdown
		_ = h.count()
		close(got)
	}()
	select {
	case <-got:
		// success — none of the producers blocked
	case <-time.After(2 * time.Second):
		t.Fatal("a hub producer blocked after run() exited — shutdown would leak a goroutine")
	}
}

// --- file signature change detection ---

func TestFileSigsChange(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap.json")
	ledger := filepath.Join(dir, "ledger.md")
	backlog := filepath.Join(dir, "backlog.md")
	goals := filepath.Join(dir, "fleet-goals.json")
	goalsYAML := filepath.Join(dir, "fleet-goals.yaml")
	paths := []string{snap, ledger, backlog, goals, goalsYAML}

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
	s3 := fileSigs(paths)
	if s3 == s2 {
		t.Error("ledger change must change the combined signature")
	}

	// Touching the goals file changes the combined signature too (so a structural
	// goals edit pushes an SSE update the same way a snapshot/backlog change does).
	if err := os.WriteFile(goals, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	s4 := fileSigs(paths)
	if s4 == s3 {
		t.Error("goals change must change the combined signature")
	}

	// Touching the goals YAML source must change the combined signature — poll()
	// watches paths[4] (GoalsYAMLPath) and bare yaml edits must fire SSE.
	if err := os.WriteFile(goalsYAML, []byte("goals: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fileSigs(paths) == s4 {
		t.Error("goals yaml change must change the combined signature")
	}
}

// TestPollEmitsOnGoalsYAMLChange: the poller emits when only the goals YAML
// source changes (paths[4]), not just the compiled goals JSON.
func TestPollEmitsOnGoalsYAMLChange(t *testing.T) {
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

	yamlPath := filepath.Join(dir, "fleet-goals.yaml")
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(yamlPath, []byte("goals: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.events:
	case <-time.After(3 * time.Second):
		t.Fatal("poller did not emit an update on a goals yaml change")
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
