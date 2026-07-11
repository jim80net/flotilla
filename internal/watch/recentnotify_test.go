package watch

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRecentNotifyWithinTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-cos-last-notify.json")
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	if err := RecordRecentNotify(path, now.Add(-2*time.Minute), "hello operator"); err != nil {
		t.Fatal(err)
	}
	if !RecentNotifyWithinTTL(path, DefaultRecentNotifySuppressTTL, now) {
		t.Fatal("within TTL")
	}
	if RecentNotifyWithinTTL(path, DefaultRecentNotifySuppressTTL, now.Add(4*time.Minute)) {
		t.Fatal("past TTL")
	}
}

func TestRecentNotifyMissingFile(t *testing.T) {
	if RecentNotifyWithinTTL(filepath.Join(t.TempDir(), "missing.json"), DefaultRecentNotifySuppressTTL, time.Now()) {
		t.Fatal("missing must not suppress")
	}
}

func TestShouldSuppressMirrorDiscord_SameBodyAfterTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-xo-last-notify.json")
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	body := "Deploy complete. No action needed."
	// Stamp 5 minutes ago — past 3m TTL, within 15m same-body window.
	if err := RecordRecentNotify(path, now.Add(-5*time.Minute), body); err != nil {
		t.Fatal(err)
	}
	sup, reason := ShouldSuppressMirrorDiscord(path, DefaultRecentNotifySuppressTTL, DefaultRecentNotifySameBodyTTL, now, body)
	if !sup || reason != "same-body as recent notify" {
		t.Fatalf("sup=%v reason=%q", sup, reason)
	}
	// Different body → no suppress past TTL.
	sup, reason = ShouldSuppressMirrorDiscord(path, DefaultRecentNotifySuppressTTL, DefaultRecentNotifySameBodyTTL, now, "totally different cargo")
	if sup {
		t.Fatalf("different body must not suppress: %s", reason)
	}
}

func TestShouldSuppressMirrorDiscord_WithinTTLGreppable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stamp.json")
	now := time.Now().UTC()
	if err := RecordRecentNotify(path, now.Add(-30*time.Second), "x"); err != nil {
		t.Fatal(err)
	}
	sup, reason := ShouldSuppressMirrorDiscord(path, DefaultRecentNotifySuppressTTL, DefaultRecentNotifySameBodyTTL, now, "anything")
	if !sup || reason != "recent notify within 3m" {
		t.Fatalf("sup=%v reason=%q", sup, reason)
	}
}

func TestNotifyBodyHash_NormalizesWhitespace(t *testing.T) {
	a := NotifyBodyHash("hello   world\n")
	b := NotifyBodyHash("hello world")
	if a == "" || a != b {
		t.Fatalf("hash a=%q b=%q", a, b)
	}
}
