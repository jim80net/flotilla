package deliver

import (
	"os"
	"path/filepath"
	"testing"
)

// writeHandoff writes a handoff file under <root>/.claude/handoffs/ and returns its path.
func writeHandoff(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, ".claude", "handoffs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const minBytes = 50

func TestHandoffAbsentAtHeadAndDurable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude", "handoffs", "recycle-tok.md")

	// (1) Baseline: the designated path is ABSENT on disk.
	if absent, err := HandoffAbsentAtHead(root, path); err != nil || !absent {
		t.Fatalf("HandoffAbsentAtHead (baseline) = (%v,%v), want (true,nil)", absent, err)
	}
	if dur, err := HandoffDurable(root, path, minBytes); err != nil || dur {
		t.Fatalf("HandoffDurable (absent) = (%v,%v), want (false,nil)", dur, err)
	}

	// (2) Write to disk (untracked): durable without any git commit (#218).
	writeHandoff(t, root, "recycle-tok.md", "x: this is a sufficiently long handoff body to clear the floor")
	if dur, err := HandoffDurable(root, path, minBytes); err != nil || !dur {
		t.Fatalf("HandoffDurable (on disk) = (%v,%v), want (true,nil)", dur, err)
	}
	if absent, err := HandoffAbsentAtHead(root, path); err != nil || absent {
		t.Fatalf("HandoffAbsentAtHead (present) = (%v,%v), want (false,nil)", absent, err)
	}
}

func TestHandoffDurableTrivialFileFails(t *testing.T) {
	root := t.TempDir()
	path := writeHandoff(t, root, "recycle-tok.md", "tiny") // < minBytes
	if dur, err := HandoffDurable(root, path, minBytes); err != nil || dur {
		t.Fatalf("HandoffDurable (trivial) = (%v,%v), want (false,nil) — the minimum-viability floor", dur, err)
	}
}

func TestHandoffDurableNonGitCwdWorks(t *testing.T) {
	// #218: durability is filesystem-based — a non-git cwd is fine.
	dir := t.TempDir()
	path := filepath.Join(dir, "handoff.md")
	body := "a body long enough to clear the configured byte floor by a wide margin..."
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if dur, err := HandoffDurable(dir, path, minBytes); err != nil || !dur {
		t.Fatalf("HandoffDurable (non-git cwd) = (%v,%v), want (true,nil)", dur, err)
	}
}

func TestHandoffPathMustBeUnderCwd(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escape.md")
	if _, err := HandoffDurable(root, outside, minBytes); err == nil {
		t.Fatal("HandoffDurable outside cwd = nil error, want refuse")
	}
}

func TestRemoveHandoffExactPath(t *testing.T) {
	root := t.TempDir()
	path := writeHandoff(t, root, "recycle-tok.md", "chapter")
	sibling := writeHandoff(t, root, "recycle-other.md", "keep")
	if err := RemoveHandoff(root, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("designated handoff still exists: %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("exact deletion touched sibling: %v", err)
	}
	if err := RemoveHandoff(root, filepath.Join(t.TempDir(), "outside.md")); err == nil {
		t.Fatal("RemoveHandoff outside cwd = nil error, want refusal")
	}
}
