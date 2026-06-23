package deliver

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// git runs a git command in dir, failing the test on error. Used to build the hermetic
// temp-repo fixtures for the durability checks (the production code filters by the HEAD
// tree, so the fixtures must actually commit — not just write to the worktree).
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// initRepo builds a temp git repo with a deterministic identity (no reliance on the host's
// global git config) and returns its root.
func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "config", "user.email", "t@t")
	git(t, root, "config", "user.name", "t")
	git(t, root, "config", "commit.gpgsign", "false")
	return root
}

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
	root := initRepo(t)
	// Seed an unrelated first commit so HEAD exists (born branch).
	git(t, root, "commit", "--allow-empty", "-q", "-m", "seed")

	path := filepath.Join(root, ".claude", "handoffs", "recycle-tok.md")

	// (1) Baseline: the designated path is ABSENT at HEAD.
	if absent, err := HandoffAbsentAtHead(root, path); err != nil || !absent {
		t.Fatalf("HandoffAbsentAtHead (baseline) = (%v,%v), want (true,nil)", absent, err)
	}
	// ...and not yet durable.
	if dur, err := HandoffDurable(root, path, minBytes); err != nil || dur {
		t.Fatalf("HandoffDurable (absent) = (%v,%v), want (false,nil)", dur, err)
	}

	// (2) Write but do NOT commit (worktree-only): still not durable, still absent at HEAD.
	writeHandoff(t, root, "recycle-tok.md", "x: this is a sufficiently long handoff body to clear the floor")
	if dur, err := HandoffDurable(root, path, minBytes); err != nil || dur {
		t.Fatalf("HandoffDurable (uncommitted worktree) = (%v,%v), want (false,nil)", dur, err)
	}
	if absent, err := HandoffAbsentAtHead(root, path); err != nil || !absent {
		t.Fatalf("HandoffAbsentAtHead (uncommitted) = (%v,%v), want (true,nil)", absent, err)
	}

	// (3) Commit it (force-add, mimicking the handoff turn): now durable + present at HEAD.
	git(t, root, "add", "-f", path)
	git(t, root, "commit", "-q", "-m", "handoff")
	if dur, err := HandoffDurable(root, path, minBytes); err != nil || !dur {
		t.Fatalf("HandoffDurable (committed non-trivial) = (%v,%v), want (true,nil)", dur, err)
	}
	if absent, err := HandoffAbsentAtHead(root, path); err != nil || absent {
		t.Fatalf("HandoffAbsentAtHead (committed) = (%v,%v), want (false,nil)", absent, err)
	}
}

func TestHandoffDurableTrivialBlobFails(t *testing.T) {
	root := initRepo(t)
	git(t, root, "commit", "--allow-empty", "-q", "-m", "seed")
	path := writeHandoff(t, root, "recycle-tok.md", "tiny") // < minBytes
	git(t, root, "add", "-f", path)
	git(t, root, "commit", "-q", "-m", "trivial")
	if dur, err := HandoffDurable(root, path, minBytes); err != nil || dur {
		t.Fatalf("HandoffDurable (committed but trivial) = (%v,%v), want (false,nil) — the minimum-viability floor", dur, err)
	}
}

func TestHandoffDurableUnbornHead(t *testing.T) {
	// A fresh repo with NO commits (unborn HEAD): ls-tree HEAD errors (exit 128); both
	// checks must treat it as not-yet-durable / absent (keep polling), never error out.
	root := initRepo(t)
	path := writeHandoff(t, root, "recycle-tok.md", "a body long enough to clear the configured byte floor easily")
	if dur, err := HandoffDurable(root, path, minBytes); err != nil || dur {
		t.Fatalf("HandoffDurable (unborn HEAD) = (%v,%v), want (false,nil)", dur, err)
	}
	if absent, err := HandoffAbsentAtHead(root, path); err != nil || !absent {
		t.Fatalf("HandoffAbsentAtHead (unborn HEAD) = (%v,%v), want (true,nil)", absent, err)
	}
}

func TestHandoffDurableNonGitRefuses(t *testing.T) {
	// A non-git cwd surfaces the rev-parse failure as an error, so the caller refuses
	// cleanly rather than treating a plain-disk file as durable.
	dir := t.TempDir()
	path := filepath.Join(dir, "handoff.md")
	if err := os.WriteFile(path, []byte("a body long enough to clear the floor by a wide margin..."), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := HandoffDurable(dir, path, minBytes); err == nil {
		t.Fatal("HandoffDurable (non-git) = nil error, want a refuse error")
	}
	if _, err := HandoffAbsentAtHead(dir, path); err == nil {
		t.Fatal("HandoffAbsentAtHead (non-git) = nil error, want a refuse error")
	}
}
