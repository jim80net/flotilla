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
	events, err := DeliveryStages(dir, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Count != 1000 || !events[0].At.Equal(start) || !events[0].LastAt.Equal(start.Add(999*time.Second)) {
		t.Fatalf("coalesced events=%+v", events)
	}
	info, err := os.Stat(stagesPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 2048 {
		t.Fatalf("coalesced ledger grew to %d bytes", info.Size())
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
