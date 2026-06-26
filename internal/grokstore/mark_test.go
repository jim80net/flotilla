package grokstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #175 content-correlation for grok: the reply is the text-bearing assistant entry following the
// LATEST user entry carrying the operator's message (not a bare turn-count delta).
func TestReplyAfterUserMsg(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	lines := []string{
		`{"type":"assistant","content":"prior unrelated turn"}`, // a queued/self turn — NOT the reply
		`{"type":"user","content":[{"type":"text","text":"what do you need from me"}]}`,
		`{"type":"reasoning","content":"thinking"}`,
		`{"type":"tool_result","content":"tool out"}`,
		`{"type":"assistant","content":[{"type":"text","text":"here is what I need"}]}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, found, err := replyAfterUserMsg(path, "what do you need from me")
	if err != nil || !found || text != "here is what I need" {
		t.Fatalf("got (text=%q found=%v err=%v), want the turn AFTER the matching user entry", text, found, err)
	}
}

func TestReplyAfterUserMsg_NoReplyYet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	lines := []string{
		`{"type":"assistant","content":"prior turn"}`,
		`{"type":"user","content":[{"type":"text","text":"hotline q"}]}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := replyAfterUserMsg(path, "hotline q"); found {
		t.Fatal("found=true before any assistant entry follows the user msg")
	}
}

// A trailing non-anchor user entry (later prompt) must not let its output overwrite the captured reply.
func TestReplyAfterUserMsg_TrailingPromptNotMisRouted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	lines := []string{
		`{"type":"user","content":[{"type":"text","text":"what do you need from me"}]}`,
		`{"type":"assistant","content":[{"type":"text","text":"THE REAL REPLY"}]}`,
		`{"type":"tool_result","content":"tool out"}`,
		`{"type":"user","content":[{"type":"text","text":"a later unrelated prompt"}]}`,
		`{"type":"assistant","content":[{"type":"text","text":"LATER UNRELATED OUTPUT"}]}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, found, err := replyAfterUserMsg(path, "what do you need from me")
	if err != nil || !found || text != "THE REAL REPLY" {
		t.Fatalf("got (%q,%v,%v), want the real reply (NOT the trailing prompt's output)", text, found, err)
	}
}
