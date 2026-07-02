package doctrine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAppendMember builds a second identity-append-shaped member with its OWN marker
// and target text, so the install loop can be exercised as member-count-agnostic
// without inventing a not-yet-designed mechanism.
func fakeAppendMember(name string) Member {
	open := "<!-- flotilla:" + name + " -->"
	close := "<!-- /flotilla:" + name + " -->"
	return Member{
		Name:        name,
		Mechanism:   MechanismIdentityAppend,
		Content:     open + "\n\nfake " + name + " doctrine body.\n" + close + "\n",
		OpenMarker:  open,
		CloseMarker: close,
	}
}

// writeIdentity scaffolds a workspace dir with a pre-existing identity file (as
// `workspace init` always does) and returns (workspaceDir, identityFileName). The
// Install signature takes the workspace dir + identity file name separately so the
// whole-file mechanism can resolve workspace-relative target paths.
func writeIdentity(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	const identity = "CLAUDE.md"
	if err := os.WriteFile(filepath.Join(dir, identity), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, identity
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// First install appends the block once; a second install detects the marker and
// skips — exactly one opening and one closing marker remain.
func TestInstallAppendsOnceAcrossRepeatedInstalls(t *testing.T) {
	stub := "# infra — desk identity\n\nYou are the infra desk.\n"
	dir, identity := writeIdentity(t, stub)
	p := filepath.Join(dir, identity)
	member := Members()[0]

	res1, err := Install(dir, identity, []Member{member}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res1) != 1 || res1[0].Action != ActionAppended {
		t.Fatalf("first install actions = %+v, want one appended", res1)
	}

	res2, err := Install(dir, identity, []Member{member}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2) != 1 || res2[0].Action != ActionSkipped {
		t.Fatalf("second install actions = %+v, want one skipped", res2)
	}

	body := readFile(t, p)
	if n := strings.Count(body, member.OpenMarker); n != 1 {
		t.Errorf("opening marker count = %d, want 1", n)
	}
	if n := strings.Count(body, member.CloseMarker); n != 1 {
		t.Errorf("closing marker count = %d, want 1", n)
	}
	// The original stub must survive verbatim ahead of the appended block.
	if !strings.HasPrefix(body, stub) {
		t.Error("identity stub was not preserved at the head of the file")
	}
}

// Operator edits BOTH inside the fenced block AND adjacent to it survive a re-install
// — the marker guard detects-and-skips, touching nothing.
func TestInstallPreservesOperatorEdits(t *testing.T) {
	member := Members()[0]
	dir, identity := writeIdentity(t, "# desk\n")
	p := filepath.Join(dir, identity)
	if _, err := Install(dir, identity, []Member{member}, false); err != nil {
		t.Fatal(err)
	}

	// Operator edits inside the block and appends a note after the closing marker.
	body := readFile(t, p)
	edited := strings.Replace(body, "Span of control", "Span of control (OPERATOR EDIT)", 1)
	edited += "\n## My own house rule\nKeep PRs small.\n"
	if err := os.WriteFile(p, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(dir, identity, []Member{member}, false); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, p)
	if got != edited {
		t.Errorf("re-install did not preserve operator edits verbatim:\n got:  %q\n want: %q", got, edited)
	}
}

// The install loop is member-count-agnostic: a SECOND fake identity-append member
// (its own marker, its own body) flows through unchanged — both append on first run,
// both skip on the second, each independently.
func TestInstallIsMemberCountAgnostic(t *testing.T) {
	set := []Member{Members()[0], fakeAppendMember("second")}
	dir, identity := writeIdentity(t, "# desk\n")
	p := filepath.Join(dir, identity)

	res1, err := Install(dir, identity, set, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res1) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res1))
	}
	for _, r := range res1 {
		if r.Action != ActionAppended {
			t.Errorf("member %q first install action = %q, want appended", r.Member, r.Action)
		}
	}

	res2, err := Install(dir, identity, set, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res2 {
		if r.Action != ActionSkipped {
			t.Errorf("member %q second install action = %q, want skipped", r.Member, r.Action)
		}
	}

	body := readFile(t, p)
	for _, m := range set {
		if n := strings.Count(body, m.OpenMarker); n != 1 {
			t.Errorf("member %q opening marker count = %d, want 1", m.Name, n)
		}
	}
}

// A missing identity file is an error: the install appends into an existing file the
// workspace already owns; it does not create the identity file (workspace init does).
func TestInstallErrorsOnMissingIdentityFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, "does-not-exist.md", Members(), false); err == nil {
		t.Fatal("install against a missing identity file = nil error, want error")
	}
}

// refresh replaces a drifted fenced block; identical content is a no-op.
func TestInstallRefreshReplacesDriftedBlock(t *testing.T) {
	member := Members()[1] // rule-of-three — distinctive body text for stale simulation
	dir, identity := writeIdentity(t, "# desk\n")
	p := filepath.Join(dir, identity)
	if _, err := Install(dir, identity, []Member{member}, false); err != nil {
		t.Fatal(err)
	}
	// Simulate stale embedded content (marker present, body old).
	body := readFile(t, p)
	stale := strings.Replace(body, "Span of control", "STALE span of control", 1)
	if err := os.WriteFile(p, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Install(dir, identity, []Member{member}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Action != ActionRefreshed {
		t.Fatalf("refresh actions = %+v, want one refreshed", res)
	}
	got := readFile(t, p)
	if strings.Contains(got, "STALE span of control") {
		t.Error("stale body survived refresh")
	}
	if !strings.Contains(got, member.OpenMarker) || !strings.Contains(got, "Span of control") {
		t.Error("refreshed block missing current asset content")
	}
	if !strings.HasPrefix(got, "# desk\n") {
		t.Error("content outside the fence was not preserved")
	}
}

func TestInstallRefreshNoOpWhenContentCurrent(t *testing.T) {
	dir, identity := writeIdentity(t, "# desk\n")
	p := filepath.Join(dir, identity)
	if _, err := Install(dir, identity, Members(), false); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, p)

	res, err := Install(dir, identity, Members(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		switch r.Action {
		case ActionSkipped:
			if r.Reason != "content current" && r.Reason != "marker present" {
				t.Fatalf("refresh skip action = %+v, unexpected reason", r)
			}
		case ActionKept:
			// heartbeat-skill whole-file members are unchanged under --refresh.
		default:
			t.Fatalf("refresh action = %+v, want skipped or kept", r)
		}
	}
	if got := readFile(t, p); got != before {
		t.Error("refresh rewrote an already-current block")
	}
}

func TestInstallRefreshErrorsOnMissingCloseMarker(t *testing.T) {
	member := Members()[1]
	dir, identity := writeIdentity(t, "# desk\n")
	p := filepath.Join(dir, identity)
	if _, err := Install(dir, identity, []Member{member}, false); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, p)
	body = strings.Replace(body, member.CloseMarker, "<!-- operator removed close -->", 1)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir, identity, []Member{member}, true); err == nil {
		t.Fatal("refresh with missing close marker = nil error, want error")
	}
}
