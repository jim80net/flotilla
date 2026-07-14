package dispatch

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
)

func TestConsumeIdempotent_SameNonce(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	e := ConsumedEntry{
		Nonce:       "flotilla-dispatch-aabbccdd",
		PayloadHash: "hash1",
		Reason:      ReasonTurnFinalAck,
		Sender:      "xo",
		Recipient:   "desk",
	}
	ins, err := reg.Consume(e)
	if err != nil || !ins {
		t.Fatalf("first consume: inserted=%v err=%v", ins, err)
	}
	ins2, err := reg.Consume(e)
	if err != nil {
		t.Fatal(err)
	}
	if ins2 {
		t.Fatal("second consume with same nonce must be idempotent (inserted=false)")
	}
	// Different hash, same nonce — still idempotent (nonce authoritative).
	e.PayloadHash = "hash2-different"
	ins3, err := reg.Consume(e)
	if err != nil || ins3 {
		t.Fatalf("same nonce different hash: inserted=%v err=%v, want false", ins3, err)
	}
	if got := len(reg.Load()); got != 1 {
		t.Fatalf("entries = %d, want 1", got)
	}
}

func TestIsConsumed_NonceAuthoritative(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	_, err := reg.Consume(ConsumedEntry{
		Nonce: "flotilla-dispatch-11223344", PayloadHash: "aaa", Reason: ReasonMerged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reg.IsConsumed("flotilla-dispatch-11223344", "totally-different-hash") {
		t.Fatal("nonce match must suppress even when payload hash differs")
	}
	if reg.IsConsumed("flotilla-dispatch-99999999", "aaa") {
		t.Fatal("different nonce must not match on hash alone when query has a nonce")
	}
}

func TestIsConsumed_HashOnlyWhenNonceEmpty(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	_, err := reg.Consume(ConsumedEntry{PayloadHash: "deadbeefcafebabe", Reason: ReasonManual})
	if err != nil {
		t.Fatal(err)
	}
	if !reg.IsConsumed("", "deadbeefcafebabe") {
		t.Fatal("hash-only consume must match hash-only query")
	}
	if reg.IsConsumed("flotilla-dispatch-abcdabcd", "deadbeefcafebabe") {
		t.Fatal("nonce query must not match hash-only entry (collision safety)")
	}
}

func TestPayloadHash_StripsFooter(t *testing.T) {
	body := "Implement feature X for the fleet board"
	withFooter, _, err := inbound.AppendDispatchNonce(body)
	if err != nil {
		t.Fatal(err)
	}
	if PayloadHash(body) != PayloadHash(withFooter) {
		t.Fatal("PayloadHash must ignore #472 footer so reinject stamps do not fork identity")
	}
}

func TestConsume_PersistsAcrossRegistryHandles(t *testing.T) {
	dir := t.TempDir()
	_, err := NewRegistry(dir).Consume(ConsumedEntry{
		Nonce: "flotilla-dispatch-persist01", PayloadHash: "p1", Reason: ReasonTurnFinalAck,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := ConsumedPath(dir)
	if path != filepath.Join(dir, "flotilla-dispatch-consumed.json") {
		t.Fatalf("path = %q", path)
	}
	if !NewRegistry(dir).IsConsumed("flotilla-dispatch-persist01", "p1") {
		t.Fatal("fresh registry handle must see durable consume")
	}
}

func TestConsume_CapsGrowth(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	for i := 0; i < 3; i++ {
		_, err := reg.Consume(ConsumedEntry{
			Nonce:       fmt.Sprintf("flotilla-dispatch-cap%05d", i),
			PayloadHash: PayloadHash(fmt.Sprintf("msg-%d", i)),
			Reason:      ReasonManual,
			ConsumedAt:  time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := len(reg.Load()); n != 3 {
		t.Fatalf("load = %d", n)
	}
}

func TestConsumeCoordinatorRecipient_SettlesNonceAtSendTime707(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("coordinate the thing")
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := ConsumeCoordinatorRecipient(dir, "desk", "xo", msg)
	if err != nil || !inserted {
		t.Fatalf("ConsumeCoordinatorRecipient = (%v, %v), want inserted", inserted, err)
	}
	e, ok := NewRegistry(dir).LookupNonce(nonce)
	if !ok || e.Reason != ReasonCoordinatorRecipient || e.Sender != "desk" || e.Recipient != "xo" {
		t.Fatalf("consumed entry = %+v, ok=%v", e, ok)
	}
	// dispatch-status must resolve the nonce (previously: unknown).
	st := LookupNonce(dir, nonce, time.Now())
	if st.Disposition != DispositionConsumed || st.Reason != ReasonCoordinatorRecipient {
		t.Fatalf("status = %+v, want consumed reason=coordinator-recipient", st)
	}
	// Idempotent on the daemon-sweep + CLI double-track path.
	if again, err := ConsumeCoordinatorRecipient(dir, "desk", "xo", msg); err != nil || again {
		t.Fatalf("second consume = (%v, %v), want no-op", again, err)
	}
}

func TestConsumeCoordinatorRecipient_NoNonceRecordsNothing707(t *testing.T) {
	dir := t.TempDir()
	if inserted, err := ConsumeCoordinatorRecipient(dir, "desk", "xo", "plain message, no footer"); err != nil || inserted {
		t.Fatalf("no-nonce consume = (%v, %v), want nothing recorded", inserted, err)
	}
	if entries := NewRegistry(dir).Load(); len(entries) != 0 {
		t.Fatalf("registry after no-nonce send = %+v, want empty", entries)
	}
}
