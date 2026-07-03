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
