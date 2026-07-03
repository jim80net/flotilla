package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRelayQueueStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-relay-queue.json")
	q := newRelayQueueStore(path)
	j := Job{
		MessageID:     "1000001",
		Agent:         "cos",
		Message:       "status?",
		Kind:          "relay",
		OriginChannel: "C1",
		deferrals:     3,
		enqueuedAt:    time.Date(2026, 7, 3, 5, 0, 0, 0, time.UTC),
	}
	q.upsert(j)
	got := q.load()
	if len(got) != 1 {
		t.Fatalf("load len = %d, want 1", len(got))
	}
	if got[0].MessageID != j.MessageID || got[0].deferrals != 3 || got[0].Agent != "cos" {
		t.Fatalf("loaded = %+v, want round-trip of %+v", got[0], j)
	}
	q.remove("1000001")
	if len(q.load()) != 0 {
		t.Fatal("remove should empty queue")
	}
}

func TestReplayRelayQueueEnqueuesPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-relay-queue.json")
	q := newRelayQueueStore(path)
	q.upsert(Job{MessageID: "99", Agent: "xo", Message: "hi", Kind: "relay", deferrals: 10})

	var count deliveredCount
	in := NewInjector(func(string, string) error {
		count.inc()
		return nil
	}, 4)
	in.Start()
	n := ReplayRelayQueue(in, path)
	deadline := time.Now().Add(500 * time.Millisecond)
	for count.get() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	in.Stop()
	if n != 1 {
		t.Fatalf("replay count = %d, want 1", n)
	}
	if count.get() != 1 {
		t.Fatalf("delivered = %d, want 1 replayed job processed", count.get())
	}
	if len(q.load()) != 1 {
		t.Fatal("entry remains on disk until explicit remove on confirm path")
	}
}

func TestRelayQueueUpsertSkipsDeferralsOnlyBump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.json")
	q := newRelayQueueStore(path)
	base := time.Date(2026, 7, 3, 5, 0, 0, 0, time.UTC)
	j := Job{MessageID: "1", Agent: "xo", Message: "hi", Kind: "relay", deferrals: 1, enqueuedAt: base, lastStaleAlert: base}
	q.upsert(j)
	afterFirst, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	j.deferrals = 99
	q.upsert(j)
	afterSecond, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterFirst) != string(afterSecond) {
		t.Fatalf("deferrals-only bump should not rewrite queue file\nfirst:  %s\nsecond: %s", afterFirst, afterSecond)
	}
}

func TestRelayQueueCorruptPreservedOnUpsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "q.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := newRelayQueueStore(path)
	q.upsert(Job{MessageID: "1", Agent: "xo", Message: "hi", Kind: "relay", enqueuedAt: time.Now()})
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var hasCorrupt, hasQueue bool
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt-") {
			hasCorrupt = true
		}
		if e.Name() == "q.json" {
			hasQueue = true
		}
	}
	if !hasCorrupt {
		t.Fatal("corrupt queue should be renamed to a .corrupt sidecar")
	}
	if !hasQueue {
		t.Fatal("new queue file should exist after upsert")
	}
}

func TestRelayQueueIgnoresCorruptEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.json")
	if err := os.WriteFile(path, []byte(`{"pending":[{"message_id":"","agent":"xo","message":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(newRelayQueueStore(path).load()) != 0 {
		t.Fatal("invalid entries should be skipped")
	}
}
