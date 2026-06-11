# Design: per-agent workspace `~/.flotilla/<agent>/`

**Status:** design (revised after systems-review — awaiting XO checkpoint) · **Date:** 2026-06-11 · **Subsumes:** #6 (pluggable tracker + heartbeat-prompt customization) · **Builds on:** `docs/agent-launch-recipes-design.md`.

## Context

flotilla makes a standard Claude Code / Grok / Cursor agent autonomous, but a
desk's per-agent state is scattered across three unrelated places and one is
missing entirely:

| concern | today | problem |
|---|---|---|
| launch recipe | flat `flotilla-launch.json` (`agents` map, host-local) | one file for all desks; no per-agent home |
| heartbeat / continuation prompt | roster `heartbeat_message` (legacy) **or** the detector's hard-coded `continuationPrompt` (`cmd/flotilla/watch.go:156`) | the production XO runs the **detector**, whose prompt is not customizable at all |
| working tracker | `watch --tracker-file` (single, global) | the detector hashes ONE tracker (the XO's) |
| desk identity/role | — | no home for "who is this desk + its standing task" |

Operator decision (2026-06-10, confirmed 2026-06-11): unify them into a per-agent
**workspace** `~/.flotilla/<agent>/` (mirroring `~/.openclaw/`, `~/.hermes/` —
illustrative precedent, not an interop contract); the per-workspace launch config
**replaces** the flat `flotilla-launch.json` (which stays a read-only migration
fallback); the identity file uses the agent's **native** convention
(`CLAUDE.md`/`AGENTS.md`), not a flotilla-only `IDENTITY.md`.

> **Production mode is the change-detector, not the legacy heartbeat.** When
> `change_detector: true` (the XO's live config), `cfg.HeartbeatMessage` /
> `DefaultHeartbeatPrompt` are **never consulted** — the XO is woken by the
> detector's `continuationPrompt` / ping / material bodies (watch.go:156-177).
> The legacy `heartbeat_message` path (watch.go:257-261) runs only when the
> detector is off. The design below targets the **detector** prompt, or
> `HEARTBEAT.md` would be silently inert for the production XO (systems-review P1-1).

## Schema — `~/.flotilla/<agent>/`

```
~/.flotilla/<agent>/
  launch.json    # the launch recipe (single object — the agent IS the dir name)
  HEARTBEAT.md   # this agent's continuation-prompt TEMPLATE (customizable)
  state.md       # this agent's working tracker (the detector hashes it)
  CLAUDE.md      # (surface=claude-code) the desk's native identity/role file
  AGENTS.md      # (surface=grok / cursor) ditto — surface-named, read natively
```

Host-local (under the resolved home dir), never committed, trusted at the secrets
level (the `launch` command is shell-run).

### Home-dir resolution (systems-review P2-4)

The workspace root is `<home>/.flotilla/`, where `<home>` = `os.UserHomeDir()`
(respects `$HOME`, then the passwd db). A `--workspace-root <dir>` flag /
`$FLOTILLA_WORKSPACE_ROOT` overrides it (mirroring `--tracker-file`), for tests and
non-standard layouts. **Load-bearing assumption, stated as a requirement:** the
`flotilla-watch` daemon and the operator's interactive `flotilla resume` resolve the
**same** home. This holds today because `flotilla-watch` is a `systemctl --user`
service (per `deploy/flotilla-watch.service.in` — runs as the operator's user, same
`$HOME`). A system-level unit running as a different user would read a different
`~/.flotilla/` and the operator's scaffolding would be invisible to the daemon — so
the runbook MUST keep it a user service (or set `--workspace-root` explicitly).

### `launch.json` — the recipe (same fields, relocated + de-mapped)

```json
{ "launch": "claude --add-dir ~/.flotilla/hydra-ops -w hydra-ops", "cwd": "/abs/worktree", "tmux": "flotilla:hydra-ops" }
```

The same `Recipe` the flat file used (`internal/launch/launch.go`), SAME validation
(launch/cwd/tmux rules), but: no `agents` map (one recipe per file; the agent is the
dir name); no `state` field (the workspace `state.md` is the pointer). The flat
file's `state` stays valid in the fallback path only.

### `HEARTBEAT.md` — the continuation-prompt template (fixes P1-1 + P1-2 together)

`HEARTBEAT.md` is an OPTIONAL template for the detector's **continuation** prompt
(the self-continuation tick — the natural customization target; ping/material
bodies stay built-in). It supports two placeholders that flotilla substitutes from
**resolved** values, and the ack instruction is always appended:

- `{{tracker}}` → the resolved tracker path (see below)
- `{{settle}}` → the settle-marker path

Resolution: `<workspace>/HEARTBEAT.md` (templated) → the built-in `continuationPrompt`.
In **legacy** mode only, `HEARTBEAT.md` (verbatim, same placeholders) → roster
`heartbeat_message` → `DefaultHeartbeatPrompt`.

**The P1-2 invariant — one resolved tracker path, two consumers.** Today
`*trackerPath` is BOTH interpolated into the continuation prompt (watch.go:158) AND
the hash target (watch.go:201) — one variable, so they cannot diverge. The workspace
MUST preserve that: `ResolveTracker(agent)` returns ONE path that is (a) fed to
`trackerHasher` and (b) substituted for `{{tracker}}` in the prompt. They can never
point at different files — otherwise the XO updates a file the detector does not
hash and the self-continuation materiality signal **silently dies**. This is the
single most dangerous failure mode and is encoded as a spec scenario ("the path the
prompt names equals the path the detector hashes").

### `state.md` — the per-agent tracker

The file the change-detector content-hashes. Resolution (for the detected agent):
`<workspace>/state.md` → `--tracker-file`/`$FLOTILLA_TRACKER_FILE` →
`<roster-dir>/.flotilla-state.md`. The resolved path threads into both the hasher
and `{{tracker}}` (above).

### Native instruction file — `CLAUDE.md` / `AGENTS.md`

The desk's identity/role in the agent's own convention (claude-code → `CLAUDE.md`;
grok/cursor → `AGENTS.md`). flotilla NEVER auto-injects or clobbers it.

## How the identity file reaches the agent — THE OPEN FORK (decide at checkpoint)

Claude Code discovers `CLAUDE.md` from the cwd, its ancestors, and `~/.claude/` —
not from an arbitrary path. A desk's `cwd` is usually its worktree, so
`~/.flotilla/<agent>/CLAUDE.md` is **not** natively on the read path. Three options:

- **Option C — `launch` command adds the workspace dir (RECOMMENDED).** The recipe's
  `launch` is already operator-authored shell (`launch.go:29`), so it can read the
  workspace identity with **zero flotilla glue and zero cwd touch**:
  `claude --add-dir ~/.flotilla/<agent> -w <agent>` (Claude Code's `--add-dir` adds
  the directory to the session context). This dominates A and B — no cwd repurposing,
  no symlink side-effect — and is just a recipe convention `workspace init` can emit.
- **Option A — flotilla never touches the cwd; operator wires it.** (A1) set the
  recipe `cwd` to the workspace dir for a repo-less desk; (A2) for a repo desk, the
  **repo's own** `CLAUDE.md` is the identity and the workspace file is *vestigial*
  (documentation-only, never read) — which undercuts "one home for identity" for the
  common repo desk.
- **Option B — `workspace init` symlinks the identity into the cwd** iff absent.
  Automatic, but flotilla now writes into the agent's worktree (a new side effect; a
  dangling symlink if the workspace moves).

**Recommendation: Option C** — zero glue, zero cwd touch, and `workspace init` emits
the `--add-dir` recipe convention. The XO ratifies C vs A vs B at the checkpoint.

## `flotilla resume` — read the workspace, fall back to the flat file

Recipe resolution (the safety-critical `runResume` core is unchanged — its two P1
invariants key on `key`/`cwd`/`launch`/tmux, never the recipe SOURCE, verified
resume.go:112-187):

1. `~/.flotilla/<agent>/launch.json` exists → use it.
2. Else the flat `flotilla-launch.json` has an entry → use it (migration fallback).
3. Else the existing clear error, now naming both locations.

The printed `/takeover` pointer = `<workspace>/state.md` if it exists **and is
non-empty** (mirrors the existing `r.State != ""` guard, resume.go:194 — an empty
scaffolded `state.md` suppresses the pointer, P2-5), else the flat recipe's `state`.

### Cross-recipe tmux-collision check (systems-review P2-1, P2-2)

Today `launch.Load` validates the WHOLE flat file every resume, so a shared `tmux`
target is caught for free (`launch.go:110-113`). With one `launch.json` per dir,
`resume <agent>` naturally loads only that agent's recipe — there is no fleet view.
The check is **preserved by a bounded fleet scan**, with a deliberately *weaker
failure posture than the flat file's fail-closed*:

- On resume, glob `~/.flotilla/*/launch.json` (∪ flat-file recipes for agents
  without a workspace, so the invariant spans both sources during migration) and
  reject only if **this** agent's resolved `tmux` target collides with another's.
- A malformed/unreadable *other* workspace is **skipped with a warning**, NOT
  fail-closed — a broken unrelated workspace must never block recovering a healthy
  desk (that would REGRESS recoverability vs. today, where one file's validity is
  all-or-nothing only because it is one file). This is a deliberate design choice,
  not an oversight.

## `flotilla watch` — per-agent prompt + tracker (MODIFIED `watch`)

- **Continuation prompt**: when enqueueing the detector continuation wake, source it
  from the resolved `HEARTBEAT.md` template (else built-in), with `{{tracker}}`/
  `{{settle}}` substituted and ack appended. Resolved ONCE at watch startup (prompts
  are built at startup today — watch.go:156, frozen into the `wake` closure), so a
  `HEARTBEAT.md` edit takes effect on the next `flotilla-watch` restart, not live.
- **Detector tracker**: `trackerHasher` path = `ResolveTracker(xo_agent)` — the SAME
  resolved path substituted into `{{tracker}}` (the P1-2 single-source invariant).

No workspace ⇒ every default at watch.go:66-80 / 257-261 is untouched ⇒ behavior is
**bit-for-bit today** (verified). See migration for the one non-additive transition.

## Migration & backward compatibility (systems-review P1-3)

**The no-workspace path is purely additive — today's behavior bit-for-bit.** The
*migration transition* is NOT invisible and the proposal must not claim it is:

- Switching the tracker source (operator `mv .flotilla-state.md ~/.flotilla/hydra-ops/state.md`)
  is a **hash-discontinuity** event. The detector's snapshot is keyed to the old
  path's content (`*snapshotPath`, watch.go:218); the first tick after the source
  switches to `state.md` hashes a different file than the persisted snapshot → **one
  spurious material wake**. This requires a `flotilla-watch` **restart** to switch the
  source cleanly, and the one-time post-migration wake is **expected and harmless**
  (the XO replies idle and re-settles). Documented in the runbook; not claimed away.

Migration per agent: `flotilla workspace init <agent>` → fill `launch.json` → move
the XO's `.flotilla-state.md` → `state.md` → restart `flotilla-watch`. The flat
`flotilla-launch.json` stays a fallback until all desks are migrated, then removable.

## New command — `flotilla workspace`

- `flotilla workspace init <agent>` — roster-validate the agent (this is where the
  flat file's per-key roster check relocates, P3-3), then scaffold: a commented
  `launch.json` template (emitting the `--add-dir` recipe convention), empty
  `HEARTBEAT.md`/`state.md`, and a surface-named identity stub. **Idempotent**: never
  overwrites; creates only what's missing. Does NOT populate real host paths (operator
  data, per the launch-design "data, not code" principle).
- `flotilla workspace path <agent>` — print the resolved dir (for scripts/the skill).

## Non-goals

- Auto-injecting identity / `/takeover` (unchanged from resume's non-goals).
- A committable/shared workspace — host-specific by design.
- Multi-agent heartbeating — v1 heartbeats only the `xo_agent`; per-agent
  `HEARTBEAT.md` makes that one prompt file-editable and future-proofs the rest.
- Customizing the ping/material detector bodies — only the continuation prompt is
  templated in v1 (the others are liveness/notification, not self-continuation).
- Removing the flat-file reader now — it stays as the migration fallback.

## Open questions for the checkpoint

1. **Identity-file fork: Option C (`--add-dir`, recommended) vs A vs B?**
2. **`HEARTBEAT.md` templating scope:** template the continuation prompt with
   `{{tracker}}`/`{{settle}}` (recommended — delivers customization for the *production*
   detector path), or defer prompt-customization and ship only launch.json + state.md +
   identity in v1 (simpler, but no prompt customization, contradicting the operator ask)?
3. **PR split:** recommend PR-1 = workspace pkg + `workspace` cmd + `resume` consumption
   (no live-daemon risk); PR-2 = `watch` consumption (the safety-critical detector — the
   P1-2 single-source threading + P1-3 restart semantics land isolated and well-tested).
