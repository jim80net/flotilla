package adjutantbuffer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendDrainRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	if err := Append(path, "xo", []string{"backend Working→Idle"}); err != nil {
		t.Fatal(err)
	}
	if Len(path) != 1 {
		t.Fatalf("len = %d, want 1", Len(path))
	}
	f, ok, err := Drain(path)
	if err != nil || !ok {
		t.Fatalf("Drain: ok=%v err=%v", ok, err)
	}
	if len(f.Items) != 1 || f.Items[0].Reason != "backend Working→Idle" {
		t.Fatalf("drained = %+v", f)
	}
	if Len(path) != 0 {
		t.Fatal("buffer should be empty after drain")
	}
}

func TestLoadResetsCorruptBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Len(path) != 0 {
		t.Fatalf("corrupt buffer should reset to empty, len=%d", Len(path))
	}
}

func TestFormatBriefListsItems(t *testing.T) {
	f := File{Leader: "alpha-xo", Items: []Item{{Reason: "backend PR gate"}}}
	got := FormatBrief("alpha-xo", f, true)
	for _, want := range []string{"alpha-xo", "backend PR gate", "Charter: not yet established", "adjutant-charter.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("brief missing %q\n%s", want, got)
		}
	}
}
