# Design: roster as org chart of repos (Track C slice 1)

## Context

Charter (operator-greenlit chapter brief, host-local): authority domains = repos +
stackable wakes + roster-as-org-chart (2026-07-10). Track C — *Roster as org chart of
repos*.

Apex policy (chapter): singular CoS retained; N-level stacking under it. This slice
is **schema only** — declaration surface for seat → primary repo (+ optional worktree
link). Consumers (status print, workspace init write-back, gatekeeper domain check)
land in later slices / tracks.

## Decisions

### 1. Field names: `primary_repo` + `worktree_path`

| Field | Shape | Why |
|-------|--------|-----|
| `primary_repo` | `owner/name` string | Portable git identity; matches `gh`/`--repo` and Track A domain checks without host paths in the committable roster |
| `worktree_path` | absolute filesystem path | Principle 11 desk home link; host-local; optional because launch `cwd` already carries runtime worktree until write-back lands |

Rejected alternatives:

- **`authority_domain` only** — too abstract; `primary_repo` names the git object.
- **Absolute path as the only domain** — not portable across hosts; breaks private-boundary
  if real home paths enter example/fixtures; Track A wants repo identity, not CWD.
- **URL form** (`https://github.com/o/r`) — noisier; normalize to `owner/name` at the edge
  later if needed. Load rejects URLs so consumers never see two shapes.

### 2. Validation at `roster.Load` (fail-closed when present)

`primary_repo` when non-empty MUST:

- Be exactly two segments separated by one `/` (`owner` and `name` both non-empty)
- Contain no whitespace, backslash, or `://` / `git@` URL forms
- Not look like a filesystem path (`/…`, `./…`, `../…`)
- Reject `..` path traversal in either segment
- Allow GitHub-style characters: alphanumerics, `.`, `_`, `-` (case-preserving)

`worktree_path` when non-empty MUST:

- Be absolute (`filepath.IsAbs`)
- Contain no tab/newline (wire-safety parity with launch cwd / resolution keys)
- **Not** require the path to exist at load (roster may be validated on another host)

Absent or empty → no error (backward compatible).

### 3. Private / public partition

- Committable roster examples and tests use **generic** owners (`acme/…`) and
  **generic** desk paths (`/srv/fleet/desks/<role>`).
- Deployment rosters (gitignored) may set real `owner/name` and host paths; those
  stay host-local.
- Schema comments in code describe the *capability*, never a live fleet's names.

### 4. Relationship to later work

| Follow-on | Depends on this schema |
|-----------|------------------------|
| Track A (#551) merge-domain guard | Reads `primary_repo` (or derived domain) |
| Track C status/CLI print | Reads fields for seat→repo column |
| `workspace init --repo` write-back | Sets `primary_repo` (+ optional `worktree_path`) consistently |
| #426 deploy-clone guard | Separate mechanical path; may later use `worktree_path` vs deploy target list |

## Out of scope here

#426, status print, stackable_wakes, #551 hooks, secondary domains, requiring the field.
