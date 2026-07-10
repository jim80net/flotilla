# authority-domains-org-chart

## Why

Humans and automation must archaeology `launch.json` cwd (and host-local state) to
learn which seat owns which product repo. Principle 11 (desk homes are worktrees)
is doctrine, not a first-class roster field. Track C of the authority-domains
chapter (operator-greenlit 2026-07-10) makes the roster the org chart of **repos**:
each seat declares a primary authority domain as a portable git identity, with an
optional host-local worktree link.

This unblocks cleaner Track A verify (merge domain vs CWD lead marker) without
requiring gatekeeper hooks in this change.

## What Changes

- **`Agent.primary_repo`** (optional): canonical `owner/name` git identity for the
  seat's primary authority domain. Load validates shape; absent is valid (backward
  compatible).
- **`Agent.worktree_path`** (optional): absolute path linking the seat to its desk
  home. Existence not checked at load. Absent is valid.
- **`flotilla.example.json`**: documents both fields with generic roles/paths only
  (`acme/*`, `/srv/fleet/desks/…`) — private-boundary clean.
- **Openspec** delta under `roster` capability for load-time validation invariants.

## Non-Goals (this change / slice 1)

- `#426` live-checkout / deploy-clone write guard
- `flotilla status` / dash topology printing seat→repo
- `stackable_wakes` flag cutover (Track B)
- `#551` gatekeeper merge-domain **hook implementation** (Track A — consume `.gatekeeper/domain` only)
- Writing `primary_repo` back into the live roster JSON from `workspace init --repo`
- Requiring every agent to set `primary_repo` (remains optional until a later gate)

## Success criteria (slice 1 + domain materialization addendum)

1. Roster Load accepts rosters without the new fields (byte-compatible for absent keys).
2. Load rejects non-`owner/name` `primary_repo` / `secondary_repos` values (paths, URLs, malformed).
3. Load rejects non-absolute `worktree_path` when set.
4. Example roster shows the fields with generic fixture values only.
5. `workspace init` / `resume` materialize `worktree/.gatekeeper/domain` (primary_repo else origin; secondary lines; 0644; idempotent).
