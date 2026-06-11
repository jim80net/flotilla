package voice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fastSupervise keeps reconnect waits sub-second.
func fastSupervise() SuperviseConfig {
	return SuperviseConfig{ReconnectDelay: 5 * time.Millisecond, MaxAttempts: 3}
}

// On a session drop, Supervise tears down and reconnects with a fresh session/runner.
func TestSuperviseReconnectsOnDrop(t *testing.T) {
	var connects, cleanups int64
	var mu sync.Mutex
	losts := []chan struct{}{}

	connect := func(ctx context.Context) (Session, <-chan struct{}, func(), error) {
		atomic.AddInt64(&connects, 1)
		lost := make(chan struct{})
		mu.Lock()
		losts = append(losts, lost)
		mu.Unlock()
		return newFakeSession(), lost, func() { atomic.AddInt64(&cleanups, 1) }, nil
	}
	run := func(ctx context.Context, _ Session) { <-ctx.Done() } // block until cancelled

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Supervise(ctx, connect, run, nil, fastSupervise()) }()

	// Wait for the first connect, then simulate a drop and expect a reconnect.
	waitFor(t, func() bool { return atomic.LoadInt64(&connects) == 1 }, time.Second)
	mu.Lock()
	close(losts[0]) // drop the first session
	mu.Unlock()
	waitFor(t, func() bool { return atomic.LoadInt64(&connects) == 2 }, time.Second)
	if atomic.LoadInt64(&cleanups) < 1 {
		t.Error("dropped session was not cleaned up before reconnect")
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Supervise returned %v, want context.Canceled on shutdown", err)
	}
	if atomic.LoadInt64(&cleanups) < 2 {
		t.Error("session was not cleaned up on shutdown")
	}
}

// Shutdown (ctx cancel) stops the runner and returns cleanly with no give-up notice.
func TestSuperviseCleanShutdown(t *testing.T) {
	runStopped := make(chan struct{})
	connect := func(ctx context.Context) (Session, <-chan struct{}, func(), error) {
		return newFakeSession(), make(chan struct{}), func() {}, nil
	}
	run := func(ctx context.Context, _ Session) { <-ctx.Done(); close(runStopped) }
	notices := make(chan string, 4)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Supervise(ctx, connect, run, func(s string) { notices <- s }, fastSupervise()) }()

	time.Sleep(20 * time.Millisecond) // let it connect + start the runner
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("returned %v, want context.Canceled", err)
	}
	select {
	case <-runStopped:
	case <-time.After(time.Second):
		t.Fatal("runner was not stopped on shutdown")
	}
	select {
	case n := <-notices:
		t.Errorf("clean shutdown emitted a notice: %q", n)
	default:
	}
}

// After MaxAttempts consecutive connect failures, Supervise gives up with one notice.
func TestSuperviseGivesUpAfterMaxAttempts(t *testing.T) {
	var connects int64
	connErr := errors.New("join failed")
	connect := func(ctx context.Context) (Session, <-chan struct{}, func(), error) {
		atomic.AddInt64(&connects, 1)
		return nil, nil, nil, connErr
	}
	notices := make(chan string, 1)

	err := Supervise(context.Background(), connect, func(context.Context, Session) {}, func(s string) { notices <- s }, fastSupervise())
	if !errors.Is(err, connErr) {
		t.Errorf("returned %v, want the connect error", err)
	}
	if got := atomic.LoadInt64(&connects); got != 3 { // MaxAttempts
		t.Errorf("connected %d times, want 3 (MaxAttempts)", got)
	}
	msg, ok := recvWithin(t, notices, time.Second)
	if !ok || !contains([]string{msg}, msg) {
		t.Fatal("no give-up notice")
	}
}

// A successful connect resets the consecutive-failure counter (a later failure run gets a
// fresh budget rather than inheriting earlier transient failures).
func TestSuperviseResetsFailureCountOnSuccess(t *testing.T) {
	var connects int64
	connErr := errors.New("flaky")
	connect := func(ctx context.Context) (Session, <-chan struct{}, func(), error) {
		n := atomic.AddInt64(&connects, 1)
		switch n {
		case 1:
			return nil, nil, nil, connErr // 1 fail
		case 2:
			lost := make(chan struct{})
			close(lost) // connects, then immediately drops → reconnect
			return newFakeSession(), lost, func() {}, nil
		default:
			return nil, nil, nil, connErr // fails again, but counter was reset by #2
		}
	}
	// MaxAttempts=3: without reset, fails at #1 then #3,#4 (3 consecutive) — but #2 succeeded
	// between, so the counter resets and we need 3 MORE consecutive fails after #2.
	cfg := SuperviseConfig{ReconnectDelay: time.Millisecond, MaxAttempts: 3}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = Supervise(ctx, connect, func(context.Context, Session) {}, nil, cfg)
	// 1 fail + 1 success + 3 fails = 5 connect calls before give-up.
	if got := atomic.LoadInt64(&connects); got < 5 {
		t.Errorf("connected %d times, want ≥5 (success reset the failure counter)", got)
	}
}

func waitFor(t *testing.T, cond func() bool, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within " + d.String())
}
