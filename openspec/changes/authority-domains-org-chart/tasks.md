# Tasks — authority-domains-org-chart (slice 1)

## Schema + load validation

- [x] Add `Agent.PrimaryRepo` (`primary_repo`) and `Agent.WorktreePath` (`worktree_path`) to `internal/roster`
- [x] Validate both fields in `Load` (optional; fail-closed when set and invalid)
- [x] Unit tests: accept absent; accept valid `owner/name` + absolute worktree; reject paths/URLs/malformed/relative

## Fixture + docs-as-code

- [x] Document fields in `flotilla.example.json` with generic `acme/*` repos and `/srv/fleet/desks/*` worktrees
- [x] Openspec change (`proposal.md`, `design.md`, `tasks.md`, `specs/roster/spec.md`)

## Explicitly deferred (later slices / tracks)

- [ ] `flotilla status` (or small CLI) prints seat → coordinator? → parent → repo/worktree
- [ ] `workspace init --repo` writes `primary_repo` / `worktree_path` into the live roster
- [ ] #426 live-checkout guard
- [ ] #551 gatekeeper domain enforcement
- [ ] Track B `stackable_wakes` canary
