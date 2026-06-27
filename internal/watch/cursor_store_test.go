package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCursorStore_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	st := cursorStore{path: path}
	in := map[string]uint64{"CH1": 100, "CH2": 1500000000000000002}
	if err := st.save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := st.load()
	if len(got) != 2 || got["CH1"] != 100 || got["CH2"] != 1500000000000000002 {
		t.Fatalf("round-trip = %v, want %v", got, in)
	}
}

func TestCursorStore_MissingFile_EmptyMap(t *testing.T) {
	st := cursorStore{path: filepath.Join(t.TempDir(), "does-not-exist.json")}
	got := st.load()
	if len(got) != 0 {
		t.Fatalf("missing file load = %v, want empty (first-boot all)", got)
	}
}

func TestCursorStore_CorruptFile_EmptyMapNoError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := cursorStore{path: path}
	got := st.load() // fail-safe: corrupt → empty, no panic, no error surfaced
	if len(got) != 0 {
		t.Fatalf("corrupt file load = %v, want empty", got)
	}
}

func TestCursorStore_EmptyPath_NoOp(t *testing.T) {
	st := cursorStore{path: ""}
	if err := st.save(map[string]uint64{"CH": 1}); err != nil {
		t.Fatalf("save with empty path should be a no-op, got %v", err)
	}
	if got := st.load(); len(got) != 0 {
		t.Fatalf("load with empty path = %v, want empty", got)
	}
}
