package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitRemoteOwnerName(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acme/flotilla.git":     "acme/flotilla",
		"https://github.com/acme/flotilla":         "acme/flotilla",
		"http://github.com/Acme-Org/web.app.git":   "Acme-Org/web.app",
		"git@github.com:acme/flotilla.git":         "acme/flotilla",
		"git@github.com:acme/flotilla":             "acme/flotilla",
		"ssh://git@github.com/acme/flotilla.git":   "acme/flotilla",
		"ssh://git@github.com/acme/flotilla":       "acme/flotilla",
		"git://github.com/acme/flotilla.git":       "acme/flotilla",
		"https://gitlab.com/group/proj.git":        "group/proj",
		"  https://github.com/acme/flotilla.git  ": "acme/flotilla",
	}
	for in, want := range cases {
		got, err := ParseGitRemoteOwnerName(in)
		if err != nil {
			t.Errorf("ParseGitRemoteOwnerName(%q) err = %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseGitRemoteOwnerName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseGitRemoteOwnerNameRejects(t *testing.T) {
	bad := []string{
		"",
		"not-a-url",
		"git@github.com:",
		"https://github.com/onlyone",
		"https://github.com/a/b/c",
		"https://github.com/",
	}
	for _, in := range bad {
		if _, err := ParseGitRemoteOwnerName(in); err == nil {
			t.Errorf("ParseGitRemoteOwnerName(%q) = nil error, want error", in)
		}
	}
}

func TestDomainFileContent(t *testing.T) {
	got := DomainFileContent("acme/primary", []string{"acme/extra", "acme/primary", "  acme/other  ", ""})
	want := "acme/primary\nacme/extra\nacme/other\n"
	if got != want {
		t.Errorf("DomainFileContent = %q, want %q", got, want)
	}
}

func TestResolveDomainPrimary(t *testing.T) {
	p, ok := ResolveDomainPrimary("acme/from-roster", "https://github.com/acme/from-origin.git")
	if !ok || p != "acme/from-roster" {
		t.Errorf("roster primary should win: got %q ok=%v", p, ok)
	}
	p, ok = ResolveDomainPrimary("", "git@github.com:acme/from-origin.git")
	if !ok || p != "acme/from-origin" {
		t.Errorf("origin fallback: got %q ok=%v", p, ok)
	}
	if _, ok := ResolveDomainPrimary("", ""); ok {
		t.Error("empty primary+origin should not resolve")
	}
	if _, ok := ResolveDomainPrimary("", "not-parseable"); ok {
		t.Error("unparseable origin should not resolve")
	}
}

func TestMaterializeGatekeeperDomainFromPrimary(t *testing.T) {
	wt := t.TempDir()
	// Make it look enough like a git dir for origin lookup (origin may fail).
	if err := MaterializeGatekeeperDomain(wt, "acme/backend-api", []string{"acme/shared-lib"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wt, GatekeeperDomainRel)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "acme/backend-api\nacme/shared-lib\n"
	if string(body) != want {
		t.Errorf("domain file = %q, want %q", body, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("domain mode = %o, want 0644", info.Mode().Perm())
	}
	// Idempotent: second write leaves content.
	if err := MaterializeGatekeeperDomain(wt, "acme/backend-api", []string{"acme/shared-lib"}); err != nil {
		t.Fatal(err)
	}
	// Update when primary changes.
	if err := MaterializeGatekeeperDomain(wt, "acme/other", nil); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(path)
	if string(body) != "acme/other\n" {
		t.Errorf("after update = %q", body)
	}
}

func TestMaterializeGatekeeperDomainFromOrigin(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init")
	run("commit", "--allow-empty", "-m", "init")
	run("remote", "add", "origin", "https://github.com/acme/from-origin.git")

	if err := MaterializeGatekeeperDomain(repo, "", nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(repo, GatekeeperDomainRel))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "acme/from-origin\n" {
		t.Errorf("from origin = %q", body)
	}
}

func TestMaterializeGatekeeperDomainNoopWithoutSource(t *testing.T) {
	wt := t.TempDir()
	if err := MaterializeGatekeeperDomain(wt, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt, GatekeeperDomainRel)); !os.IsNotExist(err) {
		t.Errorf("expected no domain file when unresolvable, stat err=%v", err)
	}
}

func TestMaterializeGatekeeperDomainRequiresAbs(t *testing.T) {
	err := MaterializeGatekeeperDomain("relative/path", "acme/x", nil)
	if err == nil {
		t.Fatal("relative worktree = nil error, want error")
	}
	if !strings.Contains(err.Error(), "not absolute") {
		t.Errorf("error = %v, want not absolute", err)
	}
}
