package dispatch

import (
	"os"
	"path/filepath"
	"strings"
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
	if st.Disposition != DispositionSuppressed {
		t.Fatalf("disposition = %s, want suppressed", st.Disposition)
	}
	if st.Reason != ReasonMerged {
		t.Fatalf("reason = %q", st.Reason)
	}
	if !strings.Contains(st.Detail, "recipient handling is not asserted") {
		t.Fatalf("suppression detail = %q", st.Detail)
	}
}

func TestLookupNonce_DistinguishesSuppressionFromHandledConsumption(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	for _, tc := range []struct {
		nonce, reason string
		want          Disposition
	}{
		{"flotilla-dispatch-suppressed-new", ReasonAutoSuppressed, DispositionSuppressed},
		{"flotilla-dispatch-suppressed-legacy", ReasonMerged, DispositionSuppressed},
		{"flotilla-dispatch-handled", ReasonDurableAck, DispositionConsumed},
	} {
		if _, err := Consume(dir, ConsumedEntry{Nonce: tc.nonce, PayloadHash: tc.nonce, Reason: tc.reason, ConsumedAt: now}); err != nil {
			t.Fatal(err)
		}
		got := LookupNonce(dir, tc.nonce, now)
		if got.Disposition != tc.want {
			t.Errorf("%s disposition = %s, want %s", tc.reason, got.Disposition, tc.want)
		}
	}
}

func TestMergedSuppress_AllCitedMustBeMerged(t *testing.T) {
	msg := "Resume gate for jim80net/flotilla#614 and acme/product PR #615 after review"
	if _, ok := ShouldSuppressMerged(msg, func(_ string, pr int) bool { return pr == 614 }); ok {
		t.Fatal("partial merge must not suppress multi-PR dispatch")
	}
	pr, ok := ShouldSuppressMerged(msg, func(_ string, pr int) bool { return pr == 614 || pr == 615 })
	if !ok || pr.Repository != "jim80net/flotilla" || pr.Number != 614 {
		t.Fatalf("all-merged: pr=%+v ok=%v", pr, ok)
	}
	if _, ok := ShouldSuppressMerged("no pr here at all", func(string, int) bool { return true }); ok {
		t.Fatal("no PR cite must not suppress")
	}
	if _, ok := ShouldSuppressMerged("memex-openclaw PR #29", func(string, int) bool { return true }); ok {
		t.Fatal("repository name without owner must not suppress")
	}
	if _, ok := ShouldSuppressMerged("jim80net/memex-openclaw#29 and PR #30", func(string, int) bool { return true }); ok {
		t.Fatal("one unscoped citation must make the whole terminal proof ambiguous")
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

func TestExtractQualifiedPRCitations(t *testing.T) {
	got, unscoped := ExtractQualifiedPRCitations("See acme/one#10, acme/two PR #20, and https://github.com/acme/three/pull/30")
	if unscoped || len(got) != 3 || got[0] != (PRCitation{Repository: "acme/one", Number: 10}) || got[1] != (PRCitation{Repository: "acme/two", Number: 20}) || got[2] != (PRCitation{Repository: "acme/three", Number: 30}) {
		t.Fatalf("got %+v unscoped=%v", got, unscoped)
	}
	if got, unscoped := ExtractQualifiedPRCitations("PR #29"); len(got) != 0 || !unscoped {
		t.Fatalf("bare citation = %+v unscoped=%v", got, unscoped)
	}
}

// Touch filesystem so tests that only use packages still compile cleanly under -count.
func TestConsumedPath_Empty(t *testing.T) {
	if ConsumedPath("") != "" {
		t.Fatal("empty rosterDir")
	}
	_ = os.WriteFile(filepath.Join(t.TempDir(), "x"), []byte("ok"), 0o644)
}
