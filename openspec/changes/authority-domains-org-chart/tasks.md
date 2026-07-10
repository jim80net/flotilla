# Tasks — authority-domains-org-chart (slice 1)

## Schema + load validation

- [x] Add `Agent.PrimaryRepo` (`primary_repo`) and `Agent.WorktreePath` (`worktree_path`) to `internal/roster`
- [x] Validate both fields in `Load` (optional; fail-closed when set and invalid)
- [x] Unit tests: accept absent; accept valid `owner/name` + absolute worktree; reject paths/URLs/malformed/relative
- [x] Add `Agent.SecondaryRepos` (`secondary_repos[]`) with owner/name validation + dedupe

## Fixture + docs-as-code

- [x] Document fields in `flotilla.example.json` with generic `acme/*` repos and `/srv/fleet/desks/*` worktrees
- [x] Openspec change (`proposal.md`, `design.md`, `tasks.md`, `specs/roster/spec.md`)

## Domain materialization (addendum — hook contract consumer)

- [x] `workspace.ParseGitRemoteOwnerName` (https + ssh + git@)
- [x] `workspace.MaterializeGatekeeperDomain` → `worktree/.gatekeeper/domain` (0644, idempotent)
- [x] Line 1 = `primary_repo` else origin; extra lines = `secondary_repos[]`
- [x] Wire into `workspace init` and `resume` (when worktree cwd exists)
- [x] Unit + integration tests; do **not** implement merge-domain hook

## Explicitly deferred (later slices / tracks)

- [ ] `flotilla status` (or small CLI) prints seat → coordinator? → parent → repo/worktree
- [ ] `workspace init --repo` writes `primary_repo` / `worktree_path` into the live roster JSON
- [ ] #426 live-checkout guard
- [ ] #551 gatekeeper hook implementation (owned by gatekeeper-xo; already shipped per contract)
- [ ] Track B `stackable_wakes` canary
