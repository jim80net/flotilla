package unacked

import (
	"testing"
	"time"
)

func ts(offset time.Duration) time.Time {
	return time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC).Add(offset)
}

func cfg() Config {
	return DefaultConfig("op")
}

func TestScan_SkipsYoungMessages(t *testing.T) {
	now := ts(0)
	msgs := []Message{{
		ID: "1", AuthorID: "op", Content: "Can you fix the dashboard?",
		Timestamp: now.Add(-10 * time.Minute),
	}}
	if got := Scan(msgs, "C1", now, cfg()); len(got) != 0 {
		t.Fatalf("10m-old message must not alert (MinAge=30m); got %v", got)
	}
}

func TestScan_FlagsNoReplyAfterMinAge(t *testing.T) {
	now := ts(0)
	msgs := []Message{{
		ID: "1", AuthorID: "op", Content: "Can you fix the dashboard?",
		Timestamp: now.Add(-45 * time.Minute),
	}}
	got := Scan(msgs, "C1", now, cfg())
	if len(got) != 1 || got[0].Reason != "no-reply" {
		t.Fatalf("want one no-reply finding; got %v", got)
	}
}

func TestScan_AcksFleetWebhookReply(t *testing.T) {
	now := ts(0)
	msgs := []Message{
		{ID: "1", AuthorID: "op", Content: "Ship the fix?", Timestamp: now.Add(-45 * time.Minute)},
		{ID: "2", WebhookID: "wh", Content: "Shipped — PR #99 ready.", Timestamp: now.Add(-40 * time.Minute)},
	}
	if got := Scan(msgs, "C1", now, cfg()); len(got) != 0 {
		t.Fatalf("substantive webhook reply should ack; got %v", got)
	}
}

func TestScan_WorkingOnlyAfterFollowUp(t *testing.T) {
	now := ts(0)
	msgs := []Message{
		{ID: "1", AuthorID: "op", Content: "Status on the rollout?", Timestamp: now.Add(-2 * time.Hour)},
		{ID: "2", WebhookID: "wh", Content: "cos is working on your message", Timestamp: now.Add(-90 * time.Minute)},
	}
	got := Scan(msgs, "C1", now, cfg())
	if len(got) != 1 || got[0].Reason != "working-only" {
		t.Fatalf("want working-only after follow-up window; got %v", got)
	}
}

func TestScan_WorkingStillInsideFollowUp(t *testing.T) {
	now := ts(0)
	msgs := []Message{
		{ID: "1", AuthorID: "op", Content: "Status?", Timestamp: now.Add(-45 * time.Minute)},
		{ID: "2", WebhookID: "wh", Content: "working on it", Timestamp: now.Add(-10 * time.Minute)},
	}
	if got := Scan(msgs, "C1", now, cfg()); len(got) != 0 {
		t.Fatalf("working reply inside follow-up window should not flag; got %v", got)
	}
}

func TestScan_SkipsTrivialOperator(t *testing.T) {
	now := ts(0)
	msgs := []Message{{
		ID: "1", AuthorID: "op", Content: "thanks!",
		Timestamp: now.Add(-45 * time.Minute),
	}}
	if got := Scan(msgs, "C1", now, cfg()); len(got) != 0 {
		t.Fatalf("trivial ack should not flag; got %v", got)
	}
}

func TestDefaultMinAgeMatchesScanInterval(t *testing.T) {
	if DefaultMinAge < DefaultScanInterval {
		t.Fatalf("MinAge %v must be >= scan interval %v", DefaultMinAge, DefaultScanInterval)
	}
}

func TestScan_SkipsBeyondAckWindow(t *testing.T) {
	now := ts(0)
	custom := cfg()
	custom.AckWindow = 1 * time.Hour
	msgs := []Message{{
		ID: "1", AuthorID: "op", Content: "Can you fix the dashboard?",
		Timestamp: now.Add(-90 * time.Minute),
	}}
	if got := Scan(msgs, "C1", now, custom); len(got) != 0 {
		t.Fatalf("message older than AckWindow must not alert; got %v", got)
	}
}

func TestScan_FlagsInsideAckWindow(t *testing.T) {
	now := ts(0)
	custom := cfg()
	custom.AckWindow = 2 * time.Hour
	msgs := []Message{{
		ID: "1", AuthorID: "op", Content: "Can you fix the dashboard?",
		Timestamp: now.Add(-45 * time.Minute),
	}}
	got := Scan(msgs, "C1", now, custom)
	if len(got) != 1 {
		t.Fatalf("message inside AckWindow and past MinAge should flag; got %v", got)
	}
}
