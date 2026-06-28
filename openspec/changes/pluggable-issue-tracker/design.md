# Design — pluggable issue-tracker interface (#103)

> **Design-first; first concrete seam of the modularity pass (#104).** Mirrors the
> established `surface.Driver` registry pattern (operator's explicit instruction: "registry
> + config-selected strategy, NOT a new bespoke mechanism"). GitHub is the default; Linear
> and Jira are documented stubs. The implementation is a follow-on lane.

## 1. The problem

The XO files operator ideas / fleet tasks as **GitHub issues** — the visible idea backlog
the operator browses. Today that is hard-wired: the XO shells out to `gh issue create|list|…`
directly, so the behavior only exists for GitHub. A flotilla user on Linear or Jira gets no
equivalent. flotilla's differentiator is config-selected pluggability (the `surface.Driver`
registry; the `SpeechProvider` seam) — the issue tracker should be the same kind of seam.

## 2. The model: a Tracker strategy, registry-selected (mirrors surface.Driver)

```
  flotilla issue create|list|update|close      roster: { "tracker": "github" }
            │                                            │
            ▼                                            ▼
     cmd/flotilla/issue.go ───────────► tracker.Get(cfg.Tracker)  ─┐
                                                                   │ registry (init()-registered)
                                  ┌────────────────────────────────┼───────────────┐
                                  ▼                ▼                ▼                ▼
                            githubTracker     linearTracker     jiraTracker     (future …)
                            (gh CLI; default) (stub: errors)   (stub: errors)
```

`tracker.Tracker` is the strategy interface; concrete strategies self-register in `init()`;
`cmd/flotilla` resolves the configured one by name and dispatches the `flotilla issue`
subcommands to it. This is structurally identical to `internal/surface` (Driver + `registry`
+ `Register`/`Get` + startup validation), so it adds no new architectural concept.

## 3. The interface

```go
// Package tracker abstracts the fleet's issue tracker behind a config-selected strategy:
// GitHub Issues (default), with Linear/Jira as plugins. Mirrors internal/surface.
package tracker

// Issue is the tracker-agnostic view of one work item. ID is the stable handle the
// tracker addresses an issue by (a GitHub issue NUMBER as a string, a Linear issue id,
// a Jira key); strategies map their native shape onto it. URL is the human link.
type Issue struct {
    ID     string   // stable handle for Update/Close (e.g. "115")
    Title  string
    Body   string
    State  string   // "open" | "closed" (normalized across trackers)
    Labels []string
    URL    string
}

// Draft is a new issue to create.
type Draft struct {
    Title  string
    Body   string
    Labels []string
}

// Query filters ListIssues. Zero value lists open issues (the common "what's on the
// board" case). State "" means open; "all" means open+closed.
type Query struct {
    State  string   // "" (open) | "open" | "closed" | "all"
    Labels []string // AND-filter; empty = no label filter
    Limit  int      // 0 = the strategy's sensible default
}

// Patch is a partial update; nil fields are left unchanged.
type Patch struct {
    Title  *string
    Body   *string
    Labels *[]string
    State  *string  // "open" | "closed"
}

// Tracker is the config-selected issue-tracker strategy.
type Tracker interface {
    Name() string                          // registry key ("github", "linear", "jira")
    CreateIssue(d Draft) (Issue, error)
    ListIssues(q Query) ([]Issue, error)
    UpdateIssue(id string, p Patch) (Issue, error)
    CloseIssue(id string) error            // sugar for UpdateIssue(id, {State:"closed"})
}
```

- **`CloseIssue` is kept distinct** (the operator named it) even though it is `UpdateIssue`
  with `State:"closed"` — it is the most common state transition and reads clearly at the
  call site; the GitHub strategy implements it as exactly that.
- **Optional capabilities via type-assertion** (the `surface.ResultReader` / `ComposerProbe`
  idiom) are available for tracker-specific features later (e.g. `Commenter`, `Assigner`)
  without widening the core interface — out of scope here, but the seam supports it.

## 4. Registry + selection (identical to surface)

```go
const DefaultTracker = "github"

var registry = map[string]Tracker{}
func Register(t Tracker)               { registry[t.Name()] = t }
func Get(name string) (Tracker, bool)  { if name == "" { name = DefaultTracker }; t, ok := registry[name]; return t, ok }
```

- Each strategy registers in its own `init()` (`github.go`, `linear.go`, `jira.go`), exactly
  like `surface/claude.go`.
- The roster gains an optional `tracker` string (the registry key). Empty ⇒ `github`.
- **Fail-closed startup validation**: `cmd/flotilla` validates `tracker.Get(cfg.Tracker)`
  resolves before acting (the `validateAgentSurfaces` precedent) — an unknown tracker is a
  clear error, never a silent mis-dispatch.

## 5. The GitHub strategy — a `gh` CLI wrapper (the key decision)

**Decision: the default GitHub strategy shells out to the `gh` CLI, not the GitHub REST/GraphQL API directly.** Rationale:

- **It reuses the operator's existing `gh auth`** — zero new secret/token to configure. The
  operator and the XO already have `gh` authenticated; the daemon/host already relies on it.
- **It matches what the XO does TODAY** (`gh issue create|list|…`) — this change *productizes*
  the existing path behind the interface, it does not introduce a new auth/transport surface.
- **It is consistent with flotilla's existing posture** of orchestrating external CLIs
  (it shells to `tmux` throughout `internal/deliver`); `gh` is the same kind of dependency.
- **Repo inference is free**: `gh` resolves the target repo from the cwd's git remote, so a
  desk filing an issue from its checkout targets the right repo with no config.

Mapping (`gh` invocations, JSON out via `--json`):
- `CreateIssue` → `gh issue create --title … --body … [--label …] --json number,url` (or parse
  the printed URL); returns the new `Issue`.
- `ListIssues` → `gh issue list --state … [--label …] --limit N --json number,title,body,state,labels,url`.
- `UpdateIssue` → `gh issue edit <id> [--title|--body|--add-label|--remove-label] …`; state
  changes via `gh issue close`/`reopen`.
- `CloseIssue` → `gh issue close <id>`.

**Trade-off acknowledged:** this depends on `gh` being installed + authenticated. That is
acceptable for v1 (it is already a hard precondition of the operator's workflow) and is
surfaced as a clear error if `gh` is absent. A **direct-API GitHub strategy** (token-based, no
`gh` dependency) is a clean future registry entry — it is exactly the kind of second
implementation the registry exists to allow, and it would register under a different key
(e.g. `github-api`) without touching the `gh`-based one. The `gh`-wrapper is the right v1.

## 6. The CLI surface

`flotilla issue <sub>` — the tracker-agnostic command the XO calls instead of raw `gh issue`:

- `flotilla issue create --title T [--body B|--file f|-] [--label L]…`  → prints the new URL.
- `flotilla issue list [--state open|closed|all] [--label L]… [--limit N]`  → one line per issue.
- `flotilla issue update <id> [--title|--body|--file|--add-label|--remove-label|--state]…`
- `flotilla issue close <id>`

Message/body resolution reuses the existing `--file`/`-`(stdin) helper that `send`/`notify`
use (consistency; one resolution path). The roster `tracker` selects the backend; `--tracker`
may override per-call (precedence: flag > roster > default `github`), matching the
`send --mirror` override precedent.

## 7. Linear / Jira — documented stubs (the door, not the room)

`linear.go` and `jira.go` register strategies named `"linear"` / `"jira"` whose methods return
a clear `ErrNotImplemented` ("the linear tracker strategy is not yet implemented — configure
`tracker: github`, or contribute the linear strategy"). This makes the seam **real and
testable** (selecting `tracker: linear` resolves a registered strategy and fails with a
helpful message, not an "unknown tracker" startup error), documents the intended plugins, and
gives a concrete skeleton for a contributor — without building two API integrations now.

(Open question for the implementer: whether a not-yet-implemented strategy should
fail at **startup validation** or only when a `flotilla issue` command is actually run. Lean:
register it so it RESOLVES, and return `ErrNotImplemented` from the methods — so a Linear-
intending operator gets a precise "not implemented yet" rather than "unknown tracker", and the
daemon/other commands are unaffected by an aspirational tracker setting.)

## 8. DECISIONS

1. **GitHub via `gh` CLI vs direct API.** *Recommend `gh` wrapper for v1* (§5) — reuses
   auth, matches today's behavior, consistent with the tmux posture; direct-API is a clean
   future registry entry. (Reversible; not a fundamental fork.)
2. **Backlog/goal-loop integration.** Should `ListIssues` feed the goal-driven loop's drive
   queue (issues-as-backlog)? *Recommend NO for #103* — the tracker is the XO's
   issue-management interface in v1; unifying it with the markdown backlog source is a
   larger, separable change (#104/future). Keeping it out bounds this design.
3. **`tracker` config: string vs object.** *Recommend a string kind for v1* (`"github"`),
   documenting that future plugins (Linear/Jira) will likely need their own config block
   (workspace/project/token) added when they are built. Start minimal.
4. **`flotilla issue` vs reusing an existing command.** *Recommend a new `issue` command
   family* — it is a distinct noun (issue CRUD), parallel to `send`/`notify`/`status`, and
   keeps the surface discoverable.

## 9. Non-goals

- **No Linear/Jira implementation** — stubs only (the seam + the documented plan).
- **No new auth surface** — the GitHub strategy uses existing `gh auth`; it adds no token
  config. (A future direct-API strategy would, under its own key.)
- **No backlog/goal-loop rewiring** — decision 8.2; the tracker does not become a backlog
  source in this change.
- **No daemon/relay/surface changes** — `flotilla issue` is a one-shot command; the `watch`
  daemon is untouched.

## 10. Phasing

- **This change (design):** the `Tracker` interface + registry, the `gh`-based GitHub
  strategy, the `tracker` roster field + startup validation, the `flotilla issue` CLI, and the
  Linear/Jira stubs — delivered as a reviewed design + `tracker` spec. Trio-reviewed; merged
  as the blessed design.
- **Follow-on (implementation lane):** TDD-implement the above (the `tasks.md` here is the
  plan), with a fake `Tracker` for the CLI tests and a thin, mock-`gh`-exec'd github test.
- **Future:** a direct-API GitHub strategy (decision 8.1); real Linear/Jira strategies;
  issues-as-backlog-source (decision 8.2, ties to #104).
