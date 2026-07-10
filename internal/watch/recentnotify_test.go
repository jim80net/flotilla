package watch

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRecentNotifyWithinTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla-cos-last-notify.json")
	now := time.Date(2026, 7, 10, 16, 0, 0, 0, time.UTC)
	if err := RecordRecentNotify(path, now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !RecentNotifyWithinTTL(path, DefaultRecentNotifySuppressTTL, now) {
		t.Fatal("notify within 3m should suppress mirror")
	}
	if RecentNotifyWithinTTL(path, DefaultRecentNotifySuppressTTL, now.Add(4*time.Minute)) {
		t.Fatal("expired notify should not suppress mirror")
	}
}

func TestRecentNotifyMissingFile(t *testing.T) {
	if RecentNotifyWithinTTL(filepath.Join(t.TempDir(), "missing.json"), DefaultRecentNotifySuppressTTL, time.Now()) {
		t.Fatal("missing stamp must not suppress")
	}
}
