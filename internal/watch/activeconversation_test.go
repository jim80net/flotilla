package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestActiveConversationTailWithinTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-xo-last-operator-relay.json")
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if err := RecordActiveConversation(path, "m1", now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !ActiveConversationTail(path, DefaultActiveConversationTTL, now) {
		t.Fatal("recent relay tail should protect leader")
	}
}

func TestActiveConversationTailExpired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-xo-last-operator-relay.json")
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if err := RecordActiveConversation(path, "m1", now.Add(-11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ActiveConversationTail(path, DefaultActiveConversationTTL, now) {
		t.Fatal("expired relay tail should not protect")
	}
}

func TestActiveConversationTailFailSafeCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-xo-last-operator-relay.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !ActiveConversationTail(path, DefaultActiveConversationTTL, time.Now()) {
		t.Fatal("corrupt sidecar should fail-safe to protected")
	}
}

func TestClearActiveConversation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-xo-last-operator-relay.json")
	if err := RecordActiveConversation(path, "m1", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := ClearActiveConversation(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("want removed, stat err=%v", err)
	}
}
