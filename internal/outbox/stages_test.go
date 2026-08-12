package outbox

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
)

func TestDeliveryStagesQueuedRefusedSubmittedAreDurable(t *testing.T) {
	dir := t.TempDir()
	id, _, err := Enqueue(dir, "sender", "recipient", "body"+inbound.FormatDispatchFooter("flotilla-dispatch-deadbeef"))
	if err != nil {
		t.Fatal(err)
	}
	e := NewStore(stagesPath(dir)) // only to make accidental path reuse obvious
	_ = e
	entry := NewStore(stageTestPath(t, dir, "sender")).Load()[0]
	if attempted, err := AttemptCurrent(dir, entry, func() error { return errors.New("classifier=wedge") }); !attempted || err == nil {
		t.Fatalf("refused attempted=%v err=%v", attempted, err)
	}
	if attempted, err := AttemptCurrent(dir, entry, func() error { return nil }); !attempted || err != nil {
		t.Fatalf("submitted attempted=%v err=%v", attempted, err)
	}
	events, err := DeliveryStages(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	want := []DeliveryStage{StageQueued, StageAttemptedRefused, StageSubmitted}
	if len(events) != len(want) {
		t.Fatalf("events=%+v", events)
	}
	for i := range want {
		if events[i].Stage != want[i] {
			t.Fatalf("stage[%d]=%s want %s", i, events[i].Stage, want[i])
		}
	}
	if events[1].Evidence != "classifier=wedge" {
		t.Fatalf("evidence=%q", events[1].Evidence)
	}
	if events[2].Nonce != "flotilla-dispatch-deadbeef" {
		t.Fatalf("nonce=%q", events[2].Nonce)
	}
}

func TestAttemptedRefusalsCoalesceToBoundedEvidence(t *testing.T) {
	dir := t.TempDir()
	e := Entry{ID: "wedged", Sender: "a", Recipient: "b", Message: "body"}
	start := time.Unix(1, 0).UTC()
	for i := 0; i < 1000; i++ {
		if err := RecordStage(dir, e, StageAttemptedRefused, "classifier=wedge", start.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := RecordStage(dir, e, StageFailed, "terminal", start.Add(1000*time.Second)); err != nil {
		t.Fatal(err)
	}
	events, err := DeliveryStages(dir, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Count != 1000 || !events[0].At.Equal(start) || !events[0].LastAt.Equal(start.Add(999*time.Second)) {
		t.Fatalf("coalesced events=%+v", events)
	}
	metric := stageWriteMetrics(stagesPath(dir))
	if metric.writes > 18 || metric.bytes > 18*4096 {
		t.Fatalf("write work not bounded: writes=%d bytes=%d", metric.writes, metric.bytes)
	}
	info, err := os.Stat(stagesPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 2048 {
		t.Fatalf("coalesced ledger grew to %d bytes", info.Size())
	}
}

func TestRefusalBatchRestoresAfterInitialPersistenceFailure(t *testing.T) {
	for _, phase := range []string{"lock", "write", "rename"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			path := stagesPath(dir)
			e := Entry{ID: "wedged", Sender: "a", Recipient: "b", Message: "body"}
			start := time.Unix(10, 0).UTC()
			injectStageFailure(path, phase, errors.New("injected "+phase+" failure"))
			if err := RecordStage(dir, e, StageAttemptedRefused, "first", start); err == nil {
				t.Fatal("injected first persistence unexpectedly succeeded")
			}
			if err := RecordStage(dir, e, StageAttemptedRefused, "second", start.Add(time.Second)); err != nil {
				t.Fatalf("retry persistence: %v", err)
			}
			if err := RecordStage(dir, e, StageFailed, "terminal", start.Add(2*time.Second)); err != nil {
				t.Fatalf("terminal persistence: %v", err)
			}
			assertRefusalTimeline(t, dir, e.ID, 2, start, start.Add(time.Second))
		})
	}
}

func TestRefusalBatchRestoresAfterTerminalPersistenceFailure(t *testing.T) {
	dir := t.TempDir()
	path := stagesPath(dir)
	e := Entry{ID: "wedged", Sender: "a", Recipient: "b", Message: "body"}
	start := time.Unix(20, 0).UTC()
	if err := RecordStage(dir, e, StageAttemptedRefused, "first", start); err != nil {
		t.Fatal(err)
	}
	if err := RecordStage(dir, e, StageAttemptedRefused, "second", start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	injectStageFailure(path, "rename", errors.New("injected terminal rename failure"))
	if err := RecordStage(dir, e, StageFailed, "terminal", start.Add(2*time.Second)); err == nil {
		t.Fatal("injected terminal persistence unexpectedly succeeded")
	}
	if err := RecordStage(dir, e, StageFailed, "terminal", start.Add(3*time.Second)); err != nil {
		t.Fatalf("terminal retry: %v", err)
	}
	assertRefusalTimeline(t, dir, e.ID, 2, start, start.Add(time.Second))
}

func assertRefusalTimeline(t *testing.T, dir, id string, count uint64, first, last time.Time) {
	t.Helper()
	events, err := DeliveryStages(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Stage != StageAttemptedRefused || events[1].Stage != StageFailed {
		t.Fatalf("timeline=%+v", events)
	}
	if events[0].Count != count || !events[0].At.Equal(first) || !events[0].LastAt.Equal(last) {
		t.Fatalf("refusal=%+v want count=%d first=%s last=%s", events[0], count, first, last)
	}
}

func TestSubmittedMeansTransportNotHandled(t *testing.T) {
	dir := t.TempDir()
	e := Entry{ID: "id", Sender: "a", Recipient: "b", Message: "body"}
	if err := RecordStage(dir, e, StageSubmitted, "paste+enter confirmed", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	events, _ := DeliveryStages(dir, "id")
	if len(events) != 1 || events[0].Stage != StageSubmitted {
		t.Fatalf("events=%+v", events)
	}
	// No recipient_consumed event is inferred. Handled remains separate.
}

func TestAttemptCurrentReconcilesSubmittedWithoutDuplicatePaste(t *testing.T) {
	dir := t.TempDir()
	id, _, err := Enqueue(dir, "sender", "recipient", "body")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := Path(dir, "sender")
	entry := NewStore(path).Load()[0]
	if err := RecordStage(dir, entry, StageSubmitted, "paste+enter confirmed", time.Now()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	attempted, err := AttemptCurrent(dir, entry, func() error {
		calls++
		return nil
	})
	if err != nil || !attempted {
		t.Fatalf("attempted=%v err=%v", attempted, err)
	}
	if calls != 0 {
		t.Fatalf("submission callback called %d times", calls)
	}
	if got := NewStore(path).Load(); len(got) != 0 {
		t.Fatalf("pending after reconciliation=%+v id=%s", got, id)
	}
}

func stageTestPath(t *testing.T, dir, sender string) string {
	t.Helper()
	p, err := Path(dir, sender)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
