package tracker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestListCacheReusesNormalizedFilterAndReturnsDefensiveCopies(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`[{
		"number":1,"title":"cached","state":"OPEN",
		"labels":[{"name":"bug"}],"comments":[{"body":"original"}]
	}]`)}
	g := newFakeTracker(t, f)
	now := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }

	first, err := g.List(ctx(), ListFilter{State: " OPEN ", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	first[0].Title = "mutated"
	first[0].Labels[0].Name = "mutated"
	first[0].Comments[0].Body = "mutated"

	second, err := g.List(ctx(), ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("runner calls = %d, want one cached call", f.calls)
	}
	if second[0].Title != "cached" || second[0].Labels[0].Name != "bug" || second[0].Comments[0].Body != "original" {
		t.Fatalf("cached result was mutated by caller: %+v", second[0])
	}

	now = now.Add(listCacheTTL + time.Nanosecond)
	if _, err := g.List(ctx(), ListFilter{}); err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 {
		t.Fatalf("runner calls after TTL = %d, want 2", f.calls)
	}
}

func TestListCacheDoesNotCacheFailures(t *testing.T) {
	f := &fakeRunner{stderr: []byte("temporary failure"), err: errors.New("exit status 1")}
	g := newFakeTracker(t, f)
	if _, err := g.List(ctx(), ListFilter{}); !errors.Is(err, ErrGH) {
		t.Fatalf("first List error = %v, want ErrGH", err)
	}
	f.stderr, f.err, f.stdout = nil, nil, []byte(`[]`)
	if _, err := g.List(ctx(), ListFilter{}); err != nil {
		t.Fatalf("retry List: %v", err)
	}
	if f.calls != 2 {
		t.Fatalf("runner calls = %d, want failure + retry", f.calls)
	}
}

func TestListCoalescesIdenticalInFlightCalls(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	run := func(_ context.Context, _ []string, _ []byte) ([]byte, []byte, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return []byte(`[{"number":1,"title":"one","state":"OPEN"}]`), nil, nil
	}
	g, err := newGH("jim80net/flotilla", run)
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := g.List(context.Background(), ListFilter{State: "all", Limit: 200, IncludeBody: true})
		firstDone <- err
	}()
	<-started

	waitCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := g.List(waitCtx, ListFilter{State: "ALL", Limit: 999, IncludeBody: true}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("coalesced waiter error = %v, want its context deadline", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("in-flight runner calls = %d, want 1", got)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if _, err := g.List(ctx(), ListFilter{State: "all", Limit: 200, IncludeBody: true}); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("cached runner calls = %d, want 1", got)
	}
}

func TestSuccessfulWriteDetachesStaleFlightAndInvalidatesCache(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var listCalls atomic.Int32
	run := func(_ context.Context, args []string, _ []byte) ([]byte, []byte, error) {
		if len(args) >= 2 && args[0] == "issue" && args[1] == "list" {
			n := listCalls.Add(1)
			if n == 1 {
				close(firstStarted)
				<-releaseFirst
			}
			return []byte(fmt.Sprintf(`[{"number":%d,"title":"generation-%d","state":"OPEN"}]`, n, n)), nil, nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "comment" {
			return []byte("ok"), nil, nil
		}
		return nil, nil, fmt.Errorf("unexpected args: %s", strings.Join(args, " "))
	}
	g, err := newGH("jim80net/flotilla", run)
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan []Issue, 1)
	go func() {
		issues, _ := g.List(context.Background(), ListFilter{})
		firstDone <- issues
	}()
	<-firstStarted

	if err := g.Comment(ctx(), 1, "refresh the cache"); err != nil {
		t.Fatal(err)
	}
	second, err := g.List(ctx(), ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Number != 2 || listCalls.Load() != 2 {
		t.Fatalf("post-write List = %+v, calls=%d; want fresh generation 2", second, listCalls.Load())
	}

	close(releaseFirst)
	if first := <-firstDone; first[0].Number != 1 {
		t.Fatalf("pre-write caller = %+v, want its generation 1 result", first)
	}
	third, err := g.List(ctx(), ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if third[0].Number != 2 || listCalls.Load() != 2 {
		t.Fatalf("stale flight replaced cache: third=%+v calls=%d", third, listCalls.Load())
	}
}
