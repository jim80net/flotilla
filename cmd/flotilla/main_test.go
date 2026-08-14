package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveMessageInline(t *testing.T) {
	got, err := resolveMessage("", []string{"ship", "it", "now"}, nil)
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "ship it now" {
		t.Errorf("got %q, want %q", got, "ship it now")
	}
}

func TestSafeSendMessageSourceRefusesWordSplitMultiline(t *testing.T) {
	_, _, err := safeSendMessageSource("", []string{"first line", "value", "stripped", "`touch", "marker`"}, false)
	if err == nil || !strings.Contains(err.Error(), "word-split") || !strings.Contains(err.Error(), "--file") {
		t.Fatalf("safeSendMessageSource error = %v, want lossless transport refusal", err)
	}
}

func TestSafeSendMessageSourceRefusesLiteralMultiline(t *testing.T) {
	_, _, err := safeSendMessageSource("", []string{"first line\n`touch marker`\nvalue=$THING"}, false)
	if err == nil || !strings.Contains(err.Error(), "multiline") || !strings.Contains(err.Error(), "piped stdin") {
		t.Fatalf("safeSendMessageSource error = %v, want multiline refusal", err)
	}
}

func TestSafeSendMessageSourceDefaultsToStdin(t *testing.T) {
	filePath, inline, err := safeSendMessageSource("", nil, false)
	if err != nil || filePath != "-" || len(inline) != 0 {
		t.Fatalf("safeSendMessageSource = (%q, %v, %v), want stdin", filePath, inline, err)
	}
	got, err := resolveMessage(filePath, inline, strings.NewReader("line one\n`literal backticks`\nvalue=$THING\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "line one\n`literal backticks`\nvalue=$THING" {
		t.Fatalf("stdin body changed: %q", got)
	}
}

func TestSafeSendMessageSourceAllowsOneQuotedSingleLine(t *testing.T) {
	filePath, inline, err := safeSendMessageSource("", []string{"ship it now"}, true)
	if err != nil || filePath != "" || len(inline) != 1 || inline[0] != "ship it now" {
		t.Fatalf("safeSendMessageSource = (%q, %v, %v)", filePath, inline, err)
	}
}

func TestSafeSendMessageSourceNeverBlocksOnTerminalStdin(t *testing.T) {
	_, _, err := safeSendMessageSource("", nil, true)
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("terminal stdin error = %v", err)
	}
}

func TestResolveMessageFileTrimsTrailingNewline(t *testing.T) {
	p := filepath.Join(t.TempDir(), "msg.txt")
	if err := os.WriteFile(p, []byte("line one\nline two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveMessage(p, nil, nil)
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "line one\nline two" {
		t.Errorf("got %q — want internal newline kept, trailing trimmed", got)
	}
}

func TestResolveMessageStdin(t *testing.T) {
	got, err := resolveMessage("-", nil, strings.NewReader("from stdin\n"))
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "from stdin" {
		t.Errorf("got %q", got)
	}
}

func TestResolveMessageMutualExclusion(t *testing.T) {
	if _, err := resolveMessage("file.txt", []string{"inline"}, nil); err == nil {
		t.Error("resolveMessage(--file + inline) = nil error, want mutual-exclusion error")
	}
}

func TestResolveMessageMissing(t *testing.T) {
	if _, err := resolveMessage("", nil, nil); err == nil {
		t.Error("resolveMessage(nothing) = nil error, want error")
	}
}

func TestResolveMessageFileNotFound(t *testing.T) {
	if _, err := resolveMessage(filepath.Join(t.TempDir(), "nope.txt"), nil, nil); err == nil {
		t.Error("resolveMessage(missing file) = nil error, want error")
	}
}

func TestResolveMessageEmptyFileReturnsEmpty(t *testing.T) {
	// Contract: an empty file resolves to "" without error; the empty-message
	// rejection lives one layer up (cmdSend's TrimSpace guard).
	p := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveMessage(p, nil, nil)
	if err != nil || got != "" {
		t.Errorf("resolveMessage(empty file) = %q, %v; want \"\", nil", got, err)
	}
}

func TestResolveMessageWhitespaceOnlyFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ws.txt")
	if err := os.WriteFile(p, []byte("   \t \n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveMessage(p, nil, nil)
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	// Trailing newlines trimmed; remaining is whitespace (rejected by cmdSend).
	if strings.TrimSpace(got) != "" {
		t.Errorf("got %q, want whitespace-only", got)
	}
}

func TestResolveMessageStdinMultiline(t *testing.T) {
	got, err := resolveMessage("-", nil, strings.NewReader("a\nb\nc\n"))
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "a\nb\nc" {
		t.Errorf("got %q, want internal newlines preserved, trailing trimmed", got)
	}
}
