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

func TestRecordPreemptPreservesAuthoredFrame695(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.json")
	authored := Frame{
		Coordinator: "xo",
		ReturnTo:    "authored next step",
		Origin:      OriginAuthored,
		At:          time.Now().UTC().Add(time.Minute),
	}
	if err := Save(path, authored); err != nil {
		t.Fatal(err)
	}
	derived := Frame{Coordinator: "xo", ReturnTo: "stale backlog fallback", Source: "seam", At: time.Now().UTC()}
	if err := RecordPreempt(path, derived); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(path)
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if got.ReturnTo != authored.ReturnTo || got.Origin != OriginAuthored || !got.At.Equal(authored.At) {
		t.Fatalf("derived preempt clobbered authored frame: got %+v want %+v", got, authored)
	}
}

func TestRecordPreemptWritesDerivedFallbackIntoEmptyFrontier695(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.json")
	if err := RecordPreempt(path, Frame{Coordinator: "xo", ReturnTo: "backlog fallback"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(path)
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if got.ReturnTo != "backlog fallback" || got.Origin != OriginDerived {
		t.Fatalf("derived fallback = %+v", got)
	}
}

func TestReturnToFromBacklogSkipsDelegatedItems695(t *testing.T) {
	md := "## Backlog\n" +
		"- [in-flight] DELEGATED — implement API; do NOT re-dispatch\n" +
		"- [pending] DELEGATED — ratified in-flight synonym owned by a desk\n" +
		"- [next] [delegated] write migration docs\n" +
		"- [next] coordinator reviews the release gate\n"
	pointer, _, ok := ReturnToFromBacklog(md)
	if !ok || pointer != "- [next] coordinator reviews the release gate" {
		t.Fatalf("ReturnToFromBacklog = %q ok=%v", pointer, ok)
	}
}

func TestRecordPreemptAllDelegatedBacklogWritesNoFrame695(t *testing.T) {
	md := "## Backlog\n" +
		"- [in-flight] DELEGATED — implementation owned by backend; do NOT re-dispatch\n" +
		"- [next] [delegated] verification owned by frontend\n"
	pointer, _, ok := ReturnToFromBacklog(md)
	if ok || pointer != "" {
		t.Fatalf("all-delegated ReturnToFromBacklog = %q ok=%v", pointer, ok)
	}
	path := filepath.Join(t.TempDir(), "frontier.json")
	if err := RecordPreempt(path, Frame{ReturnTo: pointer}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Load(path); err != nil || ok {
		t.Fatalf("empty ReturnTo wrote frame: ok=%v err=%v", ok, err)
	}
}

func TestDelegationDetectionPrecedesPointerTruncation695(t *testing.T) {
	padding := strings.Repeat("x", 105)
	delegated := "- [next] " + padding + "[delegated] should never become the pointer"
	md := "## Backlog\n" + delegated + "\n- [next] safe coordinator work\n"
	markerAt := strings.Index(delegated, "[delegated]")
	if markerAt < 0 || markerAt >= 120 || markerAt+len("[delegated]") <= 120 {
		t.Fatalf("fixture must split delegation marker across truncation boundary: markerAt=%d len=%d", markerAt, len(delegated))
	}
	pointer, _, ok := ReturnToFromBacklog(md)
	if !ok || pointer != "- [next] safe coordinator work" {
		t.Fatalf("ReturnToFromBacklog = %q ok=%v", pointer, ok)
	}
}

func TestLoadMissingInert(t *testing.T) {
	_, ok, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || ok {
		t.Fatalf("missing: ok=%v err=%v", ok, err)
	}
}

func TestClearIfUnchangedPreservesNewAuthoredFrame695(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.json")
	old := Frame{ReturnTo: "old derived pointer", Origin: OriginDerived, At: time.Now().UTC()}
	if err := Save(path, old); err != nil {
		t.Fatal(err)
	}
	evaluated, ok, err := Load(path)
	if err != nil || !ok {
		t.Fatalf("Load old: ok=%v err=%v", ok, err)
	}
	authored := Frame{ReturnTo: "new authored pointer", Origin: OriginAuthored, At: old.At.Add(time.Second)}
	if err := Save(path, authored); err != nil {
		t.Fatal(err)
	}
	cleared, err := ClearIfUnchanged(path, evaluated)
	if err != nil || cleared {
		t.Fatalf("ClearIfUnchanged = %v err=%v, want preserved replacement", cleared, err)
	}
	got, ok, err := Load(path)
	if err != nil || !ok || got.ReturnTo != authored.ReturnTo || got.Origin != OriginAuthored {
		t.Fatalf("replacement after conditional clear = %+v ok=%v err=%v", got, ok, err)
	}
}

func TestClearIfUnchangedClearsEvaluatedFrame695(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.json")
	want := Frame{ReturnTo: "completed pointer", Origin: OriginAuthored, At: time.Now().UTC()}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	evaluated, _, _ := Load(path)
	cleared, err := ClearIfUnchanged(path, evaluated)
	if err != nil || !cleared {
		t.Fatalf("ClearIfUnchanged = %v err=%v, want cleared", cleared, err)
	}
	if _, ok, err := Load(path); err != nil || ok {
		t.Fatalf("frame remains after conditional clear: ok=%v err=%v", ok, err)
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
	got, ok, err := Load(path)
	if err != nil || !ok || got.Origin != OriginAuthored {
		t.Fatalf("direct Save origin = %q ok=%v err=%v, want authored", got.Origin, ok, err)
	}
}
