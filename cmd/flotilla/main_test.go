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
