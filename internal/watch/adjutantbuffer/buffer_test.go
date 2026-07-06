package adjutantbuffer

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestPeekClearEnqueueOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	if err := Append(path, "xo", []string{"item"}); err != nil {
		t.Fatal(err)
	}
	f, ok, _, err := Peek(path)
	if err != nil || !ok || len(f.Items) != 1 {
		t.Fatalf("Peek before clear: f=%+v ok=%v err=%v", f, ok, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("sidecar must survive Peek")
	}
	if err := Clear(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("sidecar should be gone after Clear")
	}
}

func TestLoadQuarantinesCorruptBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Len(path) != 0 {
		t.Fatalf("corrupt buffer should read as empty, len=%d", Len(path))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var hasCorrupt bool
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt-") {
			hasCorrupt = true
		}
		if e.Name() == filepath.Base(path) {
			t.Fatal("corrupt buffer should be renamed, not left in place")
		}
	}
	if !hasCorrupt {
		t.Fatal("corrupt buffer should be renamed to a .corrupt sidecar")
	}
}

func TestAppendAfterQuarantineCreatesFreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, "xo", []string{"fresh"}); err != nil {
		t.Fatal(err)
	}
	if Len(path) != 1 {
		t.Fatalf("append after quarantine len=%d", Len(path))
	}
}

func TestSaveUsesUniqueTempNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			errs <- Append(path, "xo", []string{string(rune('a' + n))})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFormatBriefListsItems(t *testing.T) {
	f := File{Leader: "alpha-xo", Items: []Item{{Reason: "backend PR gate"}}}
	got := FormatBrief("alpha-xo", f, true, false)
	for _, want := range []string{"alpha-xo", "backend PR gate", "Charter: not yet established", "adjutant-charter.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("brief missing %q\n%s", want, got)
		}
	}
}

func TestFormatBriefEscalatesCorruptQuarantine(t *testing.T) {
	got := FormatBrief("xo", File{}, false, true)
	if !strings.Contains(got, "WARNING") || !strings.Contains(got, "corrupt") {
		t.Fatalf("brief should escalate corrupt quarantine:\n%s", got)
	}
}