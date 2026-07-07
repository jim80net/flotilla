package inbound

import (
	"testing"
)

func TestTrackConfirmedSend_SkipsCoordinator(t *testing.T) {
	dir := t.TempDir()
	msg, _, err := AppendDispatchNonce("hi")
	if err != nil {
		t.Fatal(err)
	}
	isCoord := func(agent string) bool { return agent == "cos" }
	if err := TrackConfirmedSend(dir, "memex", "cos", msg, "1", isCoord); err != nil {
		t.Fatal(err)
	}
	path, _ := Path(dir, "cos")
	if len(NewStore(path).Load()) != 0 {
		t.Fatal("coordinator must not be tracked")
	}
	if err := TrackConfirmedSend(dir, "memex", "backend", msg, "2", isCoord); err != nil {
		t.Fatal(err)
	}
	path, _ = Path(dir, "backend")
	if len(NewStore(path).Load()) != 1 {
		t.Fatal("execution desk must be tracked")
	}
}
