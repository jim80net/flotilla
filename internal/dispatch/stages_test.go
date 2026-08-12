package dispatch

import (
	"fmt"
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
	inserted, err := Consume(dir, ConsumedEntry{Nonce: "flotilla-dispatch-deadbeef", PayloadHash: PayloadHash(message), Sender: "sender", Recipient: "recipient"})
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

func TestRecipientConsumedBindsExactEdgeInEitherInsertionOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "A then B", true: "B then A"}[reverse], func(t *testing.T) {
			dir := t.TempDir()
			nonce := "flotilla-dispatch-deadbeef"
			messageA := "payload-a" + inbound.FormatDispatchFooter(nonce)
			messageB := "payload-b" + inbound.FormatDispatchFooter(nonce)
			type edge struct{ sender, recipient, message string }
			edges := []edge{{"sender-a", "recipient-a", messageA}, {"sender-b", "recipient-b", messageB}}
			if reverse {
				edges[0], edges[1] = edges[1], edges[0]
			}
			ids := make(map[string]string)
			for _, edge := range edges {
				id, _, err := outbox.Enqueue(dir, edge.sender, edge.recipient, edge.message)
				if err != nil {
					t.Fatal(err)
				}
				ids[edge.sender] = id
			}
			inserted, err := Consume(dir, ConsumedEntry{Nonce: nonce, PayloadHash: PayloadHash(messageA),
				Sender: "sender-a", Recipient: "recipient-a", Reason: ReasonDurableAck})
			if err != nil || !inserted {
				t.Fatalf("inserted=%v err=%v", inserted, err)
			}
			for sender, id := range ids {
				events, err := outbox.DeliveryStages(dir, id)
				if err != nil {
					t.Fatal(err)
				}
				consumed := false
				for _, event := range events {
					consumed = consumed || event.Stage == outbox.StageRecipientConsumed
				}
				if want := sender == "sender-a"; consumed != want {
					t.Fatalf("sender=%s consumed=%v want=%v events=%+v", sender, consumed, want, events)
				}
			}
		})
	}
}

func TestDispatchStatusMakesSameNonceMultiEdgeAmbiguous(t *testing.T) {
	dir := t.TempDir()
	nonce := "flotilla-dispatch-deadbeef"
	for _, sender := range []string{"sender-a", "sender-b"} {
		message := sender + inbound.FormatDispatchFooter(nonce)
		_, _, err := outbox.Enqueue(dir, sender, "recipient", message)
		if err != nil {
			t.Fatal(err)
		}
		path, _ := outbox.Path(dir, sender)
		e := outbox.NewStore(path).Load()[0]
		if attempted, err := outbox.AttemptCurrent(dir, e, func() error { return nil }); !attempted || err != nil {
			t.Fatalf("sender=%s attempted=%v err=%v", sender, attempted, err)
		}
	}
	st := LookupNonce(dir, nonce, time.Now())
	if st.Disposition != DispositionUnknown || st.Detail != "nonce is ambiguous across 2 submitted edges; specify edge identity" {
		t.Fatalf("status=%+v", st)
	}
}

func TestRecipientConsumedStageSurvivesHotRegistryEviction(t *testing.T) {
	dir := t.TempDir()
	nonce := "flotilla-dispatch-deadbeef"
	message := "payload" + inbound.FormatDispatchFooter(nonce)
	id, _, err := outbox.Enqueue(dir, "sender", "recipient", message)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(dir)
	if inserted, err := reg.Consume(ConsumeFromInbound(nonce, message, ReasonDurableAck, "sender", "recipient")); err != nil || !inserted {
		t.Fatalf("inserted=%v err=%v", inserted, err)
	}
	hot := consumedFile{Entries: make([]ConsumedEntry, maxConsumedEntries)}
	for i := range hot.Entries {
		hot.Entries[i] = ConsumedEntry{Nonce: fmt.Sprintf("flotilla-dispatch-%08x", i+1), PayloadHash: fmt.Sprintf("%032x", i+1), Reason: ReasonManual}
	}
	if err := reg.save(hot); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.LookupNonce(nonce); ok {
		t.Fatal("hot registry unexpectedly retained evicted nonce")
	}
	events, err := outbox.DeliveryStages(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-1].Stage; got != outbox.StageRecipientConsumed {
		t.Fatalf("append-only stage lost after registry eviction: %+v", events)
	}
}
