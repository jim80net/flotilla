package dispatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
)

func TestScanUndeliveredOutbox_AgeBound(t *testing.T) {
	dir := t.TempDir()
	path, err := outbox.Path(dir, "xo")
	if err != nil {
		t.Fatal(err)
	}
	msg, nonce, err := inbound.AppendDispatchNonce("completion report for deploy")
	if err != nil {
		t.Fatal(err)
	}
	st := outbox.NewStore(path)
	old := time.Now().UTC().Add(-45 * time.Minute)
	_, _, err = st.Insert(outbox.Entry{
		Sender: "xo", Recipient: "cos", Message: msg, EnqueuedAt: old,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Fresh entry must not appear.
	freshMsg, _, err := inbound.AppendDispatchNonce("fresh send body needs twentyfour chars")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.Insert(outbox.Entry{
		Sender: "xo", Recipient: "backend", Message: freshMsg, EnqueuedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	reps := ScanUndeliveredOutbox(dir, time.Now().UTC(), 30*time.Minute)
	if len(reps) != 1 {
		t.Fatalf("reports = %+v, want 1 undelivered", reps)
	}
	if reps[0].Nonce != nonce || reps[0].Recipient != "cos" {
		t.Fatalf("report = %+v", reps[0])
	}
	if reps[0].Kind != "outbox" {
		t.Fatalf("kind = %q", reps[0].Kind)
	}
}

func TestScanUndeliveredInbound_SkipsConsumed(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("implement portable-location for hermes adapter now")
	if err != nil {
		t.Fatal(err)
	}
	if err := inbound.Record(dir, inbound.Entry{
		ID: "e1", Sender: "memex", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC().Add(-30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	reps := ScanUndeliveredInbound(dir, time.Now().UTC(), 15*time.Minute)
	if len(reps) != 1 {
		t.Fatalf("want 1 before consume, got %+v", reps)
	}
	if _, err := Consume(dir, ConsumeFromInbound(nonce, msg, ReasonTurnFinalAck, "memex", "desk")); err != nil {
		t.Fatal(err)
	}
	reps = ScanUndeliveredInbound(dir, time.Now().UTC(), 15*time.Minute)
	if len(reps) != 0 {
		t.Fatalf("consumed inbound must not undelivered-escalate: %+v", reps)
	}
}

func TestLookupNonce_DispositionOrder(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := inbound.AppendDispatchNonce("status line for disposition order test pad")
	if err != nil {
		t.Fatal(err)
	}
	// Queued first.
	if _, _, err := outbox.Enqueue(dir, "xo", "desk", msg); err != nil {
		t.Fatal(err)
	}
	st := LookupNonce(dir, nonce, time.Now().UTC())
	if st.Disposition != DispositionQueued {
		t.Fatalf("disposition = %s, want queued", st.Disposition)
	}
	// Delivered shadows queue for the same nonce only if inbound recorded —
	// simulate pane confirm by writing inbound (outbox may still hold a copy).
	if err := inbound.Record(dir, inbound.Entry{
		ID: "in1", Sender: "xo", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// Prefer consumed over inbound.
	if _, err := Consume(dir, ConsumeFromInbound(nonce, msg, ReasonMerged, "xo", "desk")); err != nil {
		t.Fatal(err)
	}
	st = LookupNonce(dir, nonce, time.Now().UTC())
	if st.Disposition != DispositionConsumed {
		t.Fatalf("disposition = %s, want consumed", st.Disposition)
	}
	if st.Reason != ReasonMerged {
		t.Fatalf("reason = %q", st.Reason)
	}
}

func TestMergedSuppress_AllCitedMustBeMerged(t *testing.T) {
	msg := "Resume gate for PR #614 and PR #615 after review"
	if _, ok := ShouldSuppressMerged(msg, func(pr int) bool { return pr == 614 }); ok {
		t.Fatal("partial merge must not suppress multi-PR dispatch")
	}
	pr, ok := ShouldSuppressMerged(msg, func(pr int) bool { return pr == 614 || pr == 615 })
	if !ok || pr != 614 {
		t.Fatalf("all-merged: pr=%d ok=%v", pr, ok)
	}
	if _, ok := ShouldSuppressMerged("no pr here at all", func(int) bool { return true }); ok {
		t.Fatal("no PR cite must not suppress")
	}
}

func TestShouldSuppressTerminalRequiresContextualSHAOnMain(t *testing.T) {
	msg := "PR head deadbee is obsolete; squash merged @ 4987bfa and chapter closed"
	evidence, ok := ShouldSuppressTerminal(msg, nil, func(sha string) bool { return sha == "4987bfa" })
	if !ok || evidence != "sha:4987bfa" {
		t.Fatalf("ShouldSuppressTerminal = (%q, %v)", evidence, ok)
	}
	if _, ok := ShouldSuppressTerminal("candidate head 4987bfa awaiting gate", nil, func(string) bool { return true }); ok {
		t.Fatal("bare candidate SHA must not terminally suppress cargo")
	}
	if _, ok := ShouldSuppressTerminal("main 4987bfa", nil, func(string) bool { return false }); ok {
		t.Fatal("unreachable main citation must not suppress cargo")
	}
}

func TestFormatQueuedAck_MachineReadable(t *testing.T) {
	// Desk-visible queued ack shape used by cmd send (#475 extension).
	line := FormatQueuedAck("abc123", "memex", "xo", false)
	if line != "QUEUED id=abc123 sender=memex recipient=xo status=busy_outbox" {
		t.Fatalf("line = %q", line)
	}
	line = FormatQueuedAck("abc123", "memex", "xo", true)
	if line != "QUEUED id=abc123 sender=memex recipient=xo status=already_queued" {
		t.Fatalf("dedup line = %q", line)
	}
}

func TestExtractPRNumbers(t *testing.T) {
	got := ExtractPRNumbers("See PR #10 and pull request 10 and PR 20")
	if len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("got %v", got)
	}
}

// Touch filesystem so tests that only use packages still compile cleanly under -count.
func TestConsumedPath_Empty(t *testing.T) {
	if ConsumedPath("") != "" {
		t.Fatal("empty rosterDir")
	}
	_ = os.WriteFile(filepath.Join(t.TempDir(), "x"), []byte("ok"), 0o644)
}
