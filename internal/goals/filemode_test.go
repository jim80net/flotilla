package goals

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFilePreserveMode_KeepsExistingPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fleet-goals.yaml")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFilePreserveMode(path, []byte("updated")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o, want 644", st.Mode().Perm())
	}
}

func TestWriteFilePreserveMode_StatErrorFailsClosed(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	path := filepath.Join(locked, "fleet-goals.yaml")
	if err := writeFilePreserveMode(path, []byte("x")); err == nil {
		t.Fatal("expected error when stat of destination path fails")
	}
}
