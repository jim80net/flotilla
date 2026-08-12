package dispatch

import (
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
)

func TestDispatchStatusFindsSubmittedAfterQueueRemoval(t *testing.T) {
	dir := t.TempDir()
	message := "body" + inbound.FormatDispatchFooter("flotilla-dispatch-deadbeef")
	id, _, err := outbox.Enqueue(dir, "sender", "recipient", message)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := outbox.Path(dir, "sender")
	e := outbox.NewStore(path).Load()[0]
	if attempted, err := outbox.AttemptCurrent(dir, e, func() error { return nil }); !attempted || err != nil {
		t.Fatalf("attempted=%v err=%v", attempted, err)
	}
	st := LookupNonce(dir, "flotilla-dispatch-deadbeef", time.Now())
	if st.Disposition != DispositionSubmitted || st.ID != id {
		t.Fatalf("status=%+v", st)
	}
}

func TestConsumeRecordsRecipientConsumedStage(t *testing.T) {
	dir := t.TempDir()
	message := "body" + inbound.FormatDispatchFooter("flotilla-dispatch-deadbeef")
	id, _, err := outbox.Enqueue(dir, "sender", "recipient", message)
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := Consume(dir, ConsumedEntry{Nonce: "flotilla-dispatch-deadbeef", PayloadHash: PayloadHash(message)})
	if err != nil || !inserted {
		t.Fatalf("inserted=%v err=%v", inserted, err)
	}
	events, err := outbox.DeliveryStages(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-1].Stage; got != outbox.StageRecipientConsumed {
		t.Fatalf("last stage=%s events=%+v", got, events)
	}
}
