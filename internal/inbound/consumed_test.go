package inbound

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkConsumed_IdempotentSuppressesReinject(t *testing.T) {
	dir := t.TempDir()
	nonce := "flotilla-dispatch-deadbeef"
	msg := "implement buffer v2"

	if err := MarkConsumed(dir, nonce, msg, "merged-on-main"); err != nil {
		t.Fatal(err)
	}
	if err := MarkConsumed(dir, nonce, msg, "duplicate"); err != nil {
		t.Fatal(err)
	}
	if !IsConsumed(dir, nonce) {
		t.Fatal("expected consumed")
	}

	tr := NewTracker()
	tr.Track(Entry{
		ID: "e1", Sender: "cos", Recipient: "backend",
		Message: msg, Nonce: nonce,
	})
	// evaluateFinish should still reinject when not acked — consumed check is roster-level.
	actions := tr.OnFinish("backend", "idle without nonce")
	if len(actions) != 1 || !actions[0].Reinject {
		t.Fatalf("tracker layer still reinjects without consumed hook: %+v", actions)
	}

	// Store + consumed gate in evaluateFinishList (wired in store/dropped_dispatch).
	list := []Entry{{ID: "e1", Sender: "cos", Recipient: "backend", Message: msg, Nonce: nonce}}
	acts, rem := evaluateFinishFiltered(list, "idle", func(n string) bool {
		return IsConsumed(dir, n)
	})
	if len(acts) != 0 {
		t.Fatalf("consumed nonce should not reinject: %+v", acts)
	}
	if len(rem) != 0 {
		t.Fatalf("consumed entry should be dropped from pending: %+v", rem)
	}

	path := filepath.Join(dir, "flotilla-dispatch-consumed.json")
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("consumed sidecar missing: %v", err)
	}
}