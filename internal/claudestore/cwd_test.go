package claudestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lastTurnTextWithCwd must return the session's recorded working directory alongside the turn-final,
// so LatestTurnText can verify the session belongs to the desk it resolved (the guard against the
// lossy project-dir encoding collision).
func TestLastTurnTextWithCwdCapturesCwd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := []string{
		`{"type":"user","cwd":"/home/x/proj","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","cwd":"/home/x/proj","message":{"role":"assistant","content":[{"type":"text","text":"the turn-final"}]}}`,
		`{"type":"file-history-snapshot"}`, // a trailing non-message entry with no cwd — must not clobber it
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, cwd, ok := lastTurnTextWithCwd(path)
	if !ok || text != "the turn-final" || cwd != "/home/x/proj" {
		t.Fatalf("lastTurnTextWithCwd = (%q, %q, %v), want the turn-final + its recorded cwd", text, cwd, ok)
	}
	// The thin wrapper still satisfies the existing (text, ok) contract.
	if wt, wok := lastTurnText(path); !wok || wt != "the turn-final" {
		t.Fatalf("lastTurnText wrapper = (%q, %v), want the same turn-final", wt, wok)
	}
}
