package chapterend

import "testing"

func TestCheck_LaneDone(t *testing.T) {
	backlog := "## Backlog\n- [done] ship the feature\n- [done] PR #12 merged\n"
	turn := "Work here is done. PR #12 merged. Idle."
	r := Check(turn, backlog)
	if !r.ChapterEnd || r.Signal == SignalNone {
		t.Fatalf("want chapter-end lane-done, got %+v", r)
	}
}

func TestCheck_SuppressesMidStack(t *testing.T) {
	backlog := "## Backlog\n- [done] PR #1\n- [in-flight] PR #2 of stack\n"
	turn := "PR #1 merged. Settling for now."
	r := Check(turn, backlog)
	if r.ChapterEnd {
		t.Fatalf("mid-stack must NOT chapter-end, got %+v", r)
	}
	if r.SuppressReason != "unblocked-items-remain" {
		t.Fatalf("suppress reason = %q, want unblocked-items-remain", r.SuppressReason)
	}
}

func TestCheck_CoordinatorMarkWithoutBacklog(t *testing.T) {
	turn := "Marking the lane closed. Work here is done."
	r := Check(turn, "")
	if !r.ChapterEnd || r.Signal != SignalCoordinatorMark {
		t.Fatalf("want coordinator-mark chapter-end, got %+v", r)
	}
}

func TestCheck_PRMergedAloneNoBacklogNotEnough(t *testing.T) {
	// Without backlog we cannot prove lane-done — PR-merged alone must not fire.
	r := Check("PR #99 merged successfully.", "")
	if r.ChapterEnd {
		t.Fatalf("PR-merged without backlog/settlement must not fire, got %+v", r)
	}
}

func TestCheck_EmptyTurn(t *testing.T) {
	if r := Check("", "## Backlog\n- [done] x\n"); r.ChapterEnd {
		t.Fatal("empty turn must not chapter-end")
	}
}

func TestCheckCoordinator_TenureBypassesStandingBacklog(t *testing.T) {
	backlog := "## Backlog\n- [in-flight] continuous coordinator work\n"
	r := CheckCoordinator("Finished this turn; durable handoff is current.", backlog, true)
	if !r.ChapterEnd || r.Signal != SignalCoordinatorTenure {
		t.Fatalf("want coordinator tenure rotation, got %+v", r)
	}
	if ordinary := Check("Finished this turn; durable handoff is current.", backlog); ordinary.ChapterEnd {
		t.Fatalf("ordinary desk must retain backlog suppression, got %+v", ordinary)
	}
}

func TestCheckCoordinator_ExplicitContextHandoffBypassesBacklog(t *testing.T) {
	backlog := "## Backlog\n- [in-flight] continuous coordinator work\n"
	for _, turn := range []string{
		"Context budget reached. Durable self-handoff is ready.",
		"Please recycle me after this finish edge.",
		"Self-handoff complete; continue from the durable state.",
	} {
		r := CheckCoordinator(turn, backlog, false)
		if !r.ChapterEnd || r.Signal != SignalCoordinatorSelfHandoff {
			t.Fatalf("turn %q: want explicit coordinator handoff, got %+v", turn, r)
		}
	}
}

func TestCheckCoordinator_VagueBudgetMentionDoesNotRotate(t *testing.T) {
	r := CheckCoordinator(
		"We should consider a context budget eventually.",
		"## Backlog\n- [in-flight] continuous coordinator work\n",
		false,
	)
	if r.ChapterEnd {
		t.Fatalf("vague mention must fail closed, got %+v", r)
	}
}

func TestTracker_EdgeOnce(t *testing.T) {
	tr := NewTracker()
	r := Result{ChapterEnd: true, Signal: SignalLaneDone}
	if !tr.Record("backend", r) {
		t.Fatal("first record must fire")
	}
	if tr.Record("backend", r) {
		t.Fatal("duplicate signal must not re-fire")
	}
	// Clear latch with non-chapter finish.
	if tr.Record("backend", Result{}) {
		t.Fatal("clear must not fire")
	}
	if !tr.Record("backend", r) {
		t.Fatal("after clear, chapter-end must fire again")
	}
}

func TestTracker_SuppressNoFire(t *testing.T) {
	tr := NewTracker()
	if tr.Record("backend", Result{SuppressReason: "unblocked-items-remain"}) {
		t.Fatal("suppress must not dispatch")
	}
}

func TestTracker_ResetRearmsSignal(t *testing.T) {
	tr := NewTracker()
	r := Result{ChapterEnd: true, Signal: SignalCoordinatorTenure}
	if !tr.Record("cos", r) {
		t.Fatal("first tenure must fire")
	}
	tr.Reset("cos")
	if !tr.Record("cos", r) {
		t.Fatal("reset must rearm tenure")
	}
}
