package messagebuffer

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
)

func TestPullIsIdempotentAndRecordsArrival(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("stop before the next merge")
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := Enqueue(dir, "xo", "build", msg, EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 6, 1, 2, 3, 0, time.UTC)
	first, err := Pull(dir, "build", now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Pull(dir, "build", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID != e.ID || second[0].ID != e.ID {
		t.Fatalf("pulls = %+v / %+v", first, second)
	}
	if first[0].Nonce != nonce || first[0].PulledAt == nil || !first[0].PulledAt.Equal(now) || !second[0].PulledAt.Equal(now) {
		t.Fatalf("arrival stamp changed or nonce missing: %+v / %+v", first[0], second[0])
	}
}

func TestPerSenderOrderAndSupersessionVisible(t *testing.T) {
	dir := t.TempDir()
	old, _, err := Enqueue(dir, "xo", "build", "merge when green", EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	newer, _, err := Enqueue(dir, "xo", "build", "stop; authorization withdrawn", EnqueueOptions{Supersedes: []string{old.ID}})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := Enqueue(dir, "pm", "build", "status?", EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Pull(dir, "build", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].SenderSequence != 1 || got[1].SenderSequence != 2 || got[2].SenderSequence != 1 {
		t.Fatalf("sender order = %+v", got)
	}
	if got[0].SupersededBy != newer.ID || len(got[1].Supersedes) != 1 || got[1].Supersedes[0] != old.ID || other.SenderSequence != 1 {
		t.Fatalf("supersession relation missing: %+v", got)
	}
}

func TestAckHidesWithoutDeletingHistory(t *testing.T) {
	dir := t.TempDir()
	e, _, err := Enqueue(dir, "xo", "build", "do it", EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AckID(dir, "build", e.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, err := Pull(dir, "build", time.Now()); err != nil || len(got) != 0 {
		t.Fatalf("post-ack pull = %+v err=%v", got, err)
	}
	path, _ := Path(dir, "build")
	if got := NewStore(path).Load(); len(got) != 1 || got[0].AcknowledgedAt == nil {
		t.Fatalf("audit history = %+v", got)
	}
}

func TestMigrationPreservesFiveThousandDeferralsAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path, _ := outbox.Path(dir, "xo")
	_, _, err := outbox.NewStore(path).Insert(outbox.Entry{
		ID: "stuck", Sender: "xo", Recipient: "build", Message: "still needed",
		Deferrals: 5000, EnqueuedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := MigrateOutboxes(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MigrateOutboxes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Migrated != 1 || second.Migrated != 0 || len(outbox.NewStore(path).Load()) != 0 {
		t.Fatalf("migration results = %+v / %+v", first, second)
	}
	bufferPath, _ := Path(dir, "build")
	got := NewStore(bufferPath).Load()
	if len(got) != 1 || got[0].ID != "stuck" || got[0].LegacyDeferrals != 5000 || got[0].MigratedFrom != "sender-outbox" {
		t.Fatalf("migrated entry = %+v", got)
	}
}

func TestMigrationDoesNotCountFailedLegacyRemoval(t *testing.T) {
	dir := t.TempDir()
	path, _ := outbox.Path(dir, "xo")
	_, _, err := outbox.NewStore(path).Insert(outbox.Entry{
		ID: "still-legacy", Sender: "xo", Recipient: "build", Message: "do not double deliver",
		EnqueuedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := migrateOutboxes(dir, func(_, _ string) error {
		return fmt.Errorf("disk refused removal")
	})
	if err == nil || !strings.Contains(err.Error(), "remove legacy row") {
		t.Fatalf("migration error = %v, want removal failure", err)
	}
	if result.Migrated != 0 || len(result.Recipients) != 0 {
		t.Fatalf("failed removal reported migration: %+v", result)
	}
	if got := outbox.NewStore(path).Load(); len(got) != 1 || got[0].ID != "still-legacy" {
		t.Fatalf("legacy row after failed removal = %+v", got)
	}
	bufferPath, _ := Path(dir, "build")
	if got := NewStore(bufferPath).Load(); len(got) != 1 || got[0].ID != "still-legacy" {
		t.Fatalf("insert-before-remove buffer copy = %+v", got)
	}
}

func TestConcurrentEnqueueAllocatesGaplessSenderOrder(t *testing.T) {
	dir := t.TempDir()
	const count = 24
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := Enqueue(dir, "xo", "build", fmt.Sprintf("message %d", i), EnqueueOptions{})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	path, _ := Path(dir, "build")
	got := NewStore(path).Load()
	if len(got) != count {
		t.Fatalf("entries=%d", len(got))
	}
	seq := make([]int, 0, len(got))
	for _, e := range got {
		seq = append(seq, int(e.SenderSequence))
	}
	sort.Ints(seq)
	for i, n := range seq {
		if n != i+1 {
			t.Fatalf("sequence=%v", seq)
		}
	}
}

func TestCancelAppendsPullVisibleStop(t *testing.T) {
	dir := t.TempDir()
	target, _, err := Enqueue(dir, "xo", "build", "merge it", EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cancel, gotTarget, err := Cancel(dir, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTarget.ID != target.ID || len(cancel.Supersedes) != 1 || cancel.Supersedes[0] != target.ID || cancel.Nonce == "" {
		t.Fatalf("cancel=%+v target=%+v", cancel, gotTarget)
	}
	pulled, err := Pull(dir, "build", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(pulled) != 2 || pulled[0].SupersededBy != cancel.ID {
		t.Fatalf("pull after cancel=%+v", pulled)
	}
}
