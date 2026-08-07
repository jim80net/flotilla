package utilization

import (
	"strings"
	"testing"
)

func TestBuildUtilizationFirstSummary797(t *testing.T) {
	agents := []Agent{
		{State: "working", LoopPosture: "available", QueueState: QueueHasWork},
		{State: "idle", LoopPosture: "parked", QueueState: QueueEmpty},
		{State: "idle", LoopPosture: "available", RawLoopPosture: "awaiting-authority", QueueState: QueueEmpty},
		{State: "idle", LoopPosture: "drifted", QueueState: QueueHasWork},
		{State: "idle", LoopPosture: "blocked", QueueState: QueueEmpty},
		{State: "idle", LoopPosture: "unknown", QueueState: QueueUnknown},
	}
	got := Build(agents)
	if got.Working != 1 || got.Idle != 5 || got.IdleEmptyQueue != 3 || got.IdleHasQueue != 1 || got.IdleQueueUnknown != 1 {
		t.Fatalf("queue/activity summary = %+v", got)
	}
	if got.Blocked != 1 || got.AcceptsDispatch != 2 || got.Total != 6 {
		t.Fatalf("blocked/capacity summary = %+v", got)
	}
	if got.UtilizationPercent != 100.0/6.0 || !got.UtilizationWall {
		t.Fatalf("utilization rate/wall = %+v", got)
	}
	want := "1 of 6 seats working · 1 blocked"
	if line := Line(got); line != want {
		t.Fatalf("Line = %q, want %q", line, want)
	}
	if read := WallRead(got); read != "Almost no one is working — send work or pull the next queue item." {
		t.Fatalf("WallRead = %q", read)
	}
}

func TestHumanLineOmitsInternalUtilizationJargon814(t *testing.T) {
	line := Line(Summary{Working: 4, Total: 52, Blocked: 7, AcceptsDispatch: 44})
	if line != "4 of 52 seats working · 7 blocked" {
		t.Fatalf("Line = %q", line)
	}
	for _, forbidden := range []string{"%", "idle", "empty-queue", "accepts-dispatch", "awaiting-authority", "utilization wall"} {
		if strings.Contains(strings.ToLower(line), forbidden) {
			t.Fatalf("Line contains internal jargon %q: %q", forbidden, line)
		}
	}
}

func TestAuthorityWaitRemainsAvailableWithoutSeparateUtilizationState(t *testing.T) {
	agents := []Agent{{
		State:          "idle",
		LoopPosture:    "available",
		RawLoopPosture: "awaiting-authority",
		QueueState:     QueueEmpty,
	}}
	got := Build(agents)
	if got.AcceptsDispatch != 1 || got.Idle != 1 || got.Total != 1 {
		t.Fatalf("authority wait must remain ordinary available capacity: %+v", got)
	}
	if line := Line(got); line != "0 of 1 seat working" {
		t.Fatalf("authority wait must not reappear as a separate operator state: %q", line)
	}
}

func TestQueueStateFailHonest797(t *testing.T) {
	if got := QueueState(false, 0); got != QueueUnknown {
		t.Fatalf("unreadable backlog = %q, want unknown", got)
	}
	if got := QueueState(true, 0); got != QueueEmpty {
		t.Fatalf("known drained backlog = %q, want empty", got)
	}
	if got := QueueState(true, 2); got != QueueHasWork {
		t.Fatalf("known unblocked backlog = %q, want has-work", got)
	}
}
