package dispatch

import (
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
)

// Regression #628: turn-final contains nonce → inbound cleared → ScanUndeliveredInbound empty.
func TestReconcileInboundAcks_TurnFinalClearsPending(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("FO dispatch body for macro desk work that was acked")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "e1", Sender: "xo", Recipient: "backend", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// Before reconcile, age sweep would fire.
	if got := ScanUndeliveredInbound(dir, time.Now().UTC(), 15*time.Minute); len(got) != 1 {
		t.Fatalf("pre-reconcile undelivered = %d, want 1", len(got))
	}
	turnFinal := "row done. acked " + nonce + " — settle."
	n := ReconcileInboundAcks(dir, func(agent string) (string, bool, error) {
		if agent != "backend" {
			return "", false, nil
		}
		return turnFinal, true, nil
	})
	if n != 1 {
		t.Fatalf("cleared = %d, want 1", n)
	}
	if got := ScanUndeliveredInbound(dir, time.Now().UTC(), 15*time.Minute); len(got) != 0 {
		t.Fatalf("post-reconcile undelivered = %+v, want empty", got)
	}
	if !IsConsumed(dir, nonce, PayloadHash(msg)) {
		t.Fatal("reconcile must durable-consume acked nonce")
	}
	// OnFinish path equivalence: empty pending ledger.
	path, _ := inbound.Path(dir, "backend")
	if pend := inbound.NewStore(path).Load(); len(pend) != 0 {
		t.Fatalf("inbound pending = %+v", pend)
	}
}

func TestReconcileInboundAcks_ConsumedRegistryClearsWithoutTurnFinal(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("already consumed by a prior finish edge path")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "e2", Sender: "xo", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Consume(dir, ConsumeFromInbound(nonce, msg, ReasonTurnFinalAck, "xo", "desk")); err != nil {
		t.Fatal(err)
	}
	n := ReconcileInboundAcks(dir, nil) // no turn-final reader
	if n != 1 {
		t.Fatalf("cleared = %d", n)
	}
	if got := ScanUndeliveredInbound(dir, time.Now().UTC(), 15*time.Minute); len(got) != 0 {
		t.Fatalf("consumed entry still undelivered: %+v", got)
	}
}

func TestReconcileInboundAcksWithMergedClearsCompletedCargoBeforeAlert(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("PR #774 merged and chapter closed; no re-merge")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "merged", Sender: "cos", Recipient: "flotilla-dev-adj", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	var gotRecipient string
	n := ReconcileInboundAcksWithMerged(dir, nil, func(recipient string, pr int) bool {
		gotRecipient = recipient
		return pr == 774
	})
	if n != 1 {
		t.Fatalf("cleared = %d, want 1", n)
	}
	if gotRecipient != "flotilla-dev-adj" {
		t.Fatalf("merged checker recipient = %q", gotRecipient)
	}
	if reports := ScanUndeliveredInbound(dir, time.Now().UTC(), 15*time.Minute); len(reports) != 0 {
		t.Fatalf("merged cargo still produced undelivered alert: %+v", reports)
	}
	entry, ok := NewRegistry(dir).LookupNonce(nonce)
	if !ok || entry.Reason != ReasonMerged || entry.Recipient != "flotilla-dev-adj" {
		t.Fatalf("durable disposition = %+v, %v", entry, ok)
	}
}

func TestReconcileInboundAcksWithMergedRequiresAllCitedPRs(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("PR #774 merged; PR #775 still needs review")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "partial", Sender: "cos", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if n := ReconcileInboundAcksWithMerged(dir, nil, func(_ string, pr int) bool { return pr == 774 }); n != 0 {
		t.Fatalf("partially merged multi-PR cargo cleared = %d", n)
	}
	if reports := ScanUndeliveredInbound(dir, time.Now().UTC(), 15*time.Minute); len(reports) != 1 {
		t.Fatalf("partially merged cargo reports = %d, want 1", len(reports))
	}
}

func TestReconcileInboundAcksWithTerminalClearsMainSHACargo(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("main c48ad90 deployed; chapter closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "sha", Sender: "cos", Recipient: "dash", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	n := ReconcileInboundAcksWithTerminal(dir, nil, nil, func(recipient, sha string) bool {
		return recipient == "dash" && sha == "c48ad90"
	})
	if n != 1 {
		t.Fatalf("cleared = %d, want 1", n)
	}
	if entry, ok := NewRegistry(dir).LookupNonce(nonce); !ok || entry.Reason != ReasonMerged {
		t.Fatalf("SHA disposition = %+v, %v", entry, ok)
	}
}

// Finish-hook style: OnFinish ack clears inbound; ScanUndeliveredInbound empty.
func TestOnFinishAck_ThenScanUndeliveredEmpty(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("phase work that the desk will acknowledge in turn-final")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "m2", Sender: "xo", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	path, _ := inbound.Path(dir, "desk")
	st := inbound.NewStore(path)
	turn := "done. nonce " + nonce + " addressed."
	if actions := st.OnFinish(turn); len(actions) != 0 {
		t.Fatalf("ack should produce no reinject/escalate: %+v", actions)
	}
	if len(st.Load()) != 0 {
		t.Fatal("OnFinish must clear acked inbound")
	}
	// Durable consume as finish hook does.
	if _, err := Consume(dir, ConsumeFromInbound(nonce, msg, ReasonTurnFinalAck, "xo", "desk")); err != nil {
		t.Fatal(err)
	}
	if got := ScanUndeliveredInbound(dir, time.Now().UTC(), 15*time.Minute); len(got) != 0 {
		t.Fatalf("after OnFinish+consume, scan = %+v", got)
	}
}
