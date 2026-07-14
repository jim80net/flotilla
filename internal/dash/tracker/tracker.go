// Package tracker is the dash's native, GitHub-backed issue tracker. It exposes
// a small interface (List/Get/Create/Comment/Label/Close) with EXACTLY ONE
// implementation, backed by the `gh` CLI (gh.go). The interface exists ONLY so
// the HTTP handlers and tests do not bind to a subprocess directly (inject a
// fake) — NOT to host multiple trackers. There is deliberately no strategy
// registry and no config-selected provider: Linear/Jira are the separate,
// deferred #103 abstraction (operator-ratified), and building it now would be
// exactly the premature multi-tracker generality #103 warns against. If #103
// ever lands, this single backend slots behind its interface unchanged.
//
// All free-form, request-derived content (titles, bodies, comments, labels) is
// passed to `gh` injection-safely (gh.go) and the target repo is PINNED at
// startup — a request can never select an arbitrary repo. Every `gh` failure
// (unauthenticated / rate-limited / repo-not-found / network / unparseable) maps
// to a typed error here so the UI surfaces it honestly, never a swallowed
// failure or an empty list masquerading as "no issues" (the project's
// silent-failure discipline).
package tracker

import (
	"context"
	"errors"
)

// Tracker is the minimal seam over the issue backend. The one implementation is
// the `gh`-backed GHTracker (gh.go); tests inject a fake.
type Tracker interface {
	// List returns the repo's issues matching filter (open by default), newest
	// activity first (the backend's natural order), or a typed error.
	List(ctx context.Context, filter ListFilter) ([]Issue, error)
	// Get returns one issue with its body and comments, or a typed error
	// (ErrIssueNotFound when the number does not exist).
	Get(ctx context.Context, number int) (Issue, error)
	// Create opens a new issue and returns it (number + URL), or a typed error.
	Create(ctx context.Context, in CreateInput) (Issue, error)
	// Comment appends a comment to an issue, or returns a typed error.
	Comment(ctx context.Context, number int, body string) error
	// Label adds and/or removes labels on an issue, or returns a typed error.
	Label(ctx context.Context, number int, add, remove []string) error
	// Close closes an issue (a destructive verb — the UI confirms it), or
	// returns a typed error.
	Close(ctx context.Context, number int) error
}

// ListFilter narrows a List call. The zero value lists OPEN issues with the
// backend default limit. State is "open" (default), "closed", or "all"; Label
// optionally filters to one label (e.g. the XO's "operator-idea"); Limit caps
// the result count (0 ⇒ DefaultLimit).
type ListFilter struct {
	State string
	Label string
	Limit int
	// IncludeBody requests issue bodies from gh (heavier fetch) so goal-id trailers
	// can be parsed. The issues list UI leaves this false; the goals read path sets it.
	IncludeBody bool
}

// CreateInput is a new-issue request. Title is required; Body and Labels are
// optional. All fields are request-derived and are passed to `gh`
// injection-safely (Body via stdin, Title/Labels via the `--flag=value` form so
// a leading `-` can never be read as a flag).
type CreateInput struct {
	Title  string
	Body   string
	Labels []string
}

// Issue is one GitHub issue. The list view populates the header fields; Get
// additionally populates Body and Comments. The JSON tags match the `gh`
// `--json` field names so one struct both unmarshals gh's output and serializes
// to the dash frontend (unknown gh fields like an author's id are ignored).
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"` // "OPEN" | "CLOSED"
	Labels    []Label   `json:"labels"`
	Author    User      `json:"author"`
	CreatedAt string    `json:"createdAt,omitempty"`
	UpdatedAt string    `json:"updatedAt,omitempty"`
	ClosedAt  string    `json:"closedAt,omitempty"`
	URL       string    `json:"url,omitempty"`
	Body      string    `json:"body,omitempty"`     // detail (Get) only
	Comments  []Comment `json:"comments,omitempty"` // detail (Get) only
	GoalID    string    `json:"goal_id,omitempty"`  // parsed from body `goal-id:` trailer (read path)
}

// Label is a GitHub label (name + color for the UI chip; description optional).
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// User is the minimal author shape the UI shows (the login).
type User struct {
	Login string `json:"login"`
}

// Comment is one issue comment (author + body + timestamp).
type Comment struct {
	Author    User   `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// Typed errors. Every `gh` failure mode maps to one of these so the HTTP layer
// can choose a status + an honest message and NEVER swallow a failure. They are
// sentinels (compare with errors.Is); the generic ErrGH wraps an unrecognized
// gh failure together with its stderr so even the unknown case is surfaced.
var (
	// ErrNoRepo: no GitHub repo was resolved/pinned at startup (the working dir
	// is not a gh-resolvable repo and no --repo was given). The tracker is
	// disabled until a repo is configured.
	ErrNoRepo = errors.New("no GitHub repo configured for the tracker (pass --repo owner/name)")
	// ErrUnauthenticated: gh has no valid GitHub credentials.
	ErrUnauthenticated = errors.New("gh is not authenticated — run `gh auth login`")
	// ErrRateLimited: the GitHub API rate limit was exceeded.
	ErrRateLimited = errors.New("GitHub API rate limit exceeded — try again later")
	// ErrRepoNotFound: the pinned repo does not exist or is not accessible.
	ErrRepoNotFound = errors.New("repository not found or not accessible (check --repo and your gh access)")
	// ErrIssueNotFound: the requested issue number does not exist.
	ErrIssueNotFound = errors.New("issue not found")
	// ErrNetwork: gh could not reach GitHub (offline / DNS / connection).
	ErrNetwork = errors.New("could not reach GitHub (check your network connection)")
	// ErrTimeout: a gh invocation exceeded the per-call deadline (a hung/slow gh
	// or a degraded GitHub). The subprocess is killed; the UI surfaces a timeout
	// rather than letting a request hang indefinitely.
	ErrTimeout = errors.New("gh timed out reaching GitHub")
	// ErrGHMissing: the `gh` CLI is not installed or not on PATH (a first-run
	// host-setup failure, distinct from an authenticated gh that errored).
	ErrGHMissing = errors.New("the `gh` CLI is not installed or not on PATH")
	// ErrParse: gh exited 0 but its output could not be parsed into the pinned
	// field shape (a gh output-format change — detected, never silently mis-read).
	ErrParse = errors.New("could not parse gh output")
	// ErrGH wraps any other non-zero gh exit (its stderr is appended).
	ErrGH = errors.New("gh command failed")
	// ErrInvalidNumber: an issue number that is not a positive integer.
	ErrInvalidNumber = errors.New("issue number must be a positive integer")
	// ErrInvalidRepo: a --repo value that is not a safe owner/name.
	ErrInvalidRepo = errors.New("repo must be in owner/name form")
	// ErrEmptyTitle: a create request with no title.
	ErrEmptyTitle = errors.New("issue title is required")
	// ErrEmptyBody: a comment request with no body.
	ErrEmptyBody = errors.New("comment body is required")
	// ErrEmptyLabel: a label that is empty or whitespace-only.
	ErrEmptyLabel = errors.New("a label is empty")
	// ErrTooLong: a field exceeds its length cap.
	ErrTooLong = errors.New("field exceeds the maximum length")
	// ErrNoLabelChange: a label request with neither add nor remove.
	ErrNoLabelChange = errors.New("label change requires at least one label to add or remove")
	// ErrInvalidState: a List state filter outside {open, closed, all}.
	ErrInvalidState = errors.New("state must be open, closed, or all")
)
