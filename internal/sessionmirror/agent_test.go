package sessionmirror

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAgentName_RejectsPathTraversal(t *testing.T) {
	for _, agent := range []string{"../escape", "foo/bar", "..", "desk\\win", ""} {
		if err := ValidateAgentName(agent); err == nil {
			t.Errorf("ValidateAgentName(%q) = nil, want error", agent)
		}
	}
}

func TestLedgerPath_RejectsUnsafeAgent(t *testing.T) {
	_, err := LedgerPath("/roster", "../etc")
	if err == nil {
		t.Fatal("LedgerPath with traversal agent should fail")
	}
}

func TestLedgerPathJoinsRosterDir(t *testing.T) {
	got, err := LedgerPath("/roster", "alpha-be")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/roster", "session-mirror", "alpha-be.jsonl")
	if got != want {
		t.Errorf("LedgerPath = %q, want %q", got, want)
	}
	if strings.Contains(got, "..") {
		t.Errorf("LedgerPath escaped roster dir: %q", got)
	}
}
