# Design: agent state & launch recipes — deterministic fleet recovery

**Status:** design (awaiting approval) · **Date:** 2026-06-09 · **Closes the gap behind:** crash-recovery of a fleet desk.

## Context

flotilla coordinates a fleet of AI coding agents, each a session in a tmux
pane. It can *deliver to* and *detect the death of* a desk — but it does **not
record where a desk's state lives or how to relaunch it**. The roster carries
only names (plus optional `tmux_title`, `surface`). So when a desk's process
dies — or the whole tmux server dies — recovery requires either guessing the
launch command/working directory or out-of-band operator knowledge. Concretely
this gap blocks three things at once:

1. **Manual desk rebuild** — a domain desk's state lives in a worktree whose
   name need not match the agent name (e.g. `crypto-trend-dev`'s work is in the
   `spark-crypto` worktree), so there is no derivable launch command.
2. **Auto-relaunch of the XO** — the watchdog reliably detects the XO is down
   and alerts "restart needed", but has no declared command to actually restart
   it (the recovery half of the operator's chosen hybrid posture).
3. **The recovery skill** — `flotilla-fleet-recovery` has to *guess*
   `claude -w <name>`.

Origin: 2026-06-09, the `hydra-ops` XO + the entire tmux server died overnight
(~6.7h); `flotilla-watch` survived and (we confirmed by repro) alerts on the
down-transition — to Discord — but nothing relaunches. Operator: *"the lack of
a clear direction of where the agent state lives is a feature gap, clearly."*

## Decision (operator, 2026-06-09): host-local launch recipes (Option A)

The committable roster stays **portable** (names, surface, watch config — no
host paths). Host-specific launch recipes live in a **separate, gitignored,
host-local file**, a sibling of `flotilla-secrets.env`, referenced via
`--launch <path>` / `$FLOTILLA_LAUNCH` (default `<roster-dir>/flotilla-launch.json`).

Rationale: worktree absolute paths are host-specific; the public roster must
not carry one host's filesystem layout; a second host declares its own recipes.
This mirrors the existing secrets-file pattern exactly (committable roster +
host-local secrets). Rejected **Option B** (recipes inline in the roster) for
the same reason secrets aren't in the roster.

## Schema — `flotilla-launch.json`

```json
{
  "agents": {
    "hydra-ops": {
      "launch": "claude -w hydra-ops",
      "cwd": "/home/jim/workspace/github.com/General-ML/spark",
      "tmux": "flotilla:hydra-ops",
      "state": ".claude/handoffs/<latest>.md"
    },
    "crypto-trend-dev": {
      "launch": "claude --continue",
      "cwd": "/home/jim/workspace/github.com/General-ML/spark-crypto"
    }
  }
}
```

- **`launch`** (required) — the shell command that (re)starts the desk.
- **`cwd`** (required) — the working directory / worktree to launch in.
- **`tmux`** (optional) — target `session:window` to (re)create the pane in;
  default `flotilla:<name>` (a canonical `flotilla` session, one window per
  agent).
- **`state`** (optional) — a pointer to the desk's handoff/context doc, surfaced
  for the operator/skill to drive `/takeover` (the CLI does **not** auto-inject
  it — see Non-goals).
- An agent present in the roster but absent from the launch file is "declared
  but not relaunchable" — `relaunch` errors clearly rather than guessing.

**Validation at load** — the full table, held to `roster.Load`'s discipline
(systems-review hardening 2026-06-09):

| field | rule |
|---|---|
| `launch` | required; non-empty; reject `\t` `\n` `\r` |
| `cwd` | required; non-empty; **absolute** (host-independent typo guard; existence is NOT checked at load — the file may be loaded on another host — it surfaces as a clear `relaunch`-time error); reject `\t` `\n` `\r` |
| `tmux` | optional; if present parses as a plain `session:window` — non-empty halves, no second `:`, no `.pane` suffix in the window half (relaunch derives the pane), no spaces (would break the cold-create argv); reject `\t` `\n` `\r` |
| `state` | optional; reject `\t` `\n` `\r` (it is printed for the operator/skill to parse) |
| every key in `agents` | must name an agent in the roster (unknown → load error; catches typos) |
| no two recipes | may share a `tmux` target (would relaunch into the same window — mirrors roster's shared-title rejection) |

The reject-`\t`/`\r` rule (not just `\n`) matches `roster.go`: these values flow
onto the TAB/NEWLINE-delimited `list-panes` wire format and into the marker.

The launch-file **path** itself is NOT traversal-checked (a `..`-rejection was
considered and declined): the path comes from `--launch`/`$FLOTILLA_LAUNCH`/the
roster-relative default — all operator-controlled at the same trust level as the
secrets-file path — and `roster.Load`/`LoadSecrets` impose no such check either;
rejecting `..` would also break a legitimate relative `--launch ../shared.json`.

**Load is fail-closed:** a single malformed recipe blocks loading the whole
file, so `relaunch` for *every* desk fails until it's fixed. That is the correct
safety posture (never relaunch on a half-parsed file), but the recovery skill
MUST document it — one bad recipe entry blocks recovering the entire fleet.

**`.gitignore`:** `flotilla-launch.json` matches **no** existing ignore pattern
(`*.env`, the explicit `/flotilla.json`), so PR-1 MUST add an anchored
`/flotilla-launch.json` line — otherwise a `git add .` commits the host's
worktree layout, the exact leak Option A exists to prevent. For a non-default
`--launch` path the operator owns the leak risk (documented).

## New command: `flotilla relaunch <agent>`

The single building block both manual recovery and auto-XO consume — so its
safety properties are **enforced in the command**, not delegated to callers
(systems-review P1s 2026-06-09: a property stated only in prose is inherited by
no caller). The marker — not the window — is the source of truth for "does this
desk's pane already exist", consistent with `ResolvePane`'s two-tier precedence.

Algorithm:

1. Load roster (agent must exist) + launch recipes; resolve this agent's recipe
   (error `no launch recipe for "<agent>" in <file>` if absent). `launch` is
   the trailing **command argument** to tmux (run by the pane's `sh -c`), NOT
   `send-keys` — so a recipe like `claude -w hydra-ops` (or a compound
   `cd x && claude --continue`) is the pane's foreground process, and when it
   exits the pane dies (a dead recipe surfaces as a dead pane the watchdog
   catches). Recipes are therefore shell-interpreted; the launch file is
   host-local and trusted at the secrets level (anyone who can write it can
   already write `flotilla-secrets.env`).

2. **Resolve by marker first** — `deliver.Resolve(agent.Title())` (3-way:
   Unique / None / Ambiguous; a missing tmux server → None, the cold path):
   - **Unique** (the desk's pane already exists): **assess its liveness** via the
     same `surface.Assess` the watchdog uses, then a **fail-safe interlock**:
     - respawn **ONLY** when the pane is a definitively-dead `StateShell` (or
       `--force`). **REFUSE on every other state** — `StateWorking`, `StateIdle`,
       `StateAwaitingInput`, `StateAwaitingApproval`, `StateErrored` (all LIVE),
       AND `StateUnknown` (capture failed → can't confirm dead):
       `"<agent>" at <target> is <state> (not a dead shell); refusing to
       relaunch — close it first, or pass --force`. Refuse-by-default (not
       allow-by-default) makes "restart ≠ resume-and-act" a code invariant that
       every present/future caller and surface inherits; the claude surface fails
       OPEN to a live state on capture error, so "can't tell" lands on the safe
       side. (systems-review P2-4 2026-06-09: the earlier Working/Idle-only allow
       list would SIGKILL a future driver's live `Awaiting*` desk.)
     - on `StateShell`/`--force` → `tmux respawn-pane -k -t <target> -c <cwd>
       <launch>`. The pane id is reused, so its per-pane `@flotilla_agent` marker
       **survives the respawn**; read the marker back:
       - reads back `== agent.Title()` → confirmed; do NOT re-tag.
       - reads back `""` → the pane resolved by **title** (an untagged desk — the
         migration case): **ADOPT it by tagging** after respawn, rather than
         failing on the empty marker (systems-review P2-2).
       - reads back a different value → error (mis-tag; tell the operator to
         re-register).
   - **Ambiguous** (>1 pane matches) → **REFUSE**, surfacing the ambiguity error
     (the fleet is mis-tagged; the operator un-tags one). Never create a third
     pane on top of an ambiguous state.
   - **None** (genuine cold recovery): create the desk's pane:
     - `deliver.HasSession(session)` returns `(bool, error)` — a transient tmux
       failure is an error (not silently read as "no session", which would
       wrongly cold-create); exit-1 / no-server → `(false, nil)`.
     - session absent (covers **total tmux-server death** — the first tmux call
       cold-starts a server): `tmux new-session -d -s <session> -n <name>
       -c <cwd> <launch>` (session + the agent's window atomically; no stray
       window:0).
     - session present: `tmux new-window -t <session> -n <name> -c <cwd>
       <launch>` (`-P -F` to capture the new pane target).
     - then `deliver.TagPane(newPane, agent.Title())` (with its read-back) — the
       only branch that creates the marker.
     - **Concurrency**: resolve-by-marker-first is the primary guard — a racing
       second `relaunch` finds the first's tagged pane and respawns in place. A
       residual cold-create race remains (two cold invocations both passing None
       before either tags → a second window), which is recoverable,
       operator-visible state (NOT a duplicate marker — only one pane gets
       tagged). PR-1 does not add a per-agent lockfile; that is noted future
       hardening, not a v1 requirement.

3. Print the resolved target + the `state` pointer (if any) so the caller drives
   `/takeover`. `relaunch` (re)starts the process and ensures it's tagged; it
   does **not** restore context (see Non-goals). All tmux calls reuse
   `deliver`'s 10s `commandTimeout`.

This makes both P1 safety properties — *never kill a live desk* and *never
create a duplicate marker* — invariants of the building block, so every caller
(the recovery skill, the operator, and the future auto-XO) inherits them.

## Auto-XO composition (separate PR — PR-2)

`flotilla watch --relaunch-xo` (opt-in): on the watchdog **down-transition** for
the XO, invoke the XO's relaunch recipe — guarded by a **dedicated, time-windowed
rate limiter**, NOT the watchdog's debounce. (systems-review 2026-06-09: the
watchdog's `down` flag debounces *per transition* and clears only on a real ack
— it gives **zero** protection against a *flapping* recipe that acks once, then
re-dies seconds later, producing a fresh down-transition → relaunch → ack →
death spin.) So PR-2 carries its own sliding-window guard with true
`StartLimitIntervalSec`/`StartLimitBurst` semantics: at most **N relaunches per
rolling window T**, counting only deaths-within-T (so steady-state healthy
operation never trips it, but a flap latches off after the burst). On exceeding:
**stop auto-relaunching, alert `XO relaunch storm — auto-relaunch disabled,
manual intervention required`, and require an operator reset.** The heartbeat
then re-orients a successfully-relaunched XO from its `state` on the next tick.

Only the XO is auto-relaunched, and that asymmetry is **load-bearing, not
arbitrary**: the XO is what notices dead *domain* desks, so auto-relaunching the
XO bootstraps the entire manual-recovery path (if the XO is down, nothing drives
desk recovery either). A future reader must not "simplify" by making domain desks
auto-relaunch too — that would multiply the storm-guard surface across N desks
for no bootstrapping benefit. Domain desks stay manual: the recovery skill calls
`flotilla relaunch <name>` per dead desk.

## PR split

- **PR-1** (this design): launch-recipe file (load + validate) + `flotilla
  relaunch <agent>` + docs + update the `flotilla-fleet-recovery` skill to call
  `relaunch`. Unblocks deterministic *manual* rebuild of any desk.
- **PR-2**: `watch --relaunch-xo` auto-relaunch on the down-transition +
  storm-guard. Composes on PR-1.

## Non-goals

- Auto-relaunching **domain** desks (hybrid: XO auto, desks manual).
- Auto-injecting `/takeover` — context restoration stays an explicit step the
  operator/skill drives (a desk could resume mid-destructive-op; restart ≠
  resume-and-act).
- A committable/shared launch file — host-specific by design.

## Backward compatibility

Purely additive. No launch file → `relaunch` errors clearly; `watch` without
`--relaunch-xo` behaves exactly as today (alert-only). The roster schema is
unchanged.

## Populating the recipes (data, not code)

The feature ships the mechanism. The actual recipes are host data the operator
owns; `hydra-ops` is known empirically (`claude -w hydra-ops` in the spark repo
root). Domain-desk launch commands/worktrees are operator-provided (or derived
from each desk's handoff) and written into `flotilla-launch.json` on Spark.
