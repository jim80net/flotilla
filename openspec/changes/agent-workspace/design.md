# Design: per-agent workspace `~/.flotilla/<agent>/`

**Status:** design (awaiting XO checkpoint) · **Date:** 2026-06-11 · **Subsumes:** #6 (pluggable tracker + heartbeat-prompt customization) · **Builds on:** `docs/agent-launch-recipes-design.md` (the flat launch recipe this evolves).

## Context

flotilla makes a standard Claude Code / Grok / Cursor agent autonomous, but a
desk's per-agent state is scattered across three unrelated places and one is
missing entirely:

| concern | today | problem |
|---|---|---|
| launch recipe | flat `flotilla-launch.json` (`agents` map, host-local) | one file for all desks; no per-agent home |
| heartbeat prompt | roster `heartbeat_message` (single, global) | every heartbeated agent shares one prompt |
| working tracker | `watch --tracker-file` (single, global) | the detector hashes ONE tracker (the XO's) |
| desk identity/role | — | no home for "who is this desk + its standing task" |

The operator's decision (2026-06-10, confirmed 2026-06-11): unify them into a
per-agent **workspace** under a host-local home `~/.flotilla/<agent>/` — mirroring
`~/.openclaw/workspace-<name>/` and `~/.hermes/` — and the per-workspace launch
config **replaces** the flat `flotilla-launch.json` (the flat file stays a
read-only migration fallback). The identity file uses the agent's **native**
convention (`CLAUDE.md` / `AGENTS.md`), not a flotilla-only `IDENTITY.md`, so it
is read with zero glue.

## Schema — `~/.flotilla/<agent>/`

```
~/.flotilla/<agent>/
  launch.json    # the launch recipe (single object — the agent IS the dir name)
  HEARTBEAT.md   # this agent's heartbeat / continuation prompt (customizable)
  state.md       # this agent's working tracker (the detector hashes it)
  CLAUDE.md      # (surface=claude-code) the desk's native identity/role file
  AGENTS.md      # (surface=grok / cursor) ditto — surface-named, read natively
```

The directory is **host-local and gitignored-by-location** (it lives under `$HOME`,
not the repo) and trusted at the secrets level (the launch command is shell-run —
anyone who can write the workspace can already write `flotilla-secrets.env`),
exactly the posture the flat launch file established.

### `launch.json` — the recipe (unchanged fields, relocated + de-mapped)

```json
{ "launch": "claude -w hydra-ops", "cwd": "/abs/path/to/worktree", "tmux": "flotilla:hydra-ops" }
```

The same `Recipe` the flat file used (`internal/launch/launch.go`), with the SAME
validation table (launch: required, no `\t\n\r`; cwd: required, absolute, no
`\t\n\r`; tmux: optional, plain `session:window`, no `.pane` suffix / second `:` /
spaces), but:

- **No `agents` map** — one recipe per file; the agent is the directory name.
- **No `state` field** — the workspace's own `state.md` IS the state pointer
  (`resume` defaults the printed `/takeover` pointer to `<workspace>/state.md`).
  The flat file's `state` stays valid in the fallback path only.
- The cross-recipe "no two share a tmux target" check moves to a **fleet-load**
  step (load all workspaces, reject a shared tmux target) — preserved, not lost.

### `HEARTBEAT.md` — the per-agent prompt

The text `flotilla watch` injects on a tick for THIS agent. Replaces the single
roster `heartbeat_message`. Resolution order (per agent): `<workspace>/HEARTBEAT.md`
→ roster `heartbeat_message` → `watch.DefaultHeartbeatPrompt`. The change-detector's
continuation prompt (today hard-coded in `cmd/flotilla/watch.go`) resolves the same
way, so the XO's self-continuation wording becomes editable in one file.

### `state.md` — the per-agent tracker

The tracker the change-detector content-hashes to decide "did the goal/task state
change". Replaces `watch --tracker-file`. Resolution (for the detected agent):
`<workspace>/state.md` → `--tracker-file` / `$FLOTILLA_TRACKER_FILE` →
`<roster-dir>/.flotilla-state.md`. The XO's `.flotilla-state.md` relocates to
`~/.flotilla/hydra-ops/state.md`.

### Native instruction file — `CLAUDE.md` / `AGENTS.md`

The desk's identity + standing role, in the **agent's own convention** so it is
read natively (zero glue). The file NAME is chosen by the agent's `surface`
(claude-code → `CLAUDE.md`; grok / cursor → `AGENTS.md`). **flotilla never
auto-injects or clobbers it** — mirroring resume's non-goal (no auto-`/takeover`;
restart ≠ resume-and-act). See the open fork below for how it reaches the agent.

## `flotilla resume` — read the workspace, fall back to the flat file

Today `resume` loads the flat `flotilla-launch.json` and `Config.Recipe(agent)`.
New resolution (no change to the safety-critical `runResume` core — the two P1
invariants are surface-/source-agnostic):

1. If `~/.flotilla/<agent>/launch.json` exists → load + validate it as the recipe.
2. Else if the flat launch file has an entry for `<agent>` → use it (migration
   fallback), exactly as today.
3. Else → the existing clear error (`no launch recipe for "<agent>"…`), now naming
   both the workspace path and the flat file it looked in.

The printed `/takeover` state pointer = `<workspace>/state.md` if present, else the
flat recipe's `state`. **Everything else in `resume` is unchanged** — resolve-by-
marker-first, the fail-safe liveness interlock, cold-create, tagging read-back.

## `flotilla watch` — per-agent prompt + tracker (MODIFIED `watch`)

- **Heartbeat prompt**: resolve per the order above when enqueueing a tick for an
  agent. v1 only the `xo_agent` is heartbeated, so this reads
  `~/.flotilla/<xo_agent>/HEARTBEAT.md`; absent → today's roster/default behavior.
- **Detector tracker**: the `trackerHasher` path resolves to
  `~/.flotilla/<xo_agent>/state.md` when present, else `--tracker-file`/default.

Both are **fallback-defaulted**, so a deployment with no workspace behaves exactly
as today (the `watch` MODIFIED requirements keep the existing scenarios and ADD the
workspace-source-precedence scenario).

## New command — `flotilla workspace`

- `flotilla workspace init <agent>` — scaffold `~/.flotilla/<agent>/`: write a
  `launch.json` template (commented with the validation rules), an empty
  `HEARTBEAT.md` and `state.md`, and a stub identity file named for the agent's
  `surface` (`CLAUDE.md` / `AGENTS.md`). **Idempotent**: never overwrites an
  existing file (prints "exists, kept"); creates only what's missing. Validates the
  agent is in the roster first.
- `flotilla workspace path <agent>` — print the resolved workspace dir (for scripts
  / the recovery skill).

`init` does NOT populate the recipe with real paths (host data the operator owns,
per the launch-design "data, not code" principle) — it scaffolds the shape.

## Native instruction file — THE OPEN FORK (decide at checkpoint)

Claude Code discovers `CLAUDE.md` from the cwd, its ancestors, and `~/.claude/` —
**not** from an arbitrary path. A desk's `cwd` is typically its worktree, so
`~/.flotilla/<agent>/CLAUDE.md` is **not** on the agent's native read path. Two
ways to honor "read natively, zero glue":

- **Option A — `flotilla` never touches the cwd (recommended).** The workspace
  holds the *canonical* identity file; how it reaches the agent is operator wiring,
  with two supported patterns documented: (A1) set the recipe `cwd` to the
  workspace dir for an identity-first / repo-less desk (then `CLAUDE.md` is native);
  (A2) for a repo/worktree desk, the **repo's own** `CLAUDE.md`/`AGENTS.md` is its
  identity (the agent already reads it — true zero glue) and the workspace file is
  optional. Mirrors resume's "no auto-edit of the agent's files" non-goal exactly.
- **Option B — `workspace init` symlinks the identity file into the cwd** iff the
  cwd lacks one (never clobbers a repo's existing file). More automatic, but
  flotilla now writes into the agent's working tree — a new side effect to reason
  about (and a dangling symlink if the workspace moves).

**Recommendation: Option A** — it keeps flotilla's "never silently edit the
agent's files" invariant, and the repo-native case (A2) is the genuine zero-glue
story. The XO ratifies A vs B at the checkpoint before any build.

## Migration & backward compatibility

Purely additive. No workspace → flat launch file + roster prompt + `--tracker-file`,
i.e. today's behavior bit-for-bit. The operator migrates per-agent with
`flotilla workspace init`, fills `launch.json`, moves the XO's `.flotilla-state.md`
to `state.md`. The flat `flotilla-launch.json` is a read-only fallback retained
until every desk has a workspace, then removable. No roster schema change.

## Non-goals

- Auto-injecting identity / `/takeover` (unchanged from resume's non-goals).
- A committable/shared workspace — host-specific by design (it holds host paths).
- Multi-agent heartbeating — v1 still heartbeats only the `xo_agent`; per-agent
  `HEARTBEAT.md` simply makes that one prompt file-editable and future-proofs the
  rest.
- Removing the flat `flotilla-launch.json` reader now — it stays as the migration
  fallback; its deletion is a future cleanup once all desks are migrated.

## Open questions for the checkpoint

1. **Native-instruction-file fork: Option A (no cwd touch, recommended) vs B
   (symlink-on-init)?**
2. **`state` field drop:** confirm `resume`'s `/takeover` pointer should default to
   `<workspace>/state.md` (vs keeping an explicit recipe `state`).
3. **Scope/PR split:** one PR (workspace schema + `workspace` cmd + resume + watch),
   or split resume-consumption (PR-1) from watch-consumption (PR-2)? (Recommend
   split — watch is the live safety-critical daemon; isolate its change.)
