package frontier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckResumeViaReturnToPointer(t *testing.T) {
	f := Frame{ReturnTo: "[in-flight] ORG-ARCHITECTURE SHIFT goal-loop (#530)"}
	turn := "Side item handled. Resuming [in-flight] ORG-ARCHITECTURE SHIFT goal-loop (#530) — next step is frontier sidecar."
	if r := Check(turn, f); r.Violation {
		t.Fatalf("want satisfied resume, got violation signal=%q", r.Signal)
	}
}

func TestCheckViolationWhenSideItemSettlesWithoutResume(t *testing.T) {
	f := Frame{
		ReturnTo: "[in-flight] ship #530 frontier guard",
		SideItem: "backend: finished a turn",
	}
	turn := "Handled the adjutant seam brief. Backend change noted. Nothing further — idle."
	if r := Check(turn, f); !r.Violation {
		t.Fatal("want violation when frontier not addressed")
	}
}

func TestCheckReassignSatisfies(t *testing.T) {
	f := Frame{ReturnTo: "[in-flight] implement frontier sidecar"}
	turn := "Reassigned implementation: flotilla send backend-desk \"implement frontier sidecar\"."
	if r := Check(turn, f); r.Violation {
		t.Fatalf("reassign should satisfy, got %q", r.Signal)
	}
}

func TestCheckNamedGateSatisfies(t *testing.T) {
	f := Frame{ReturnTo: "[in-flight] merge-forward #521"}
	turn := "Cannot resume #521 yet — [awaiting-auth] operator merge-forward posture. Waiting on you: affirm lead merge."
	if r := Check(turn, f); r.Violation {
		t.Fatalf("named gate should satisfy, got %q", r.Signal)
	}
}

func TestReturnToFromBacklog(t *testing.T) {
	md := "## Backlog\n\n- [in-flight] ship return-to-frontier (#530)\n- [next] loop arbitration API\n"
	ptr, label, ok := ReturnToFromBacklog(md)
	if !ok || ptr == "" || label == "" {
		t.Fatalf("ReturnToFromBacklog = %q %q ok=%v", ptr, label, ok)
	}
	if !strings.Contains(ptr, "#530") {
		t.Fatalf("pointer = %q", ptr)
	}
}

func TestRecordPreemptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-frontier.json")
	f := Frame{
		Coordinator: "xo",
		ReturnTo:    "[in-flight] goal-loop step 1",
		Priority:    PriorityMechanical,
		Source:      "adjutant-buffer",
		SideItem:    "backend: edge",
		At:          time.Now().UTC(),
	}
	if err := RecordPreempt(path, f); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(path)
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if got.ReturnTo != f.ReturnTo || got.Coordinator != f.Coordinator {
		t.Fatalf("got %+v want %+v", got, f)
	}
	if err := Clear(path); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := Load(path); ok {
		t.Fatal("expected cleared")
	}
}

func TestLoadMissingInert(t *testing.T) {
	_, ok, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || ok {
		t.Fatalf("missing: ok=%v err=%v", ok, err)
	}
}

func TestNudgePromptIncludesReturnTo(t *testing.T) {
	p := NudgePrompt("xo", Frame{ReturnTo: "[in-flight] #530"})
	if !strings.Contains(p, "[in-flight] #530") || !strings.Contains(p, "return-to-frontier") {
		t.Fatalf("nudge missing context: %s", p)
	}
}

func TestSaveCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "frontier.json")
	if err := Save(path, Frame{ReturnTo: "warrant-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
