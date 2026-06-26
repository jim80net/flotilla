package claudestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	text, cwd, count, ok := lastTurnTextWithCwd(path)
	if !ok || text != "the turn-final" || cwd != "/home/x/proj" || count != 1 {
		t.Fatalf("lastTurnTextWithCwd = (%q, %q, count=%d, %v), want the turn-final + its recorded cwd + count=1", text, cwd, count, ok)
	}
	// The thin wrapper still satisfies the existing (text, ok) contract.
	if wt, wok := lastTurnText(path); !wok || wt != "the turn-final" {
		t.Fatalf("lastTurnText wrapper = (%q, %v), want the same turn-final", wt, wok)
	}
}

// Under the lossy-encoding collision (two distinct working dirs encoding to one project dir),
// latestTurnTextForCwd must SKIP the colliding desk's (newer) session and recover THIS desk's own
// turn from its (older) session — not bail to silence, and not post the wrong desk's turn.
func TestLatestTurnTextForCwdRecoversAcrossCollision(t *testing.T) {
	origSleep := sleep
	defer func() { sleep = origSleep }()
	sleep = func(time.Duration) {} // no real quiescence wait in the test

	home := t.TempDir()
	t.Setenv("HOME", home)
	const myCwd = "/home/x/my.app"    // encodes to -home-x-my-app
	const otherCwd = "/home/x/my/app" // ALSO encodes to -home-x-my-app — the collision
	if encodeProjectDir(myCwd) != encodeProjectDir(otherCwd) {
		t.Fatalf("test premise broken: %q and %q must encode alike", myCwd, otherCwd)
	}
	dir := filepath.Join(home, ".claude", "projects", encodeProjectDir(myCwd))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "mine.jsonl")
	other := filepath.Join(dir, "other.jsonl")
	write := func(p, cwd, text string) {
		line := `{"type":"assistant","cwd":"` + cwd + `","message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}`
		if err := os.WriteFile(p, []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(mine, myCwd, "MY desk turn")
	write(other, otherCwd, "OTHER desk turn")
	// The COLLIDING desk's session is NEWER (it wrote more recently) — the case cubic flagged.
	old := time.Now().Add(-time.Minute)
	now := time.Now()
	_ = os.Chtimes(mine, old, old)
	_ = os.Chtimes(other, now, now)

	got, ok := latestTurnTextForCwd(myCwd)
	if !ok || got != "MY desk turn" {
		t.Fatalf("latestTurnTextForCwd = (%q, %v), want MY desk's turn (the newer colliding session must be skipped, not bailed on)", got, ok)
	}
}
