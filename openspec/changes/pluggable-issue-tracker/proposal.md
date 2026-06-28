# Proposal — pluggable issue-tracker interface (#103)

## Why

The XO has started filing operator ideas and fleet tasks as GitHub issues — the
*visible idea backlog* the operator browses (e.g. issues #115, #116 filed this session).
That file/list/update/close path is currently hard-wired to GitHub: the XO shells out to
`gh issue …` directly. A team running flotilla against **Linear** or **Jira** gets none
of that visible-backlog behavior.

flotilla already leans on **config-selected, registry-based pluggable strategies** — the
`surface.Driver` per-harness registry (claude-code / grok / cursor / aider / opencode,
registered via `init()`, resolved by name, validated at startup) and the `SpeechProvider`
voice seam. The issue-tracker is the next natural seam, and the operator named it as the
**first concrete formalization** in the broader modularity pass (#104).

## What changes

Introduce a `tracker` capability: a `Tracker` strategy interface
(`CreateIssue` / `ListIssues` / `UpdateIssue` / `CloseIssue`) with a registry that mirrors
`surface.Driver` exactly — strategies self-register in `init()`, the active one is selected
from the roster, and an unknown selection is a fail-closed startup error.

- **GitHub is the default strategy**, implemented as a thin wrapper over the `gh` CLI — the
  same path the XO uses today, reusing the operator's existing `gh auth` with no new secret
  to configure.
- **A new `flotilla issue` command family** (`create` / `list` / `update` / `close`) is the
  tracker-agnostic surface the XO calls instead of raw `gh issue`. On a GitHub fleet it
  behaves exactly as today; on a Linear/Jira fleet (once those strategies exist) the same
  commands hit the configured tracker.
- **Linear and Jira are documented stub strategies** — named in the design, registered as
  "not yet implemented" so the seam is real and the door is open, without building them now.

This is **design-first**: this change delivers the interface, the GitHub strategy, config
selection, the CLI surface, and the Linear/Jira stubs as a reviewed design + spec. The
implementation is a follow-on lane.

## Impact

- **New capability:** `tracker` (`openspec/specs/tracker/`).
- **New package:** `internal/tracker` (interface + registry + GitHub strategy + stubs).
- **New CLI command:** `flotilla issue create|list|update|close`.
- **Roster:** a new optional `tracker` field (kind; default `github`), validated at load.
- **Backward compatible:** the field is optional and defaults to GitHub; nothing existing
  changes. No daemon/relay/surface code is touched.
- **Explicitly out of scope (documented decision):** wiring `ListIssues` into the
  goal-driven loop's backlog source — the tracker is the XO's issue-management interface in
  v1, not (yet) a backlog feed. That integration is deferred to #104/future.
