package utilization

import "testing"

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
	if got.Blocked != 1 || got.AcceptsWork != 2 || got.Total != 6 {
		t.Fatalf("blocked/capacity summary = %+v", got)
	}
	want := "working:1 / idle:5 (empty-queue:3 · has-queue:1 · queue-unknown:1) / blocked:1 · total:6 · accepts-work:2"
	if line := Line(got); line != want {
		t.Fatalf("Line = %q, want %q", line, want)
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
