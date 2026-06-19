package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Field caps for request-derived content. They bound a single request's blast
// radius (and keep the dash from shipping a multi-megabyte argv/stdin to gh);
// they are generous relative to GitHub's own limits.
const (
	maxTitleLen   = 256
	maxBodyLen    = 65536
	maxLabelLen   = 128
	maxLabelCount = 30
	// DefaultLimit caps a List when ListFilter.Limit is 0.
	DefaultLimit = 50
	// maxLimit caps a caller-supplied List limit (defense against a huge fetch).
	maxLimit = 200
)

// listFields / detailFields PIN the exact `gh --json` field set we parse. Pinning
// the set (never the default shape) means a gh output change is DETECTED at parse
// time, not silently mis-read (design §6). Keep these in sync with the Issue
// struct's json tags.
const (
	listFields   = "number,title,labels,state,author,updatedAt"
	detailFields = "number,title,body,labels,state,author,createdAt,updatedAt,comments,url"
)

// repoPattern validates a --repo value as a safe owner/name: each segment starts
// with an alphanumeric (so a value can never begin with "-" and be read as a
// flag) and contains only GitHub-legal name characters. The repo is operator-
// supplied and pinned at startup, but validating it is cheap defense-in-depth.
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ghRunner executes a `gh` subprocess: args is the full argv after "gh", stdin
// is fed to the process (nil for none). It returns stdout, stderr, and the
// process error (non-nil on a non-zero exit). This seam is the ONLY coupling to
// os/exec, so tests inject a fake gh (canned stdout / stderr / error); the real
// implementation is execRunner.
type ghRunner func(ctx context.Context, args []string, stdin []byte) (stdout, stderr []byte, err error)

// GHTracker is the one Tracker implementation: it shells out to `gh` against a
// repo PINNED at construction. The repo is never request-derived, so a client
// can never retarget an arbitrary repository.
type GHTracker struct {
	repo string   // pinned owner/name, passed as --repo=<repo> on every call
	run  ghRunner // injected runner (real = execRunner)
}

// NewGH builds a gh-backed tracker for the pinned repo, validating the repo
// shape (fail-closed: an invalid repo yields ErrInvalidRepo, never a tracker
// that could be coaxed into a bad --repo). repo must be non-empty owner/name;
// callers that have no repo should not construct a tracker (the server treats a
// nil tracker as "not configured" → ErrNoRepo).
func NewGH(repo string) (*GHTracker, error) {
	return newGH(repo, execRunner)
}

// newGH is NewGH with an injectable runner (tests use a fake).
func newGH(repo string, run ghRunner) (*GHTracker, error) {
	if !repoPattern.MatchString(repo) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRepo, repo)
	}
	return &GHTracker{repo: repo, run: run}, nil
}

// Repo returns the pinned target repo (owner/name).
func (g *GHTracker) Repo() string { return g.repo }

// List lists issues matching filter. An empty (exit 0) result is an empty slice,
// never an error; a gh failure is a typed error.
func (g *GHTracker) List(ctx context.Context, filter ListFilter) ([]Issue, error) {
	args := []string{"issue", "list", "--repo=" + g.repo, "--json=" + listFields}
	state := strings.ToLower(strings.TrimSpace(filter.State))
	if state == "" {
		state = "open"
	}
	args = append(args, "--state="+state)
	if filter.Label != "" {
		args = append(args, "--label="+filter.Label)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	args = append(args, "--limit="+strconv.Itoa(limit))

	out, errb, err := g.run(ctx, args, nil)
	if err != nil {
		return nil, classify(errb, err)
	}
	var issues []Issue
	if uerr := json.Unmarshal(out, &issues); uerr != nil {
		return nil, fmt.Errorf("%w: gh issue list: %v", ErrParse, uerr)
	}
	if issues == nil {
		issues = []Issue{}
	}
	return issues, nil
}

// Get returns one issue with body + comments.
func (g *GHTracker) Get(ctx context.Context, number int) (Issue, error) {
	if number <= 0 {
		return Issue{}, ErrInvalidNumber
	}
	// Flags first, then the validated number after `--` (defense-in-depth: the
	// number is already a positive int, so it can never be read as a flag).
	args := []string{"issue", "view", "--repo=" + g.repo, "--json=" + detailFields, "--", strconv.Itoa(number)}
	out, errb, err := g.run(ctx, args, nil)
	if err != nil {
		return Issue{}, classify(errb, err)
	}
	var issue Issue
	if uerr := json.Unmarshal(out, &issue); uerr != nil {
		return Issue{}, fmt.Errorf("%w: gh issue view: %v", ErrParse, uerr)
	}
	return issue, nil
}

// Create opens a new issue. Title/Labels go via the `--flag=value` form and the
// body via stdin (`--body-file -`) so no request content can be read as a flag.
func (g *GHTracker) Create(ctx context.Context, in CreateInput) (Issue, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Issue{}, ErrEmptyTitle
	}
	if len(in.Title) > maxTitleLen {
		return Issue{}, fmt.Errorf("%w: title (max %d)", ErrTooLong, maxTitleLen)
	}
	if len(in.Body) > maxBodyLen {
		return Issue{}, fmt.Errorf("%w: body (max %d)", ErrTooLong, maxBodyLen)
	}
	if err := validateLabels(in.Labels); err != nil {
		return Issue{}, err
	}
	args := []string{"issue", "create", "--repo=" + g.repo, "--title=" + in.Title, "--body-file=-"}
	for _, l := range in.Labels {
		args = append(args, "--label="+l)
	}
	out, errb, err := g.run(ctx, args, []byte(in.Body))
	if err != nil {
		return Issue{}, classify(errb, err)
	}
	// `gh issue create` prints the new issue URL (it has no --json). Parse the
	// number from the URL's last path segment; the frontend refetches the list.
	url := strings.TrimSpace(string(out))
	num := issueNumberFromURL(url)
	return Issue{Number: num, URL: url, State: "OPEN", Title: in.Title}, nil
}

// Comment appends a comment, body via stdin (injection-safe).
func (g *GHTracker) Comment(ctx context.Context, number int, body string) error {
	if number <= 0 {
		return ErrInvalidNumber
	}
	if strings.TrimSpace(body) == "" {
		return ErrEmptyBody
	}
	if len(body) > maxBodyLen {
		return fmt.Errorf("%w: comment (max %d)", ErrTooLong, maxBodyLen)
	}
	args := []string{"issue", "comment", "--repo=" + g.repo, "--body-file=-", "--", strconv.Itoa(number)}
	_, errb, err := g.run(ctx, args, []byte(body))
	if err != nil {
		return classify(errb, err)
	}
	return nil
}

// Label adds and/or removes labels. Labels go via `--add-label=`/`--remove-label=`
// (the `=value` form is injection-safe even for a label starting with `-`).
func (g *GHTracker) Label(ctx context.Context, number int, add, remove []string) error {
	if number <= 0 {
		return ErrInvalidNumber
	}
	if len(add) == 0 && len(remove) == 0 {
		return ErrNoLabelChange
	}
	if err := validateLabels(add); err != nil {
		return err
	}
	if err := validateLabels(remove); err != nil {
		return err
	}
	args := []string{"issue", "edit", "--repo=" + g.repo}
	for _, l := range add {
		args = append(args, "--add-label="+l)
	}
	for _, l := range remove {
		args = append(args, "--remove-label="+l)
	}
	args = append(args, "--", strconv.Itoa(number))
	_, errb, err := g.run(ctx, args, nil)
	if err != nil {
		return classify(errb, err)
	}
	return nil
}

// Close closes an issue (destructive — the UI confirms it).
func (g *GHTracker) Close(ctx context.Context, number int) error {
	if number <= 0 {
		return ErrInvalidNumber
	}
	args := []string{"issue", "close", "--repo=" + g.repo, "--", strconv.Itoa(number)}
	_, errb, err := g.run(ctx, args, nil)
	if err != nil {
		return classify(errb, err)
	}
	return nil
}

// --- helpers ---

// validateLabels rejects empty/over-long labels and an over-count list. Labels
// may contain spaces and most characters; the `--label=value` form means a
// leading `-` is not a flag, so only emptiness, length, and count are checked.
func validateLabels(labels []string) error {
	if len(labels) > maxLabelCount {
		return fmt.Errorf("%w: labels (max %d)", ErrTooLong, maxLabelCount)
	}
	for _, l := range labels {
		if strings.TrimSpace(l) == "" {
			return fmt.Errorf("%w: a label is empty", ErrTooLong)
		}
		if len(l) > maxLabelLen {
			return fmt.Errorf("%w: label (max %d)", ErrTooLong, maxLabelLen)
		}
	}
	return nil
}

var issueURLTail = regexp.MustCompile(`/(\d+)\s*$`)

// issueNumberFromURL extracts the trailing issue number from a gh-printed URL
// (https://github.com/owner/repo/issues/123). Returns 0 if absent (the create
// still succeeded; the frontend's refetch is authoritative for the number).
func issueNumberFromURL(url string) int {
	m := issueURLTail.FindStringSubmatch(url)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// classify maps a gh non-zero exit + its stderr onto a typed error. The order
// matters: the specific GraphQL "could not resolve" messages are checked before
// the generic fallback. The patterns were verified against gh 2.45 live output
// (repo-not-found, issue-not-found, HTTP 401, rate limit, connection failure).
func classify(stderr []byte, runErr error) error {
	s := string(stderr)
	switch {
	case strings.Contains(s, "Could not resolve to a Repository"):
		return ErrRepoNotFound
	case strings.Contains(s, "Could not resolve to an issue") ||
		strings.Contains(s, "Could not resolve to a PullRequest"):
		return ErrIssueNotFound
	case strings.Contains(s, "rate limit"):
		return ErrRateLimited
	case strings.Contains(s, "Bad credentials") ||
		strings.Contains(s, "HTTP 401") ||
		strings.Contains(s, "gh auth login"):
		return ErrUnauthenticated
	case strings.Contains(s, "error connecting") ||
		strings.Contains(s, "check your internet connection") ||
		strings.Contains(s, "dial tcp") ||
		strings.Contains(s, "no such host"):
		return ErrNetwork
	default:
		msg := strings.TrimSpace(s)
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		return fmt.Errorf("%w: %s", ErrGH, msg)
	}
}

// execRunner is the real ghRunner: it runs `gh <args>` with stdin piped, and
// captures stdout/stderr separately (stderr drives error classification).
func execRunner(ctx context.Context, args []string, stdin []byte) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

// ResolveDefaultRepo asks gh for the working directory's repo (owner/name), used
// when --repo is not given. A failure (cwd is not a repo, gh unauth) returns a
// typed error so the caller can disable the tracker with a clear message rather
// than fail the whole dash.
func ResolveDefaultRepo(ctx context.Context) (string, error) {
	return resolveDefaultRepo(ctx, execRunner)
}

func resolveDefaultRepo(ctx context.Context, run ghRunner) (string, error) {
	out, errb, err := run(ctx, []string{"repo", "view", "--json=nameWithOwner", "--jq=.nameWithOwner"}, nil)
	if err != nil {
		return "", classify(errb, err)
	}
	repo := strings.TrimSpace(string(out))
	if !repoPattern.MatchString(repo) {
		return "", fmt.Errorf("%w: gh returned %q", ErrInvalidRepo, repo)
	}
	return repo, nil
}
