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

func TestConsumeCoordinatorRecipient_QuotedNonceSettlesNothing707(t *testing.T) {
	dir := t.TempDir()
	// An upward report QUOTING a live dispatch's nonce in prose carries no #472
	// footer of its own (AppendDispatchNonce reuses the quoted nonce without
	// stamping). Settling it would disable the target desk's supervision.
	report := "status: dispatched flotilla-dispatch-aaaa1111 to the desk; awaiting its ack"
	if inserted, err := ConsumeCoordinatorRecipient(dir, "xo", "cos", report); err != nil || inserted {
		t.Fatalf("quoted-nonce consume = (%v, %v), want nothing recorded", inserted, err)
	}
	if entries := NewRegistry(dir).Load(); len(entries) != 0 {
		t.Fatalf("registry after quoted-nonce report = %+v, want empty", entries)
	}
}

func TestSettlesInboundRow_CoordinatorEntryScopedToItsOwnHop707(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("do the thing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConsumeCoordinatorRecipient(dir, "cos", "xo", msg); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(dir)
	// The coordinator hop is settled…
	if !reg.SettlesInboundRow(nonce, PayloadHash(msg), "xo") {
		t.Fatal("coordinator's own hop must read settled")
	}
	// …but a desk holding the SAME forwarded dispatch text stays supervised.
	if reg.SettlesInboundRow(nonce, PayloadHash(msg), "backend") {
		t.Fatal("coordinator settlement must not settle the desk's row")
	}
	// A real settlement for the desk still works and takes lookup preference.
	if _, err := reg.Consume(ConsumeFromInbound(nonce, msg, ReasonDurableAck, "xo", "backend")); err != nil {
		t.Fatal(err)
	}
	if !reg.SettlesInboundRow(nonce, PayloadHash(msg), "backend") {
		t.Fatal("desk durable ack must settle the desk's row")
	}
	if e, ok := reg.LookupNonce(nonce); !ok || e.Reason != ReasonDurableAck {
		t.Fatalf("lookup preference = %+v, want the desk's durable ack", e)
	}
}

// #707 N1: a coordinator-hop settlement must not mask a LIVE desk copy of the
// same nonce from the manual status probe.
func TestStatusPrefersLiveInboundOverCoordinatorHop707(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("forwarded work still pending on a desk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConsumeCoordinatorRecipient(dir, "cos", "xo", msg); err != nil {
		t.Fatal(err)
	}
	// Hop entry alone → consumed with the hop reason.
	if st := LookupNonce(dir, nonce, time.Now()); st.Disposition != DispositionConsumed || st.Reason != ReasonCoordinatorRecipient {
		t.Fatalf("hop-only status = %+v", st)
	}
	// A live desk row for the same nonce takes precedence over the hop entry…
	if err := inbound.Record(dir, inbound.Entry{
		ID: "fwd-1", Sender: "xo", Recipient: "backend", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if st := LookupNonce(dir, nonce, time.Now()); st.Disposition != DispositionDelivered || st.Recipient != "backend" {
		t.Fatalf("status with live desk row = %+v, want delivered to backend", st)
	}
	// …and a stale desk row surfaces as undelivered, not masked as consumed.
	stale := LookupNonce(dir, nonce, time.Now().Add(UndeliveredInboundAge+time.Minute))
	if stale.Disposition != DispositionUndelivered {
		t.Fatalf("stale-row status = %+v, want undelivered", stale)
	}
	// A REAL settlement still wins over everything.
	if _, err := NewRegistry(dir).Consume(ConsumeFromInbound(nonce, msg, ReasonDurableAck, "xo", "backend")); err != nil {
		t.Fatal(err)
	}
	if st := LookupNonce(dir, nonce, time.Now()); st.Disposition != DispositionConsumed || st.Reason != ReasonDurableAck {
		t.Fatalf("status after real settle = %+v, want consumed durable-ack", st)
	}
}

// #707 nit: the undelivered sweeps stay recipient-scoped end-to-end — a hop
// entry neither hides a stale desk row from the scan nor lets the reconcile
// sweep scrub it.
func TestUndeliveredSweepsIgnoreCoordinatorHopEntry707(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("stale forwarded work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConsumeCoordinatorRecipient(dir, "cos", "xo", msg); err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "stale-1", Sender: "xo", Recipient: "backend", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-2 * UndeliveredInboundAge),
	}); err != nil {
		t.Fatal(err)
	}
	if n := ReconcileInboundAcks(dir, nil); n != 0 {
		t.Fatalf("reconcile scrubbed %d rows on a hop entry, want 0", n)
	}
	reports := ScanUndeliveredInbound(dir, time.Now().UTC(), 0)
	if len(reports) != 1 || reports[0].Nonce != nonce || reports[0].Recipient != "backend" {
		t.Fatalf("undelivered reports = %+v, want the stale backend row", reports)
	}
}

// #707 N2b: per-edge settlements coexist in EITHER insertion order — a desk's
// real ack landing first must not block the coordinator hop entry (whose
// absence would re-break the coordinator's own footer ack).
func TestConsume_HopEntryInsertsAfterRealSettlement707(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("desk settled first")
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(dir)
	if _, err := reg.Consume(ConsumeFromInbound(nonce, msg, ReasonDurableAck, "xo", "backend")); err != nil {
		t.Fatal(err)
	}
	inserted, err := ConsumeCoordinatorRecipient(dir, "cos", "xo", msg)
	if err != nil || !inserted {
		t.Fatalf("hop insert after real settlement = (%v, %v), want inserted", inserted, err)
	}
	// The coordinator's hop is settled without disturbing the desk's settlement…
	if !reg.SettlesInboundRow(nonce, PayloadHash(msg), "xo") || !reg.SettlesInboundRow(nonce, PayloadHash(msg), "backend") {
		t.Fatal("both edges must read settled")
	}
	// …and lookup still prefers the real settlement.
	if e, ok := reg.LookupNonce(nonce); !ok || e.Reason != ReasonDurableAck {
		t.Fatalf("lookup = %+v, want durable-ack preference", e)
	}
	// Same-edge idempotency is preserved for both reasons.
	if again, _ := ConsumeCoordinatorRecipient(dir, "cos", "xo", msg); again {
		t.Fatal("hop rerun must be a no-op")
	}
	if again, _ := reg.Consume(ConsumeFromInbound(nonce, msg, ReasonDurableAck, "xo", "backend")); again {
		t.Fatal("real-ack rerun must be a no-op")
	}
}
