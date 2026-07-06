package adjutantbuffer

import (
	"path/filepath"
	"testing"
)

func TestRemoveConfirmedItemsRetainsConcurrentAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-buffer.json")
	first := "backend: finished a turn (working→idle)"
	second := "frontend: entered shell"
	if err := Append(path, "xo", []string{first}); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := Peek(path)
	if err != nil || len(f.Items) != 1 {
		t.Fatalf("peek first: %+v err=%v", f, err)
	}
	delivered := f.Items
	if err := Append(path, "xo", []string{second}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveConfirmedItems(path, "xo", delivered); err != nil {
		t.Fatal(err)
	}
	got, ok, _, err := Peek(path)
	if err != nil || !ok || len(got.Items) != 1 {
		t.Fatalf("after scoped remove: %+v ok=%v err=%v", got, ok, err)
	}
	if got.Items[0].Reason != second {
		t.Fatalf("want retained %q, got %+v", second, got.Items)
	}
}
