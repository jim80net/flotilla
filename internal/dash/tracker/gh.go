package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
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

// ghTimeout bounds a single gh invocation when the caller's context carries no
// deadline (the HTTP request path). A hung/slow gh (degraded GitHub, DNS stall,
// a huge paginated repo) is killed at this deadline rather than pinning a
// handler goroutine + child process indefinitely. The seam owns its own latency
// budget so every verb inherits it; a caller WITH a shorter deadline (e.g. the
// startup repo-resolve) keeps its own.
const ghTimeout = 30 * time.Second

// listCacheTTL matches the dashboard's 15-second fallback refresh cadence. A
// successful list may be reused for one cadence window; writes invalidate it
// immediately. The short bound removes cold-load/SSE subprocess fan-out without
// turning the tracker into a long-lived source of stale GitHub state.
const listCacheTTL = 15 * time.Second

// listFields / detailFields PIN the exact `gh --json` field set we parse. Pinning
// the set catches a gh output SHAPE change (a removed/retyped field → an
// ErrParse, not a silent mis-read); a field RENAME degrades to a zero value
// (encoding/json ignores unknown keys), which the env-gated live test (gh_live_test.go)
// is the canary for. Keep these in sync with the Issue struct's json tags.
const (
	listFields   = "number,title,labels,state,author,createdAt,updatedAt,closedAt"
	detailFields = "number,title,body,labels,state,author,createdAt,updatedAt,closedAt,comments,url"
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
	now  func() time.Time

	listMu      sync.Mutex
	listCache   map[listCacheKey]listCacheEntry
	listFlights map[listCacheKey]*listFlight
}

type listCacheKey struct {
	state       string
	label       string
	limit       int
	includeBody bool
}

type listCacheEntry struct {
	issues  []Issue
	expires time.Time
}

type listFlight struct {
	done   chan struct{}
	issues []Issue
	err    error
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
	return &GHTracker{
		repo:        repo,
		run:         run,
		now:         time.Now,
		listCache:   make(map[listCacheKey]listCacheEntry),
		listFlights: make(map[listCacheKey]*listFlight),
	}, nil
}

// Repo returns the pinned target repo (owner/name).
func (g *GHTracker) Repo() string { return g.repo }

// List lists issues matching filter. An empty (exit 0) result is an empty slice,
// never an error; a gh failure is a typed error.
func (g *GHTracker) List(ctx context.Context, filter ListFilter) ([]Issue, error) {
	key, err := normalizeListFilter(filter)
	if err != nil {
		return nil, err
	}

	g.listMu.Lock()
	now := g.now()
	for cachedKey, cached := range g.listCache {
		if !now.Before(cached.expires) {
			delete(g.listCache, cachedKey)
		}
	}
	if cached, ok := g.listCache[key]; ok {
		issues := cloneIssues(cached.issues)
		g.listMu.Unlock()
		return issues, nil
	}
	if flight := g.listFlights[key]; flight != nil {
		g.listMu.Unlock()
		select {
		case <-flight.done:
			return cloneIssues(flight.issues), flight.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	flight := &listFlight{done: make(chan struct{})}
	g.listFlights[key] = flight
	g.listMu.Unlock()

	issues, err := g.listUncached(ctx, key)

	g.listMu.Lock()
	flight.issues, flight.err = cloneIssues(issues), err
	// A successful write replaces listFlights, detaching pre-write work. Only
	// the still-current flight may repopulate the cache or delete this key.
	if g.listFlights[key] == flight {
		if err == nil {
			g.listCache[key] = listCacheEntry{
				issues: cloneIssues(issues), expires: g.now().Add(listCacheTTL),
			}
		}
		delete(g.listFlights, key)
	}
	close(flight.done)
	g.listMu.Unlock()
	return cloneIssues(issues), err
}

func (g *GHTracker) listUncached(ctx context.Context, key listCacheKey) ([]Issue, error) {
	fields := listFields
	if key.includeBody {
		fields = listFields + ",body"
	}
	args := []string{"issue", "list", "--repo=" + g.repo, "--json=" + fields}
	args = append(args, "--state="+key.state)
	if key.label != "" {
		args = append(args, "--label="+key.label)
	}
	args = append(args, "--limit="+strconv.Itoa(key.limit))

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
	if key.includeBody {
		for i := range issues {
			EnrichIssue(&issues[i])
		}
	}
	return issues, nil
}

func normalizeListFilter(filter ListFilter) (listCacheKey, error) {
	state := strings.ToLower(strings.TrimSpace(filter.State))
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "closed" && state != "all" {
		return listCacheKey{}, fmt.Errorf("%w: state %q (want open|closed|all)", ErrInvalidState, state)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return listCacheKey{
		state: state, label: filter.Label, limit: limit, includeBody: filter.IncludeBody,
	}, nil
}

func cloneIssues(src []Issue) []Issue {
	if src == nil {
		return nil
	}
	out := make([]Issue, len(src))
	copy(out, src)
	for i := range out {
		out[i].Labels = append([]Label(nil), src[i].Labels...)
		out[i].Comments = append([]Comment(nil), src[i].Comments...)
	}
	return out
}

func (g *GHTracker) invalidateListCache() {
	g.listMu.Lock()
	g.listCache = make(map[listCacheKey]listCacheEntry)
	// Detach in-flight pre-write reads. Their existing waiters still receive the
	// result, but post-write callers start a fresh flight and stale completions
	// cannot repopulate the new cache generation.
	g.listFlights = make(map[listCacheKey]*listFlight)
	g.listMu.Unlock()
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
	EnrichIssue(&issue)
	return issue, nil
}

// Create opens a new issue. Title/Labels go via the `--flag=value` form and the
// body via stdin (`--body-file -`) so no request content can be read as a flag.
func (g *GHTracker) Create(ctx context.Context, in CreateInput) (Issue, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Issue{}, ErrEmptyTitle
	}
	// Cap the TRIMMED title — that is the value actually sent and stored, so the
	// length check must match it (a title within the cap after trimming must not
	// be rejected for surrounding whitespace).
	if len(title) > maxTitleLen {
		return Issue{}, fmt.Errorf("%w: title (max %d)", ErrTooLong, maxTitleLen)
	}
	if len(in.Body) > maxBodyLen {
		return Issue{}, fmt.Errorf("%w: body (max %d)", ErrTooLong, maxBodyLen)
	}
	labels, err := normalizeLabels(in.Labels)
	if err != nil {
		return Issue{}, err
	}
	// Send the TRIMMED title (the empty-check trims, so the stored/sent value
	// must match — otherwise " hi " ships leading/trailing whitespace to GitHub).
	args := []string{"issue", "create", "--repo=" + g.repo, "--title=" + title, "--body-file=-"}
	for _, l := range labels {
		args = append(args, "--label="+l)
	}
	out, errb, rerr := g.run(ctx, args, []byte(in.Body))
	if rerr != nil {
		return Issue{}, classify(errb, rerr)
	}
	g.invalidateListCache()
	// `gh issue create` prints the new issue URL (it has no --json). Parse the
	// number from the URL's last path segment; the frontend refetches the list.
	url := strings.TrimSpace(string(out))
	num := issueNumberFromURL(url)
	return Issue{Number: num, URL: url, State: "OPEN", Title: title}, nil
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
	g.invalidateListCache()
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
	addN, err := normalizeLabels(add)
	if err != nil {
		return err
	}
	removeN, err := normalizeLabels(remove)
	if err != nil {
		return err
	}
	args := []string{"issue", "edit", "--repo=" + g.repo}
	for _, l := range addN {
		args = append(args, "--add-label="+l)
	}
	for _, l := range removeN {
		args = append(args, "--remove-label="+l)
	}
	args = append(args, "--", strconv.Itoa(number))
	_, errb, err := g.run(ctx, args, nil)
	if err != nil {
		return classify(errb, err)
	}
	g.invalidateListCache()
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
	g.invalidateListCache()
	return nil
}

// --- helpers ---

// normalizeLabels trims each label and rejects empty/over-long labels and an
// over-count list, returning the cleaned labels to send. Labels may contain
// spaces and most characters; the `--label=value` form means a leading `-` is
// not a flag, so only emptiness, length, and count are checked. Trimming here
// means " bug " and "bug" are the same label (not two distinct GitHub labels).
func normalizeLabels(labels []string) ([]string, error) {
	if len(labels) > maxLabelCount {
		return nil, fmt.Errorf("%w: labels (max %d)", ErrTooLong, maxLabelCount)
	}
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		t := strings.TrimSpace(l)
		if t == "" {
			return nil, ErrEmptyLabel
		}
		if len(t) > maxLabelLen {
			return nil, fmt.Errorf("%w: label (max %d)", ErrTooLong, maxLabelLen)
		}
		out = append(out, t)
	}
	return out, nil
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
	// Process-level failures the stderr can't describe come first.
	if errors.Is(runErr, context.DeadlineExceeded) {
		return ErrTimeout
	}
	if errors.Is(runErr, exec.ErrNotFound) {
		return ErrGHMissing
	}
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
// captures stdout/stderr separately (stderr drives error classification). When
// the caller's context carries no deadline (the HTTP request path), it imposes
// ghTimeout so a hung gh is killed instead of pinning the goroutine; a timeout
// surfaces as context.DeadlineExceeded so classify maps it to ErrTimeout.
func execRunner(ctx context.Context, args []string, stdin []byte) ([]byte, []byte, error) {
	// Bound only a deadline-free call (the HTTP request path); a caller that
	// already set a deadline (e.g. the startup resolve) keeps its own. Note this
	// does not CLAMP a longer caller deadline — today every caller is either
	// deadline-free or sets a deadline shorter than ghTimeout.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ghTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	// When CommandContext kills the child on deadline, cmd.Run() returns a
	// "signal: killed" error; report the cause so classify recognizes it.
	if ctx.Err() == context.DeadlineExceeded {
		return out.Bytes(), errb.Bytes(), context.DeadlineExceeded
	}
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
