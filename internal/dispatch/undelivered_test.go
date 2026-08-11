package dispatch

import (
	"fmt"
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
	if st.Disposition != DispositionConsumed {
		t.Fatalf("disposition = %s, want consumed", st.Disposition)
	}
	if st.Reason != ReasonMerged {
		t.Fatalf("reason = %q", st.Reason)
	}
}

func TestOutboxNonceJoinsRejectQuotedHistoricalText(t *testing.T) {
	dir := t.TempDir()
	const decoy = "flotilla-dispatch-aabbccdd"
	message := "status report quoting " + decoy + " from an earlier dispatch"
	path, err := outbox.Path(dir, "sender")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = outbox.NewStore(path).Insert(outbox.Entry{
		Sender: "sender", Recipient: "recipient", Message: message,
		EnqueuedAt: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := LookupNonce(dir, decoy, time.Now().UTC()); got.Disposition != DispositionUnknown {
		t.Fatalf("dispatch-status adopted quoted decoy: %+v", got)
	}
	reports := ScanUndeliveredOutbox(dir, time.Now().UTC(), 30*time.Minute)
	if len(reports) != 1 {
		t.Fatalf("undelivered reports = %+v, want queued entry with no adopted nonce", reports)
	}
	if reports[0].Nonce != "" {
		t.Fatalf("undelivered scan adopted quoted decoy nonce %q", reports[0].Nonce)
	}
}

func TestLookupNonceReportsSenderRecipientFIFOPosition(t *testing.T) {
	dir := t.TempDir()
	var nonces []string
	for _, body := range []string{"first queued work", "second queued work", "third queued work"} {
		message, nonce, err := inbound.AppendDispatchNonce(body)
		if err != nil {
			t.Fatal(err)
		}
		nonces = append(nonces, nonce)
		if _, _, err := outbox.Enqueue(dir, "sender", "recipient", message); err != nil {
			t.Fatal(err)
		}
	}
	entries := outbox.ListAll(dir)
	st := LookupNonce(dir, nonces[1], time.Now().UTC().Add(time.Hour))
	if st.Disposition != DispositionQueued || st.Position != 2 || st.QueueDepth != 3 || st.HeadID != entries[0].ID {
		t.Fatalf("follower status = %+v, want position 2/3 behind %s", st, entries[0].ID)
	}
	if st.Detail != "sender-recipient FIFO follower; position 2 behind lane head "+entries[0].ID {
		t.Fatalf("follower detail = %q", st.Detail)
	}
	formatted := FormatStatus(st)
	for _, want := range []string{"queue_position=2/3", "head_id=" + entries[0].ID, "deferrals=0", "sender-recipient FIFO follower"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted status %q missing %q", formatted, want)
		}
	}
}

func TestLookupNonceQueuePositionIsPerSenderRecipientLane(t *testing.T) {
	dir := t.TempDir()
	alphaMessage, alphaNonce, err := inbound.AppendDispatchNonce("alpha wedged work")
	if err != nil {
		t.Fatal(err)
	}
	betaMessage, betaNonce, err := inbound.AppendDispatchNonce("beta independent work")
	if err != nil {
		t.Fatal(err)
	}
	alphaID, _, err := outbox.Enqueue(dir, "alpha", "recipient", alphaMessage)
	if err != nil {
		t.Fatal(err)
	}
	betaID, _, err := outbox.Enqueue(dir, "beta", "recipient", betaMessage)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Add(time.Hour)
	alpha := LookupNonce(dir, alphaNonce, now)
	beta := LookupNonce(dir, betaNonce, now)
	if alpha.Position != 1 || alpha.QueueDepth != 1 || alpha.HeadID != alphaID {
		t.Fatalf("alpha lane status = %+v, want independent position 1/1 at %s", alpha, alphaID)
	}
	if beta.Position != 1 || beta.QueueDepth != 1 || beta.HeadID != betaID {
		t.Fatalf("beta lane status = %+v, want independent position 1/1 at %s", beta, betaID)
	}
}

func TestLookupNonceAndRecipientQueueShareCurrentPopulation(t *testing.T) {
	dir := t.TempDir()
	staleMessage, staleNonce, err := inbound.AppendDispatchNonce("superseded queued work")
	if err != nil {
		t.Fatal(err)
	}
	liveMessage, liveNonce, err := inbound.AppendDispatchNonce("current queued work")
	if err != nil {
		t.Fatal(err)
	}
	path, err := outbox.Path(dir, "sender")
	if err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf(`{"epochs":{"recipient":2},"pending":[`+
		`{"id":"stale","sender":"sender","recipient":"recipient","message":%q,"epoch":1,"enqueued_at":"2026-08-01T00:00:00Z"},`+
		`{"id":"live","sender":"sender","recipient":"recipient","message":%q,"epoch":2,"enqueued_at":"2026-08-01T00:01:00Z"}]}`, staleMessage, liveMessage)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 1, 0, 2, 0, 0, time.UTC)
	stale := LookupNonce(dir, staleNonce, now)
	if stale.Disposition != DispositionSuperseded || stale.Position != 0 || stale.QueueDepth != 0 || stale.HeadID != "" {
		t.Fatalf("stale status = %+v, want superseded outside recipient FIFO", stale)
	}
	if !strings.Contains(stale.Detail, "superseded at epoch 1") {
		t.Fatalf("stale detail = %q", stale.Detail)
	}

	live := LookupNonce(dir, liveNonce, now)
	if live.Disposition != DispositionQueued || live.Position != 1 || live.QueueDepth != 1 || live.HeadID != "live" {
		t.Fatalf("live status = %+v, want sole current FIFO head", live)
	}

	entries := outbox.ListAll(dir)
	for _, entry := range entries {
		queue := senderRecipientQueue(dir, entries, entry.Sender, "recipient")
		inQueue := false
		for _, queued := range queue {
			inQueue = inQueue || queued.ID == entry.ID
		}
		if inQueue != outbox.RecipientQueueMember(dir, entry, "recipient") {
			t.Fatalf("entry %s membership disagrees: queue=%v predicate=%v", entry.ID, inQueue, outbox.RecipientQueueMember(dir, entry, "recipient"))
		}
	}
}

func TestScanUndeliveredOutboxExcludesSupersededPopulation(t *testing.T) {
	dir := t.TempDir()
	staleMessage, _, err := inbound.AppendDispatchNonce("superseded aged work")
	if err != nil {
		t.Fatal(err)
	}
	liveMessage, liveNonce, err := inbound.AppendDispatchNonce("current aged work")
	if err != nil {
		t.Fatal(err)
	}
	path, err := outbox.Path(dir, "sender")
	if err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf(`{"epochs":{"recipient":2},"pending":[`+
		`{"id":"stale","sender":"sender","recipient":"recipient","message":%q,"epoch":1,"enqueued_at":"2026-08-01T00:00:00Z"},`+
		`{"id":"live","sender":"sender","recipient":"recipient","message":%q,"epoch":2,"enqueued_at":"2026-08-01T00:01:00Z"}]}`, staleMessage, liveMessage)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	reports := ScanUndeliveredOutbox(dir, time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC), 15*time.Minute)
	if len(reports) != 1 || reports[0].ID != "live" || reports[0].Nonce != liveNonce {
		t.Fatalf("undelivered reports = %+v, want only current epoch-2 entry", reports)
	}
	if reports[0].Message == "" || strings.Contains(reports[0].Message, "stale") {
		t.Fatalf("undelivered report leaked superseded entry: %+v", reports[0])
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
