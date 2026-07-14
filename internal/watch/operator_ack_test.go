package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOperatorAck_ConfirmedRelayThenTurnFinalMarksExactOrigin(t *testing.T) {
	root := t.TempDir()
	delivered := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	j := Job{Agent: "alpha-xo", Kind: KindRelay, MessageID: "1001", OriginChannel: "C_ALPHA"}
	if err := TrackOperatorRelayAck(root, j, delivered); err != nil {
		t.Fatal(err)
	}
	if OperatorMessageAcknowledged(root, "C_ALPHA", "1001") {
		t.Fatal("confirmed delivery alone must not acknowledge the operator message")
	}
	if n, err := AcknowledgeOperatorTurnFinal(root, "alpha-xo", delivered.Add(time.Minute)); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("acknowledged = %d, want 1", n)
	}
	if !OperatorMessageAcknowledged(root, "C_ALPHA", "1001") {
		t.Fatal("the delivered seat's turn-final must mark the exact origin message")
	}
	if OperatorMessageAcknowledged(root, "C_ALPHA", "1002") || OperatorMessageAcknowledged(root, "C_OTHER", "1001") {
		t.Fatal("ack marker must not cross message or channel boundaries")
	}
}

func TestOperatorAck_OutboxSendEpochCannotCreateMarker(t *testing.T) {
	root := t.TempDir()
	j := Job{
		Agent: "alpha-xo", Kind: KindSend, MessageID: "send-1", OriginChannel: "C_ALPHA",
		Sender: "alpha-desk", Epoch: 4, OutboxBound: true,
	}
	if err := TrackOperatorRelayAck(root, j, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, err := AcknowledgeOperatorTurnFinal(root, "alpha-xo", time.Now()); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Fatalf("outbox KindSend produced %d operator ack marker(s), want 0", n)
	}
	if OperatorMessageAcknowledged(root, "C_ALPHA", "send-1") {
		t.Fatal("canceled or stale outbox work must never manufacture an operator ack")
	}
}

func TestOperatorAck_AcknowledgesOnlyDeliveredSeat(t *testing.T) {
	root := t.TempDir()
	j := Job{Agent: "alpha-desk", Kind: KindRelay, MessageID: "1001", OriginChannel: "C_ALPHA"}
	if err := TrackOperatorRelayAck(root, j, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, err := AcknowledgeOperatorTurnFinal(root, "beta-desk", time.Now()); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Fatalf("foreign seat acknowledged %d messages, want 0", n)
	}
	if OperatorMessageAcknowledged(root, "C_ALPHA", "1001") {
		t.Fatal("another seat's turn-final must not acknowledge the delivery")
	}
}

func TestOperatorAck_OneFinishConsumesOnlyOldestConfirmedRelay(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"first", "second"} {
		j := Job{Agent: "alpha-xo", Kind: KindRelay, MessageID: id, OriginChannel: "C_ALPHA"}
		if err := TrackOperatorRelayAck(root, j, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := AcknowledgeOperatorTurnFinal(root, "alpha-xo", base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("first finish acknowledged %d relays, want 1", n)
	}
	if !OperatorMessageAcknowledged(root, "C_ALPHA", "first") {
		t.Fatal("oldest confirmed relay must be associated with the first finish")
	}
	if OperatorMessageAcknowledged(root, "C_ALPHA", "second") {
		t.Fatal("one finish must not acknowledge a later queued relay")
	}
	if n, err := AcknowledgeOperatorTurnFinal(root, "alpha-xo", base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	} else if n != 1 || !OperatorMessageAcknowledged(root, "C_ALPHA", "second") {
		t.Fatal("second finish must acknowledge the remaining relay")
	}
}

func TestOperatorAck_CorruptMarkerDoesNotSuppressBackstop(t *testing.T) {
	root := t.TempDir()
	path := operatorAckMarkerPath(root, "C_ALPHA", "1001")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if OperatorMessageAcknowledged(root, "C_ALPHA", "1001") {
		t.Fatal("corrupt marker must fail open to the alert path")
	}
}

func TestOperatorAck_RecoversMarkerCommittedBeforePendingRemoval(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	old := Job{Agent: "alpha-xo", Kind: KindRelay, MessageID: "old", OriginChannel: "C_ALPHA"}
	next := Job{Agent: "alpha-xo", Kind: KindRelay, MessageID: "next", OriginChannel: "C_ALPHA"}
	if err := TrackOperatorRelayAck(root, old, base); err != nil {
		t.Fatal(err)
	}
	oldRec := operatorAckRecord{
		Agent: "alpha-xo", ChannelID: "C_ALPHA", MessageID: "old",
		DeliveredAt: base, AcknowledgedAt: base.Add(time.Minute),
	}
	// Simulate a crash after durable marker commit and before pending removal.
	if err := writeOperatorAckRecord(operatorAckMarkerPath(root, "C_ALPHA", "old"), oldRec); err != nil {
		t.Fatal(err)
	}
	if err := TrackOperatorRelayAck(root, next, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n, err := AcknowledgeOperatorTurnFinal(root, "alpha-xo", base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("recovered finish acknowledged %d pending relays, want next relay only", n)
	}
	if !OperatorMessageAcknowledged(root, "C_ALPHA", "next") {
		t.Fatal("already-marked stale pending record blocked the next turn's acknowledgement")
	}
}

func TestOperatorAck_PrunesExpiredMarkers(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		id string
		at time.Time
	}{{"old", now.Add(-8 * 24 * time.Hour)}, {"new", now.Add(-time.Hour)}} {
		j := Job{Agent: "alpha-xo", Kind: KindRelay, MessageID: tc.id, OriginChannel: "C_ALPHA"}
		if err := TrackOperatorRelayAck(root, j, tc.at); err != nil {
			t.Fatal(err)
		}
		if _, err := AcknowledgeOperatorTurnFinal(root, "alpha-xo", tc.at); err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneOperatorAckMarkers(root, now.Add(-7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if OperatorMessageAcknowledged(root, "C_ALPHA", "old") {
		t.Fatal("expired marker was not pruned")
	}
	if !OperatorMessageAcknowledged(root, "C_ALPHA", "new") {
		t.Fatal("recent marker must survive pruning")
	}
}

func TestOperatorAck_ConcurrentFinishesConsumeDistinctRelays(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	const total = 20
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("message-%02d", i)
		j := Job{Agent: "alpha-xo", Kind: KindRelay, MessageID: id, OriginChannel: "C_ALPHA"}
		if err := TrackOperatorRelayAck(root, j, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errCh := make(chan error, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n, err := AcknowledgeOperatorTurnFinal(root, "alpha-xo", base.Add(time.Hour+time.Duration(i)*time.Second))
			if err != nil {
				errCh <- err
				return
			}
			if n != 1 {
				errCh <- fmt.Errorf("acknowledged %d relays, want 1", n)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("message-%02d", i)
		if !OperatorMessageAcknowledged(root, "C_ALPHA", id) {
			t.Errorf("concurrent finishes did not acknowledge %s", id)
		}
	}
}

func TestOperatorAck_CleanupFailureDoesNotBlockCurrentPendingRelay(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	old := Job{Agent: "alpha-xo", Kind: KindRelay, MessageID: "old", OriginChannel: "C_ALPHA"}
	next := Job{Agent: "alpha-xo", Kind: KindRelay, MessageID: "next", OriginChannel: "C_ALPHA"}
	for i, j := range []Job{old, next} {
		if err := TrackOperatorRelayAck(root, j, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	oldRec := operatorAckRecord{
		Agent: "alpha-xo", ChannelID: "C_ALPHA", MessageID: "old",
		DeliveredAt: base, AcknowledgedAt: base.Add(time.Minute),
	}
	if err := writeOperatorAckRecord(operatorAckMarkerPath(root, "C_ALPHA", "old"), oldRec); err != nil {
		t.Fatal(err)
	}
	pendingDir := filepath.Dir(operatorAckPendingPath(root, oldRec))
	if err := os.Chmod(pendingDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pendingDir, 0o700) })
	n, err := AcknowledgeOperatorTurnFinal(root, "alpha-xo", base.Add(2*time.Minute))
	if err == nil {
		t.Fatal("read-only pending directory should report cleanup failures")
	}
	if n != 1 {
		t.Fatalf("acknowledged = %d, want 1 despite stale cleanup failure", n)
	}
	if !OperatorMessageAcknowledged(root, "C_ALPHA", "next") {
		t.Fatal("stale cleanup failure blocked the current relay marker")
	}
}
