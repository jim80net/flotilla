package frontier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
		Coordinator: "xo", ReturnTo: "[in-flight] goal-loop step 1",
		Priority: PriorityMechanical, Source: "adjutant-buffer", SideItem: "backend: edge",
		SourcePath: "backlog.md", ItemID: "sha256:item", SourceRevision: "sha256:revision",
		ObservedStatus: "in-flight", At: time.Now().UTC(),
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
	derived := Frame{Coordinator: "xo", ReturnTo: "stale backlog fallback", Source: "seam", At: time.Now().UTC(),
		SourcePath: "backlog.md", ItemID: "sha256:item", SourceRevision: "sha256:revision", ObservedStatus: "next"}
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
	if err := RecordPreempt(path, Frame{Coordinator: "xo", ReturnTo: "backlog fallback", SourcePath: "backlog.md",
		ItemID: "sha256:item", SourceRevision: "sha256:revision", ObservedStatus: "next"}); err != nil {
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

func TestDerivedSourceNextToDoneRetires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alpha.md")
	active := []byte("## Backlog\n- [next] finish source validation\n")
	f, ok, err := DeriveFromBacklog("alpha-xo", path, active)
	if err != nil || !ok {
		t.Fatalf("derive: ok=%v err=%v", ok, err)
	}
	done := []byte("## Backlog\n- [done] finish source validation\n")
	if _, active, err := RefreshDerived(f, done); err != nil || active {
		t.Fatalf("done refresh: active=%v err=%v", active, err)
	}
}

func TestDerivedSourceRemovedDelegatedBlockedAndAwaitingRetire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alpha.md")
	original := []byte("## Backlog\n- [in-flight] validate exact item\n")
	f, ok, err := DeriveFromBacklog("alpha-xo", path, original)
	if err != nil || !ok {
		t.Fatalf("derive: ok=%v err=%v", ok, err)
	}
	for name, raw := range map[string]string{
		"removed":       "## Backlog\n- [next] another item\n",
		"delegated":     "## Backlog\n- [in-flight] [delegated] validate exact item\n",
		"blocked":       "## Backlog\n- [blocked] validate exact item\n",
		"awaiting-auth": "## Backlog\n- [awaiting-auth] validate exact item\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, active, err := RefreshDerived(f, []byte(raw)); err != nil || active {
				t.Fatalf("refresh: active=%v err=%v", active, err)
			}
		})
	}
}

func TestReplaceDerivedAtomicAndPreservesAuthored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.json")
	old := Frame{Coordinator: "alpha-xo", ReturnTo: "old", Origin: OriginDerived, ItemID: "sha256:old", At: time.Now().UTC()}
	if err := Save(path, old); err != nil {
		t.Fatal(err)
	}
	replacement := old
	replacement.ReturnTo = "new"
	replacement.SourceRevision = "sha256:new"
	if replaced, err := ReplaceDerivedIfUnchanged(path, old, replacement); err != nil || !replaced {
		t.Fatalf("replace: replaced=%v err=%v", replaced, err)
	}
	if replaced, err := ReplaceDerivedIfUnchanged(path, old, old); err != nil || replaced {
		t.Fatalf("stale CAS: replaced=%v err=%v", replaced, err)
	}
	authored := Frame{Coordinator: "alpha-xo", ReturnTo: "authored", Origin: OriginAuthored, At: time.Now().UTC()}
	if err := Save(path, authored); err != nil {
		t.Fatal(err)
	}
	if replaced, err := ReplaceDerivedIfUnchanged(path, authored, replacement); err != nil || replaced {
		t.Fatalf("authored replace: replaced=%v err=%v", replaced, err)
	}
	got, _, _ := Load(path)
	if got.ReturnTo != "authored" {
		t.Fatalf("authored frame overwritten: %+v", got)
	}
}

func TestRecordPreemptAtomicallySupersedesDerivedOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.json")
	old := Frame{Coordinator: "alpha-xo", ReturnTo: "old", SourcePath: "alpha.md", ItemID: "sha256:old",
		SourceRevision: "sha256:revision-1", ObservedStatus: "next"}
	newer := Frame{Coordinator: "alpha-xo", ReturnTo: "new", SourcePath: "alpha.md", ItemID: "sha256:new",
		SourceRevision: "sha256:revision-2", ObservedStatus: "in-flight"}
	if err := RecordPreempt(path, old); err != nil {
		t.Fatal(err)
	}
	if err := RecordPreempt(path, newer); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(path)
	if err != nil || !ok || got.ReturnTo != "new" || got.ItemID != "sha256:new" {
		t.Fatalf("superseded derived = %+v ok=%v err=%v", got, ok, err)
	}
}

func TestDerivedDisplayTruncationIsRuneSafeAndIdentityIsFull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alpha.md")
	text := strings.Repeat("a", 111) + "界" + strings.Repeat("z", 40)
	raw := []byte("## Backlog\n- [next] " + text + "\n")
	f, ok, err := DeriveFromBacklog("alpha-xo", path, raw)
	if err != nil || !ok {
		t.Fatalf("derive: ok=%v err=%v", ok, err)
	}
	if !utf8.ValidString(f.ReturnTo) || len(f.ReturnTo) > 120 {
		t.Fatalf("display pointer invalid/truncated unsafely: len=%d %q", len(f.ReturnTo), f.ReturnTo)
	}
	if len(f.ItemID) != len("sha256:")+64 {
		t.Fatalf("ItemID = %q, want full sha256", f.ItemID)
	}
}

func TestRefreshDerivedFailsClosedOnTornSource(t *testing.T) {
	f := Frame{Origin: OriginDerived, SourcePath: "alpha.md", ItemID: "sha256:item"}
	for name, raw := range map[string][]byte{
		"invalid UTF-8":   {0xff, 0xfe},
		"missing section": []byte("## Notes\n- [next] item\n"),
		"torn marker":     []byte("## Backlog\n- [ne"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, active, err := RefreshDerived(f, raw); err == nil || active {
				t.Fatalf("refresh: active=%v err=%v, want suppressed error", active, err)
			}
		})
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
