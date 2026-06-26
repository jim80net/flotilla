package claudestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #175 content-correlation: the reply is the text-bearing assistant turn following the LATEST user
// turn carrying the operator's message — NOT a bare turn-count delta (which would mis-route a queued
// or interleaved turn).
func TestReplyAfterUserMsg(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := []string{
		// A PRIOR unrelated turn (e.g. a queued/self turn) — must NOT be routed as the reply.
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"prior unrelated turn"}]}}`,
		// The operator's hotline message, recorded verbatim as a user turn.
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"what do you need from me"}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"tool_use","text":""}]}}`, // tool-only, skipped
		`{"type":"tool_result","cwd":"/p"}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"here is what I need"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, cwd, found := replyAfterUserMsg(path, "what do you need from me")
	if !found || text != "here is what I need" || cwd != "/p" {
		t.Fatalf("got (text=%q cwd=%q found=%v), want the turn AFTER the matching user msg (not the prior turn)", text, cwd, found)
	}
}

// Before the reply lands (the user msg is recorded but no assistant turn follows), found=false — the
// watcher keeps polling rather than routing a stale prior turn.
func TestReplyAfterUserMsg_NoReplyYet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := []string{
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"prior turn"}]}}`,
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"hotline question"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, found := replyAfterUserMsg(path, "hotline question"); found {
		t.Fatal("found=true before any assistant turn follows the user msg — would route a stale prior turn")
	}
}

// A re-asked message re-anchors to the LATEST occurrence (its reply, not the first one's).
func TestReplyAfterUserMsg_ReAnchorsToLatest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := []string{
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"status?"}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"first answer"}]}}`,
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"status?"}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"second answer"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, _, found := replyAfterUserMsg(path, "status?")
	if !found || text != "second answer" {
		t.Fatalf("got (%q,%v), want the LATEST occurrence's reply 'second answer'", text, found)
	}
}

// A self-continuation wake (a NON-anchor user turn) recorded AFTER the reply must NOT let its output
// overwrite the captured reply — the reply window closes at the next substantive user turn.
func TestReplyAfterUserMsg_TrailingSelfContNotMisRouted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := []string{
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"what do you need from me"}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"THE REAL REPLY"}]}}`,
		// a tool_result user entry (empty text) must NOT close the window prematurely:
		`{"type":"user","cwd":"/p","message":{"role":"user","content":[{"type":"tool_result","text":""}]}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"THE REAL REPLY (after tool)"}]}}`,
		// a self-continuation wake (substantive non-anchor user turn) CLOSES the window:
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"[flotilla change-detector] continue the next step"}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"LATER UNRELATED SELF-CONT OUTPUT"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, _, found := replyAfterUserMsg(path, "what do you need from me")
	if !found || text != "THE REAL REPLY (after tool)" {
		t.Fatalf("got (%q,%v), want the real reply (NOT the trailing self-cont output)", text, found)
	}
}

// cubic P1: a SHORT/common operator message must anchor by EXACT match, not substring — a later
// non-anchor user turn that merely CONTAINS it must NOT re-anchor (which would mis-route its output).
func TestReplyAfterUserMsg_SubstringDoesNotMisRoute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := []string{
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"ok"}}`, // the operator's short message
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"THE REAL REPLY to ok"}]}}`,
		// a later self-cont prompt that CONTAINS "ok" as a substring — must NOT re-anchor:
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"looks ok, now continue the next task"}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"UNRELATED self-cont output"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, _, found := replyAfterUserMsg(path, "ok")
	if !found || text != "THE REAL REPLY to ok" {
		t.Fatalf("got (%q,%v), want the real reply — a substring-containing later turn must not re-anchor", text, found)
	}
}

// Whitespace-normalized exact match: a paste that alters whitespace still matches the same message.
func TestReplyAfterUserMsg_WhitespaceNormalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := []string{
		`{"type":"user","cwd":"/p","message":{"role":"user","content":"status   report\nplease"}}`,
		`{"type":"assistant","cwd":"/p","message":{"role":"assistant","content":[{"type":"text","text":"the status"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if text, _, found := replyAfterUserMsg(path, "status report please"); !found || text != "the status" {
		t.Fatalf("got (%q,%v), want a whitespace-normalized exact match to anchor", text, found)
	}
}
