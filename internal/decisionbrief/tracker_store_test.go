package decisionbrief

import (
	"path/filepath"
	"testing"
)

func TestTrackerPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.json")
	t1 := NewTracker()
	if !t1.TryBeginDispatch("goal-a:item") {
		t.Fatal("begin dispatch")
	}
	t1.Confirm("goal-a:item")
	if err := t1.Save(path); err != nil {
		t.Fatal(err)
	}
	t2 := LoadTracker(path)
	if t2.TryBeginDispatch("goal-a:item") {
		t.Fatal("restarted tracker should remember confirmed dispatch")
	}
}

func TestTrackerPendingNotPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.json")
	t1 := NewTracker()
	if !t1.TryBeginDispatch("goal-a:item") {
		t.Fatal("begin dispatch")
	}
	if err := t1.Save(path); err != nil {
		t.Fatal(err)
	}
	t2 := LoadTracker(path)
	if !t2.TryBeginDispatch("goal-a:item") {
		t.Fatal("pending-only begin must not survive restart — busy drop can retry")
	}
}
