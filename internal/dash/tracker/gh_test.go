package tracker

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// fakeRunner is an injectable gh stand-in. It records the LAST invocation (args +
// stdin) for argument-safety assertions and returns canned stdout/stderr/err.
type fakeRunner struct {
	stdout  []byte
	stderr  []byte
	err     error
	gotArgs []string
	gotIn   []byte
	calls   int
}

func (f *fakeRunner) run(_ context.Context, args []string, stdin []byte) ([]byte, []byte, error) {
	f.calls++
	f.gotArgs = args
	f.gotIn = stdin
	return f.stdout, f.stderr, f.err
}

// arg returns the value of a --flag=value arg, or "" if absent.
func (f *fakeRunner) arg(flag string) (string, bool) {
	prefix := flag + "="
	for _, a := range f.gotArgs {
		if strings.HasPrefix(a, prefix) {
			return strings.TrimPrefix(a, prefix), true
		}
	}
	return "", false
}

func newFakeTracker(t *testing.T, f *fakeRunner) *GHTracker {
	t.Helper()
	g, err := newGH("jim80net/flotilla", f.run)
	if err != nil {
		t.Fatalf("newGH: %v", err)
	}
	return g
}

func ctx() context.Context { return context.Background() }

// --- repo validation (fail-closed) ---

func TestNewGH_RejectsBadRepo(t *testing.T) {
	bad := []string{"", "noslash", "-evil/repo", "owner/-evil", "owner/name; rm -rf", "a/b/c", "owner name/x"}
	for _, r := range bad {
		if _, err := newGH(r, (&fakeRunner{}).run); !errors.Is(err, ErrInvalidRepo) {
			t.Errorf("newGH(%q) err = %v, want ErrInvalidRepo", r, err)
		}
	}
	good := []string{"jim80net/flotilla", "a/b", "Org-1/repo.name_2"}
	for _, r := range good {
		if _, err := newGH(r, (&fakeRunner{}).run); err != nil {
			t.Errorf("newGH(%q) err = %v, want nil", r, err)
		}
	}
}

// --- List ---

func TestList_HappyPath(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`[
		{"number":116,"title":"a bug","state":"OPEN","labels":[{"name":"bug","color":"d73a4a"}],"author":{"login":"operator"},"updatedAt":"2026-06-18T06:48:12Z"},
		{"number":115,"title":"an idea","state":"OPEN","labels":[{"name":"operator-idea"}],"author":{"login":"operator"},"updatedAt":"2026-06-18T06:34:59Z"}
	]`)}
	g := newFakeTracker(t, f)
	issues, err := g.List(ctx(), ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(issues) != 2 || issues[0].Number != 116 || issues[1].Labels[0].Name != "operator-idea" {
		t.Fatalf("issues = %+v", issues)
	}
	// Default state is open; the pinned repo + field set are present.
	if v, _ := f.arg("--repo"); v != "jim80net/flotilla" {
		t.Errorf("--repo = %q", v)
	}
	if v, _ := f.arg("--state"); v != "open" {
		t.Errorf("--state = %q", v)
	}
	if v, _ := f.arg("--json"); v != listFields {
		t.Errorf("--json = %q, want pinned %q", v, listFields)
	}
}

func TestList_EmptyIsNotError(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`[]`)}
	g := newFakeTracker(t, f)
	issues, err := g.List(ctx(), ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if issues == nil || len(issues) != 0 {
		t.Fatalf("empty list should be a non-nil empty slice, got %v", issues)
	}
}

func TestList_LabelFilterAndLimit(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`[]`)}
	g := newFakeTracker(t, f)
	if _, err := g.List(ctx(), ListFilter{Label: "operator-idea", Limit: 5}); err != nil {
		t.Fatal(err)
	}
	if v, ok := f.arg("--label"); !ok || v != "operator-idea" {
		t.Errorf("--label = %q (%v)", v, ok)
	}
	if v, _ := f.arg("--limit"); v != "5" {
		t.Errorf("--limit = %q", v)
	}
}

func TestList_LimitClampedToMax(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`[]`)}
	g := newFakeTracker(t, f)
	if _, err := g.List(ctx(), ListFilter{Limit: 99999}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.arg("--limit"); v != "200" {
		t.Errorf("--limit = %q, want clamped to 200", v)
	}
}

func TestList_UnparseableIsTypedError(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`not json at all`)}
	g := newFakeTracker(t, f)
	if _, err := g.List(ctx(), ListFilter{}); !errors.Is(err, ErrParse) {
		t.Fatalf("List err = %v, want ErrParse", err)
	}
}

// --- typed gh-failure classification (verified against gh 2.45 stderr) ---

func TestClassify_GHFailureModes(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   error
	}{
		{"repo-not-found", "GraphQL: Could not resolve to a Repository with the name 'x/y'. (repository)", ErrRepoNotFound},
		{"issue-not-found", "GraphQL: Could not resolve to an issue or pull request with the number of 999999. (repository.issue)", ErrIssueNotFound},
		{"unauth", "HTTP 401: Bad credentials (https://api.github.com/graphql)\nTry authenticating with:  gh auth login", ErrUnauthenticated},
		{"rate-limited", "HTTP 403: API rate limit exceeded for user ID 1.", ErrRateLimited},
		{"network", "error connecting to nonexistent.invalid\ncheck your internet connection or https://githubstatus.com", ErrNetwork},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeRunner{stderr: []byte(c.stderr), err: errors.New("exit status 1")}
			g := newFakeTracker(t, f)
			_, err := g.List(ctx(), ListFilter{})
			if !errors.Is(err, c.want) {
				t.Fatalf("classify(%q) = %v, want %v", c.name, err, c.want)
			}
		})
	}
}

func TestClassify_TimeoutAndMissingGH(t *testing.T) {
	// A context-deadline (a hung gh killed by execRunner) maps to ErrTimeout.
	f := &fakeRunner{err: context.DeadlineExceeded}
	g := newFakeTracker(t, f)
	if _, err := g.List(ctx(), ListFilter{}); !errors.Is(err, ErrTimeout) {
		t.Errorf("deadline → %v, want ErrTimeout", err)
	}
	// gh not installed (exec.ErrNotFound) maps to ErrGHMissing, not a swallowed
	// or generic error.
	f2 := &fakeRunner{err: exec.ErrNotFound}
	g2 := newFakeTracker(t, f2)
	if _, err := g2.List(ctx(), ListFilter{}); !errors.Is(err, ErrGHMissing) {
		t.Errorf("exec.ErrNotFound → %v, want ErrGHMissing", err)
	}
}

func TestList_InvalidStateRejected(t *testing.T) {
	f := &fakeRunner{}
	g := newFakeTracker(t, f)
	if _, err := g.List(ctx(), ListFilter{State: "garbage"}); !errors.Is(err, ErrInvalidState) {
		t.Errorf("err = %v, want ErrInvalidState", err)
	}
	if f.calls != 0 {
		t.Error("an invalid state must not reach gh")
	}
}

func TestCreate_TrimsTitleAndLabels(t *testing.T) {
	f := &fakeRunner{stdout: []byte("https://github.com/jim80net/flotilla/issues/1\n")}
	g := newFakeTracker(t, f)
	issue, err := g.Create(ctx(), CreateInput{Title: "  spaced  ", Body: "x", Labels: []string{"  operator-idea  "}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if v, _ := f.arg("--title"); v != "spaced" {
		t.Errorf("--title = %q, want trimmed 'spaced'", v)
	}
	if v, _ := f.arg("--label"); v != "operator-idea" {
		t.Errorf("--label = %q, want trimmed", v)
	}
	if issue.Title != "spaced" {
		t.Errorf("returned title = %q, want trimmed", issue.Title)
	}
}

func TestCreate_EmptyLabelRejected(t *testing.T) {
	f := &fakeRunner{}
	g := newFakeTracker(t, f)
	if _, err := g.Create(ctx(), CreateInput{Title: "ok", Labels: []string{"  "}}); !errors.Is(err, ErrEmptyLabel) {
		t.Errorf("err = %v, want ErrEmptyLabel", err)
	}
	if f.calls != 0 {
		t.Error("an empty label must not reach gh")
	}
}

func TestClassify_UnknownFailureNeverSwallowed(t *testing.T) {
	f := &fakeRunner{stderr: []byte("something weird happened"), err: errors.New("exit status 2")}
	g := newFakeTracker(t, f)
	_, err := g.List(ctx(), ListFilter{})
	if !errors.Is(err, ErrGH) {
		t.Fatalf("err = %v, want ErrGH", err)
	}
	if !strings.Contains(err.Error(), "something weird") {
		t.Errorf("unknown gh error must carry stderr, got %v", err)
	}
}

// --- Get ---

func TestGet_HappyPath(t *testing.T) {
	f := &fakeRunner{stdout: []byte(`{"number":106,"title":"t","body":"the body","state":"OPEN","labels":[],"author":{"login":"operator"},"comments":[{"author":{"login":"operator"},"body":"a comment","createdAt":"2026-06-18T03:00:20Z"}],"url":"https://github.com/jim80net/flotilla/issues/106"}`)}
	g := newFakeTracker(t, f)
	issue, err := g.Get(ctx(), 106)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Body != "the body" || len(issue.Comments) != 1 || issue.Comments[0].Body != "a comment" {
		t.Fatalf("issue = %+v", issue)
	}
	// The number is passed after `--`, never as a flag; the pinned --json is the detail set.
	if v, _ := f.arg("--json"); v != detailFields {
		t.Errorf("--json = %q, want %q", v, detailFields)
	}
	if !lastArgsContain(f.gotArgs, "--", "106") {
		t.Errorf("number must follow `--`; args = %v", f.gotArgs)
	}
}

func TestGet_RejectsNonPositiveNumber(t *testing.T) {
	f := &fakeRunner{}
	g := newFakeTracker(t, f)
	for _, n := range []int{0, -1} {
		if _, err := g.Get(ctx(), n); !errors.Is(err, ErrInvalidNumber) {
			t.Errorf("Get(%d) = %v, want ErrInvalidNumber", n, err)
		}
	}
	if f.calls != 0 {
		t.Errorf("a bad number must not reach gh (calls=%d)", f.calls)
	}
}

// --- Create + injection-safety ---

func TestCreate_HappyPathParsesURL(t *testing.T) {
	f := &fakeRunner{stdout: []byte("https://github.com/jim80net/flotilla/issues/130\n")}
	g := newFakeTracker(t, f)
	issue, err := g.Create(ctx(), CreateInput{Title: "new", Body: "body here", Labels: []string{"operator-idea"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if issue.Number != 130 {
		t.Errorf("number = %d, want 130", issue.Number)
	}
	// Body goes via STDIN, never argv.
	if string(f.gotIn) != "body here" {
		t.Errorf("body must be passed via stdin, got stdin=%q", f.gotIn)
	}
	for _, a := range f.gotArgs {
		if strings.Contains(a, "body here") {
			t.Errorf("body must NOT appear in argv: %q", a)
		}
	}
	if v, _ := f.arg("--label"); v != "operator-idea" {
		t.Errorf("--label = %q", v)
	}
}

func TestCreate_InjectionSafeTitleAndRepoPin(t *testing.T) {
	// A malicious title that tries to inject flags / retarget the repo.
	evil := "-rf --repo=attacker/evil --confirm"
	f := &fakeRunner{stdout: []byte("https://github.com/jim80net/flotilla/issues/131\n")}
	g := newFakeTracker(t, f)
	if _, err := g.Create(ctx(), CreateInput{Title: evil, Body: "x"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The title must be a SINGLE argv token bound to --title via `=`, so the
	// leading `-` and embedded --repo can never be parsed as flags.
	if v, ok := f.arg("--title"); !ok || v != evil {
		t.Errorf("--title = %q (ok=%v), want the literal evil string", v, ok)
	}
	// The repo is the PINNED one — the title's injected --repo did not retarget it.
	repoArgs := 0
	for _, a := range f.gotArgs {
		if strings.HasPrefix(a, "--repo=") {
			repoArgs++
			if a != "--repo=jim80net/flotilla" {
				t.Errorf("repo arg = %q, want the pinned repo", a)
			}
		}
	}
	if repoArgs != 1 {
		t.Errorf("expected exactly one --repo arg, got %d (args=%v)", repoArgs, f.gotArgs)
	}
}

func TestCreate_NewlineAndLeadingDashBodyViaStdin(t *testing.T) {
	body := "-not-a-flag\nsecond line\nthird"
	f := &fakeRunner{stdout: []byte("https://github.com/jim80net/flotilla/issues/1\n")}
	g := newFakeTracker(t, f)
	if _, err := g.Create(ctx(), CreateInput{Title: "ok", Body: body}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(f.gotIn) != body {
		t.Errorf("body via stdin = %q, want intact %q", f.gotIn, body)
	}
}

func TestCreate_EmptyTitleRejected(t *testing.T) {
	f := &fakeRunner{}
	g := newFakeTracker(t, f)
	if _, err := g.Create(ctx(), CreateInput{Title: "   ", Body: "x"}); !errors.Is(err, ErrEmptyTitle) {
		t.Errorf("err = %v, want ErrEmptyTitle", err)
	}
	if f.calls != 0 {
		t.Errorf("empty title must not reach gh")
	}
}

func TestCreate_OverLongRejected(t *testing.T) {
	f := &fakeRunner{}
	g := newFakeTracker(t, f)
	long := strings.Repeat("x", maxTitleLen+1)
	if _, err := g.Create(ctx(), CreateInput{Title: long, Body: "x"}); !errors.Is(err, ErrTooLong) {
		t.Errorf("err = %v, want ErrTooLong", err)
	}
}

// --- Comment ---

func TestComment_BodyViaStdin(t *testing.T) {
	f := &fakeRunner{stdout: []byte("https://github.com/jim80net/flotilla/issues/5#issuecomment-1\n")}
	g := newFakeTracker(t, f)
	if err := g.Comment(ctx(), 5, "-leading dash comment"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	if string(f.gotIn) != "-leading dash comment" {
		t.Errorf("comment body must go via stdin, got %q", f.gotIn)
	}
	if !lastArgsContain(f.gotArgs, "--", "5") {
		t.Errorf("number must follow `--`; args = %v", f.gotArgs)
	}
}

func TestComment_EmptyRejected(t *testing.T) {
	f := &fakeRunner{}
	g := newFakeTracker(t, f)
	if err := g.Comment(ctx(), 5, "  "); !errors.Is(err, ErrEmptyBody) {
		t.Errorf("err = %v, want ErrEmptyBody", err)
	}
	if f.calls != 0 {
		t.Error("empty comment must not reach gh")
	}
}

// --- Label ---

func TestLabel_AddAndRemove(t *testing.T) {
	f := &fakeRunner{}
	g := newFakeTracker(t, f)
	if err := g.Label(ctx(), 7, []string{"operator-idea"}, []string{"bug"}); err != nil {
		t.Fatalf("Label: %v", err)
	}
	if v, ok := f.arg("--add-label"); !ok || v != "operator-idea" {
		t.Errorf("--add-label = %q (%v)", v, ok)
	}
	if v, ok := f.arg("--remove-label"); !ok || v != "bug" {
		t.Errorf("--remove-label = %q (%v)", v, ok)
	}
}

func TestLabel_NoChangeRejected(t *testing.T) {
	f := &fakeRunner{}
	g := newFakeTracker(t, f)
	if err := g.Label(ctx(), 7, nil, nil); !errors.Is(err, ErrNoLabelChange) {
		t.Errorf("err = %v, want ErrNoLabelChange", err)
	}
	if f.calls != 0 {
		t.Error("a no-op label change must not reach gh")
	}
}

// --- Close ---

func TestClose_HappyPath(t *testing.T) {
	f := &fakeRunner{stdout: []byte("Closed issue #9\n")}
	g := newFakeTracker(t, f)
	if err := g.Close(ctx(), 9); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !lastArgsContain(f.gotArgs, "--", "9") {
		t.Errorf("number must follow `--`; args = %v", f.gotArgs)
	}
}

func TestClose_GHFailureSurfaces(t *testing.T) {
	f := &fakeRunner{stderr: []byte("GraphQL: Could not resolve to an issue or pull request with the number of 9. (repository.issue)"), err: errors.New("exit status 1")}
	g := newFakeTracker(t, f)
	if err := g.Close(ctx(), 9); !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("err = %v, want ErrIssueNotFound", err)
	}
}

// --- ResolveDefaultRepo ---

func TestResolveDefaultRepo(t *testing.T) {
	f := &fakeRunner{stdout: []byte("jim80net/flotilla\n")}
	repo, err := resolveDefaultRepo(ctx(), f.run)
	if err != nil {
		t.Fatalf("resolveDefaultRepo: %v", err)
	}
	if repo != "jim80net/flotilla" {
		t.Errorf("repo = %q", repo)
	}
}

func TestResolveDefaultRepo_NotARepo(t *testing.T) {
	f := &fakeRunner{stderr: []byte("none of the git remotes configured for this repository point to a known GitHub host"), err: errors.New("exit status 1")}
	if _, err := resolveDefaultRepo(ctx(), f.run); err == nil {
		t.Error("resolveDefaultRepo must error when cwd is not a gh repo")
	}
}

// --- helpers ---

// lastArgsContain reports whether args contains the subsequence a,b adjacently.
func lastArgsContain(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}
