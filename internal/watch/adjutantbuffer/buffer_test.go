package adjutantbuffer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// Append is single-writer (watch detector thread). Sequential appends must preserve every item.
func TestSequentialAppendPreservesAllItems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	want := []string{"a", "b", "c", "d"}
	for _, r := range want {
		if err := Append(path, "xo", []string{r}); err != nil {
			t.Fatal(err)
		}
	}
	if got := Len(path); got != len(want) {
		t.Fatalf("Len = %d, want %d (single-writer sequential appends must not lose items)", got, len(want))
	}
	f, ok, _, err := Peek(path)
	if err != nil || !ok {
		t.Fatalf("Peek: ok=%v err=%v", ok, err)
	}
	seen := make(map[string]bool, len(want))
	for _, it := range f.Items {
		seen[it.Reason] = true
	}
	for _, r := range want {
		if !seen[r] {
			t.Fatalf("missing reason %q after sequential appends; items=%+v", r, f.Items)
		}
	}
}

func TestLoadCorruptQuarantineRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, quarantined, err := load(path)
	if err == nil {
		t.Fatal("expected error when quarantine rename fails")
	}
	if quarantined {
		t.Fatal("quarantined must be false when rename fails")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatal("corrupt file must remain when quarantine rename fails")
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

func TestOldestItemAge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	at := now.Add(-45 * time.Minute)
	f := File{Leader: "xo", Items: []Item{{At: at, Reason: "backend: edge"}}}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	age, ok := OldestItemAge(path, now)
	if !ok {
		t.Fatal("expected buffered item")
	}
	if age < 44*time.Minute || age > 46*time.Minute {
		t.Fatalf("age = %v, want ~45m", age)
	}
	if _, ok := OldestItemAge(filepath.Join(dir, "missing.json"), now); ok {
		t.Fatal("missing buffer should not report age")
	}
}

func TestFormatBriefEscalatesCorruptQuarantine(t *testing.T) {
	got := FormatBrief("xo", File{}, false, true)
	if !strings.Contains(got, "WARNING") || !strings.Contains(got, "corrupt") {
		t.Fatalf("brief should escalate corrupt quarantine:\n%s", got)
	}
}
