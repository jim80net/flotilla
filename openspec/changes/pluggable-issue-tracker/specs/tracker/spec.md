# tracker Specification (delta: new capability)

> Adds the `tracker` capability: a config-selected issue-tracker strategy
> (`CreateIssue`/`ListIssues`/`UpdateIssue`/`CloseIssue`) with GitHub as the default
> (a `gh`-CLI wrapper), Linear/Jira as documented stub strategies, surfaced via a new
> `flotilla issue` command. Mirrors the `surface.Driver` registry pattern. Backward
> compatible (the roster `tracker` field is optional and defaults to GitHub).

## ADDED Requirements

### Requirement: A config-selected issue-tracker strategy

The system SHALL define a `Tracker` strategy interface — `CreateIssue`, `ListIssues`,
`UpdateIssue`, `CloseIssue` over a tracker-agnostic `Issue` shape — and select the active
strategy from the roster's optional `tracker` field via a name-keyed registry, mirroring the
`surface.Driver` pattern (strategies self-register; an empty selection resolves to the
default `github`). An unknown `tracker` value SHALL be a fail-closed startup error.

#### Scenario: The default tracker is GitHub
- **WHEN** a roster does not set `tracker`
- **THEN** the `github` strategy is selected, exactly as if `tracker: "github"` were set

#### Scenario: An unknown tracker fails closed at startup
- **WHEN** a roster sets `tracker` to a name that no registered strategy provides
- **THEN** the command fails with a clear error naming the unknown tracker, before any issue operation

### Requirement: The GitHub strategy reuses the gh CLI

The default GitHub strategy SHALL perform issue operations through the authenticated `gh`
CLI (the path the XO already uses), introducing no new token/secret configuration. It SHALL
map each `Tracker` operation onto the corresponding `gh issue` invocation and return the
tracker-agnostic `Issue` shape. A missing or unauthenticated `gh` SHALL surface a clear
error, never a silent no-op.

#### Scenario: Creating an issue files it on GitHub via gh
- **WHEN** `flotilla issue create --title T --body B --label operator-idea` runs on a `github`-configured fleet
- **THEN** a GitHub issue is created via `gh issue create` and its URL is reported

#### Scenario: gh absent surfaces a clear error
- **WHEN** the `github` strategy runs but `gh` is not installed or not authenticated
- **THEN** the command fails with a clear error (it does not report success and does not silently skip)

### Requirement: A tracker-agnostic `flotilla issue` command

The system SHALL provide a `flotilla issue` command family — `create`, `list`, `update`,
`close` — that dispatches to the configured `Tracker` strategy, so the same commands work
against whatever tracker a fleet configures. Body/message input SHALL reuse the existing
`--file`/stdin resolution used by `send`/`notify`. A `--tracker` flag SHALL override the
roster selection for a single call (precedence: flag > roster > default).

#### Scenario: The same command targets the configured tracker
- **WHEN** the XO runs `flotilla issue create …` on a fleet configured with `tracker: github`
- **THEN** the issue is filed on GitHub; were the fleet configured for another implemented tracker, the identical command would file it there

#### Scenario: Closing an issue
- **WHEN** `flotilla issue close <id>` runs
- **THEN** the addressed issue is transitioned to the closed state on the configured tracker

### Requirement: Linear and Jira are registered, not-yet-implemented strategies

The system SHALL register `linear` and `jira` strategies that resolve (so selecting them is
not an "unknown tracker" error) but return a clear not-implemented error from their
operations, documenting the intended plugins and the contribution seam without providing a
working implementation in this change.

#### Scenario: Selecting an unimplemented strategy gives a precise message
- **WHEN** a roster sets `tracker: "linear"` and a `flotilla issue` command runs
- **THEN** the command fails with a clear "linear strategy not yet implemented" message (NOT "unknown tracker"), guiding the operator to use `github` or contribute the strategy
