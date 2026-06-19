package dash

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

// loadInlineRoster writes a roster JSON body to a temp file and loads it through
// the real roster.Load (so the test exercises the same validation the command
// does). Returns the loaded config.
func loadInlineRoster(t *testing.T, body string) (*roster.Config, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return roster.Load(path)
}

// loadInlineRosterAt loads a roster from an existing path (the caller wrote it).
func loadInlineRosterAt(t *testing.T, path string) (*roster.Config, error) {
	t.Helper()
	return roster.Load(path)
}
