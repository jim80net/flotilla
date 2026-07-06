package decisionbrief

import (
	"path/filepath"
	"testing"
)

func TestTrackerPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.json")
	t1 := NewTracker()
	if !t1.TryClaim("goal-a:item") {
		t.Fatal("first claim")
	}
	if err := t1.Save(path); err != nil {
		t.Fatal(err)
	}
	t2 := LoadTracker(path)
	if t2.TryClaim("goal-a:item") {
		t.Fatal("restarted tracker should remember claim")
	}
}
