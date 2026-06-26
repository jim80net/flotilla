package claudestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The #175 reply-watch marker: count counts TEXT-bearing assistant turns and rises as the desk
// produces new replies; user / tool_result / sidechain / empty-text-assistant entries do NOT count.
func TestLastTurnTextWithCwdCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := []string{
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"first operator message"}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"reply one"}]}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"tool_use","text":""}]}}`, // tool-only: no text → not counted
		`{"type":"tool_result","cwd":"/p"}`,                                                                          // not an assistant turn
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"second operator message"}}`,
		`{"type":"assistant","cwd":"/p","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent noise"}]}}`, // sidechain → skipped
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"reply two"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, cwd, count, ok := lastTurnTextWithCwd(path)
	if !ok || text != "reply two" || cwd != "/p" || count != 2 {
		t.Fatalf("got (text=%q cwd=%q count=%d ok=%v), want (reply two, /p, 2, true) — only text-bearing non-sidechain assistant turns count", text, cwd, count, ok)
	}
}

// A transcript with no substantive latest turn still reports a count baseline (so the watcher can
// snapshot before the reply lands), and ok=false.
func TestLastTurnMarkBaselineBeforeReply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	// One prior text turn, then the operator's user message (no assistant reply yet).
	lines := []string{
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"earlier turn"}]}}`,
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"what do you need from me"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, count, ok := lastTurnTextWithCwd(path)
	if !ok || count != 1 {
		t.Fatalf("baseline got (count=%d ok=%v), want count=1 ok=true (one prior assistant turn; the user msg adds no assistant turn)", count, ok)
	}
}
