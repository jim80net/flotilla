package grokstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The #175 reply-watch marker for grok: count counts TEXT-bearing assistant entries; user / system /
// tool_result / reasoning / empty-text entries do NOT count, and the latest text is the reply.
func TestLastAssistantCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	lines := []string{
		`{"type":"system","content":"sys"}`,
		`{"type":"user","content":[{"type":"text","text":"first operator msg"}]}`,
		`{"type":"assistant","content":"reply one"}`,
		`{"type":"reasoning","content":"thinking"}`,
		`{"type":"tool_result","content":"tool out"}`,
		`{"type":"assistant","content":""}`, // empty assistant → not counted
		`{"type":"user","content":[{"type":"text","text":"second operator msg"}]}`,
		`{"type":"assistant","content":[{"type":"text","text":"reply two"}]}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, count, err := lastAssistant(path, "sess")
	if err != nil || text != "reply two" || count != 2 {
		t.Fatalf("got (text=%q count=%d err=%v), want (reply two, 2, nil) — only text-bearing assistant entries count", text, count, err)
	}
}
