# Design — ephemeral ceremony runner

**Status:** Design-only (operator-raised concern, 2026-07-06). Implementation follows operator gate.

## Prerequisites

**#369 confirmed-delivery for schedules must merge before P0 implementation.** Branch
`feat/schedule-confirmed-delivery-369` adds `KindSchedule`, deferred `last_fired` commit via
`CommitFired`, and durable `pending` + `ReplayPending`. On `main` today schedules still enqueue
`KindDetector` (busy-drop) and commit `last_fired` at enqueue — the ephemeral runner builds on
#369's delivery semantics for the **completion ping only**, not the ceremony body.

## The gap, stated in one line

Ceremony dispatches (walk, parade, visibility synthesis beats) inject **full ceremony prompts**
into a desk/XO/CoS **standing session** every time they fire. The standing pane accumulates
ceremony register across runs — context poisoning — while the product has no **disposable
one-shot invocation** path, only `deliver.ResolvePane` + tmux keystrokes into the persistent
harness.

## Grounded seams (cite, do not re-derive)

| Seam | Location | What it gives this design |
|------|----------|---------------------------|
| Standing-pane injection | `cmd/flotilla/watch.go` — scheduler (`KindDetector` on main; `KindSchedule` post-#369), `flotilla parade` CLI | Scheduled ceremonies + operator parade CLI inject full prompts into the **standing pane** |
| Desk heartbeat (out of scope) | `newDeskHeartbeatDispatch` | Continuation beats — **not** ceremony-class; unchanged by this design |
| Session hygiene (not disposable) | `internal/surface/surface.go:192` `RotateContext` | Wipes standing context **in place** after the fact; does not isolate one task |
| Host cwd / worktree | `internal/launch/launch.go` `Recipe.Cwd`; `internal/workspace/worktree.go` `ProvisionWorktree` | Ceremony runner inherits desk filesystem context without new access machinery |
| Headless precedent | `deploy/flotilla-doctor.sh` — `claude --print` recovery agent | Proves subprocess one-shot is already trusted for **side-channel** work |
| Tmux pane creation | `internal/deliver/resume.go` `NewWindow` / `NewSession` | Optional visibility path; not required for subprocess-first |
| Confirmed delivery (#369) | `internal/watch/inject.go` `KindSchedule` (post-#369) | Completion **ping** uses relay-defer policy; ceremony subprocess is off-injector |

## One-shot harness verification (first design obligation — probed 2026-07-06)

| Surface | One-shot mode | Verified? | Notes |
|---------|---------------|-----------|-------|
| **claude-code** | `claude -p/--print "<prompt>"` | **Yes** | `--print` documented; doctor uses `claude --print` |
| **grok** | `grok -p/--single "<prompt>"` or `--prompt-file <path>` | **Yes** | `--single` = "prints response to stdout and exits"; `--permission-mode` + `--always-approve` for unattended |
| **codex** | `codex exec "<prompt>"` | **Yes** | `codex exec` subcommand is explicitly non-interactive |
| **opencode** | `opencode run "<message>"` | **Yes** (CLI help) | `opencode run` exists; **live exit-code / cwd behavior not yet probed** — live smoke deferred to **P1** (see Phasing) |

**Falsified assumption to drop:** "every surface needs a tmux pane to run a ceremony." Three of four
already expose headless/single-turn CLIs. **P0 live-probe gate applies to claude/grok/codex only**;
opencode parity waits on P1 verification.

---

## Three approaches

### A. Subprocess ceremony runner (recommended)

`flotilla watch` (or `flotilla ceremony run`) spawns a **child process** in the owning desk's
`Recipe.Cwd` with a surface-specific one-shot command. No tmux pane for the ceremony body.

```
Scheduler due → ceremony.Run(spec)
  → build one-shot argv (claude -p / grok -p / codex exec / opencode run)
  → exec in desk cwd (inherit env, AGENTS.md/CLAUDE.md from worktree)
  → wait for exit + verify artifact path(s)
  → injector.Enqueue(short ping to standing pane, KindSchedule or KindDetector)
  → never enqueue ceremony transcript
```

**Pros:** True context isolation; natural completion signal (process exit); reuses doctor
precedent; minimal standing-pane pollution (one short line).

**Cons:** New package (`internal/ceremony`); per-surface argv builder; no live tmux visibility
during run (acceptable — ceremony output goes to log + artifact files).

### B. Ephemeral tmux window (throwaway pane, interactive harness)

Create `flotilla-ceremony:<run-id>` window via `deliver.NewWindow`, launch one-shot command
that exits on completion, kill window after.

**Pros:** Operator can `tmux attach` to watch; reuses launch recipe shape.

**Cons:** Still couples to tmux lifecycle; interactive harness may not exit cleanly; ceremony
transcript exists in tmux scrollback (recoverable by agent on mistake); more moving parts than A
with weaker isolation guarantee.

### C. Rotate-before/after on standing pane (rejected)

`RotateContext` before ceremony dispatch; inject prompt; rotate after.

**Cons:** Ceremony transcript still enters session history before rotate; rotate wipes **all**
standing context (destructive to in-flight work); does not solve register poisoning — only resets
after damage. **Fails the operator's stated concern.**

---

## Recommended approach: **A (subprocess-first)**, with optional B for debug

Ship **A** as the product default. Add `FLOTILLA_CEREMONY_TMUX=1` or `--ceremony-visible` later
for operators who want tmux visibility — not P0.

---

## Architecture

### New package: `internal/ceremony`

```go
// Spec is one bounded ceremony invocation.
type Spec struct {
    Owner       string            // roster agent (for cwd + completion ping target)
    Name        string            // ceremony id (walk-a, parade, …)
    Prompt      string            // inline or resolved file contents
    Artifacts   []ArtifactTarget  // durable outputs the runner must verify exist
    WriteLocks  []string          // absolute paths requiring flock serialization
    Timeout     time.Duration
}

type ArtifactTarget struct {
    Path        string // must exist after success
    Mode        string // "create" | "replace-anchor" (documented convention)
}

type Result struct {
    ExitCode int
    LogPath  string // host-local sidecar log (not injected into standing pane)
    Err      error
}

func Run(ctx context.Context, spec Spec, launch launch.Recipe, surface string) Result
func BuildOneShotArgv(surface, prompt string, cwd string) ([]string, error)
```

**Pure policy** in `internal/ceremony`; **watch** wires scheduler → `ceremony.Run` when the
resolved schedule entry has `mode: ephemeral` (opt-in per entry — shipped code default remains
`standing`; see Open questions #1).

### Host-local ceremony overlay (not committable roster)

`artifacts`, `write_locks`, `commit_on`, and `mode` live in a **host-local overlay** beside the
roster (gitignored, secrets-trust level) — same partition as `flotilla-launch.json`. The
committable `roster.Schedule` struct gains only `mode` (optional, default `standing`) in a
follow-on schema bump; artifact paths and `commit_on` stay host-local to avoid polluting the
public roster contract.

**Mode source-of-truth (load-bearing):** at load, merge overlay entry over roster schedule by
name. Precedence when both exist:

| Field | Winner | Notes |
|-------|--------|-------|
| `mode` | Overlay if set, else roster `mode`, else `standing` | Transition: overlay is dogfood lever until roster schema bump ships |
| `artifacts`, `write_locks`, `commit_on` | Overlay only | Never committable; required when mode resolves to `ephemeral` (see load-time rule below) |
| `at`, `to`, `prompt` | Roster | Portable schedule identity unchanged |

**Load-time validation (fail-closed):** when any schedule resolves to `mode: ephemeral` (from
overlay or roster), `roster.Load` MUST reject the config unless the overlay contains a **complete**
entry for that schedule name:

- non-empty `artifacts` (≥1 path; empty list vacuously passes verification and must not compile)
- valid `commit_on` if set (`artifact` | `ping`; default `artifact` for walk-shaped entries)
- `write_locks` paths absolute when present

A roster entry with `mode: ephemeral` and **no overlay row** (or a row missing required fields) is
**not deployable** — the daemon refuses to start rather than running a ceremony that produces nothing
and still `CommitFired`.

Once the committable roster gains `mode`, **overlay `mode` still wins when present** (operator
can force per-host behavior without a roster PR). Overlay does not gain committable fields beyond
what stays host-local; `artifacts` / `write_locks` / `commit_on` remain overlay-only permanently.

```json
{
  "schedules": {
    "walk-agent-a": {
      "mode": "ephemeral",
      "commit_on": "artifact",
      "artifacts": ["state/scorecards/walk-agent-a.yaml"],
      "write_locks": ["<roster-dir>/fleet-backlog.md"],
      "max_subprocess_retries": 3
    }
  }
}
```

- `write_locks`: **absolute paths** (validated at load). Relative paths rejected fail-closed.
- Generic docs use `agent-a` / `agent-b` only.

### One-shot argv table (not parsed from `Recipe.Launch`)

`Recipe.Launch` is an interactive shell compound (`cd x && claude --continue`) — **not**
reverse-engineerable. Ephemeral runner uses explicit per-surface templates resolved via
`workspace.ResolveActiveRecipe` + `agentSurface()` (overlay-first, same as watch delivery):

| Surface | argv template | Unattended flags |
|---------|---------------|------------------|
| claude-code | `claude -p <prompt>` | `--permission-mode` per desk policy; document gatekeeper parity |
| grok | `grok -p <prompt>` or `--prompt-file` | `--permission-mode` + `--always-approve` for unattended tool use |
| codex | `codex exec <prompt>` | config.toml overrides via `-c` as needed |
| opencode | `opencode run <message>` | **P1 live probe required** before claiming parity; fail-closed in P0 |
| aider, cursor-agent | — | **Fail-closed** at load: `mode: ephemeral` rejected for surfaces without one-shot |

Prompt delivery: prefer `--prompt-file` (host-local temp in roster dir) over argv length limits.

### Completion ping (standing session — minimal poisoning)

After `ceremony.Run` succeeds (exit 0 + artifact checks):

```
[flotilla ceremony] walk-agent-a complete — scorecard: state/scorecards/walk-agent-a.yaml
```

Enqueued via injector as **`KindSchedule`** (post-#369 relay-defer policy). **Max ~120 bytes** —
no ceremony body, no register.

On ping failure: LOUD escalate; pending row stays until confirm or operator clears. **Enqueue
precedes `CommitFired` in artifact mode** (below) so a crash after subprocess success still leaves
a pending row to replay — never silent notification loss.

### Ephemeral pending phase machine (extends #369 sidecar)

`schedulePending` gains `phase` for `mode: ephemeral` entries:

| Phase | Meaning | ReplayPending behavior |
|-------|---------|------------------------|
| `subprocess` | Child running | Re-spawn subprocess (idempotent artifact check) |
| `ping` | Subprocess done; ping enqueued, not yet confirmed (or `CommitFired` pending for artifact mode) | Re-enqueue completion ping only — **never** ceremony body; subprocess not re-run if artifacts already present |
| (absent) | Standing mode (#369 today) | Replay full `Message` as today |

Ceremony prompt text is **not** stored in pending rows for ephemeral schedules (only a hash +
artifact paths). Prevents replay from re-injecting register into standing pane.

### CommitFired policy (per ceremony class)

| Class | `CommitFired` when | Pending row role |
|-------|-------------------|------------------|
| Walk / scorecard | Subprocess exit 0 + artifact present + completion ping **enqueued** | `ping` phase replays notification until confirm; schedule ack does not wait for confirm |
| Parade / ack-required | Completion ping **confirmed** (#369) | `ping` phase until confirm; schedule ack == confirm |

Host-local overlay field `commit_on: artifact | ping` (default `artifact` for walk-shaped entries).

**Never `CommitFired` on subprocess start** — committing at spawn reintroduces the silent-drop
`#369` exists to kill.

**Artifact vs ping — one coherent story each:**

- **`commit_on: artifact`:** artifact presence is the schedule-success predicate; the completion
  ping is still a `KindSchedule` delivery with a durable pending row. **Order:** enqueue ping
  first (pending `phase=ping` exists) → `CommitFired` only after enqueue succeeds. A crash before
  enqueue ⇒ no `CommitFired`, ceremony re-attempts. A crash after enqueue ⇒ `ReplayPending`
  re-delivers the ping (subprocess not re-run when artifact already present). Ping confirm is
  **not** required for `CommitFired`; LOUD escalate + pending replay handle notification gaps.
- **`commit_on: ping`:** enqueue ping → `CommitFired` only on confirmed delivery (#369). Pending
  row is the sole recovery path until confirm.

### Subprocess failure escalation (poison ceremony guard)

A ceremony that fails every tick (bad prompt, missing binary, persistent artifact miss) must not
replay forever. Overlay fields (defaults shown):

| Field | Default | Behavior |
|-------|---------|----------|
| `max_subprocess_retries` | `3` | Consecutive subprocess failures before escalation |
| `escalate_after` | same as max | LOUD operator alert naming schedule + last error |
| Post-escalation | — | Schedule row marked **poisoned** in sidecar; no further auto-fire until operator clears |

Replay of an in-flight `subprocess` phase (artifact idempotent re-check) does not increment the
failure counter; only exit≠0 or artifact-miss after exit 0 counts.

### Durable-write serialization (load-bearing — do not ship without)

**Problem:** two ephemeral runners replacing the same anchor (e.g. `fleet-backlog.md`) race.

**Mechanism (recommended):** `internal/ceremony/anchorlock.go`

1. Before subprocess start, acquire **exclusive `flock`** on each `write_locks` path (create empty
   lock stub if missing).
2. Hold lock for subprocess duration + artifact verify.
3. Release on exit.

If lock unavailable (another ceremony holds it): **queue locally** in watch (one runner per anchor),
escalate if wait exceeds `relayStaleAlertInterval`.

**Limitation (P0):** flock serializes ephemeral runners only. The **standing interactive harness**
can still write the same anchor without flock. P0 accepts this for dogfood; P1 staging-file merge
if observed in practice.

**Alternatives considered:**
- *Funnel through standing XO* — reintroduces poisoning on the XO pane for merge work. Reject for
  ceremony body; acceptable only for a **short** merge ping if file-per-runner staging is used.
- *Staging file per runner* (`walk-agent-a-20260706.md`) + later merge — heavier; defer to P1
  unless anchor-lock proves insufficient on dogfood fleet.

### Scheduler integration

Replace (when `mode: ephemeral`):

```
Tick → enqueue full prompt to standing pane
```

With:

```
Tick → ceremony.Run (subprocess, off injector worker)
     → on subprocess success (exit 0 + artifacts verified):
         read commit_on from overlay (default artifact for walk-shaped entries)
         enqueue completion ping (KindSchedule) → pending row phase=ping
         if commit_on == artifact:
             CommitFired after enqueue succeeds (artifact already verified)
             ping confirm not required; pending replays on failure (LOUD escalate)
         if commit_on == ping:
             CommitFired only on confirmed ping delivery (#369)
     → if enqueue fails: no CommitFired; pending/subprocess replay per phase machine
     → never CommitFired on subprocess start
```

`CommitFired` timing is **per-class** via overlay `commit_on`. Both classes enqueue the ping
**before** schedule ack when `commit_on: artifact`; only ping-mode waits for confirm before ack.

### Relation to #369 items

| #369 item | This design |
|-----------|-------------|
| 1 Walk cadence (N schedules) | Unchanged — roster already supports N entries; ephemeral mode is per-entry |
| 2 Confirmed delivery | Completion **ping** uses #369 path; ceremony body never touches injector |
| 3 Memex integration | **Out of scope** |
| 4 R&D lane | **Out of scope** |

---

## Phasing

- **P0 (after #369 merge):** `internal/ceremony` subprocess runner; argv table for
  claude/grok/codex (**opencode fail-closed** until P1); flock serialization; host-local overlay +
  scheduler `mode: ephemeral` branch; ephemeral phase machine; per-class `commit_on`; subprocess
  failure escalation (`max_subprocess_retries`); completion ping via `KindSchedule`; tests with
  `agent-a`/`agent-b`; one live probe each for **claude/grok/codex** on dogfood host.
- **P1:** `flotilla ceremony run` CLI; migrate `flotilla parade` off standing-pane injection;
  visibility-synthesis ephemeral path; **opencode live verification + argv enablement**;
  staging-file merge for anchors.
- **P2:** Optional tmux visibility window; dash surfacing of in-flight ceremony runs.

**Explicitly out of P0:** `flotilla parade` CLI (`cmd/flotilla/parade.go` — still standing-pane),
visibility-synthesis wakes (`WakeSynthesis`), desk heartbeat beats.

---

## Open questions for the operator

1. **Default mode flip:** Should new generic `flotilla.example.json` schedules default to
   `ephemeral`, or stay `standing` until dogfood proves subprocess parity on all coordinator surfaces?
   Design recommends: example **may** show `ephemeral` as documentation; **shipped code default
   stays `standing`** until operator affirms flip on private roster. New schedules are not
   ephemeral-by-default without an explicit `mode` set.

2. **Artifact-only success — RESOLVED (design gate):** Encoded in `commit_on` per class.
   - Walk/scorecard (`commit_on: artifact`): subprocess exit 0 + artifact present ⇒ enqueue ping
     (pending row) ⇒ `CommitFired` after enqueue succeeds; standing pane down does not block schedule
     ack; notification recovers via pending replay + LOUD escalate.
   - Parade/ack-required (`commit_on: ping`): `CommitFired` only on confirmed completion ping (#369).
   - **Never** commit at subprocess start in either class.

3. **Coordinator ceremonies:** CoS/XO walks use the same subprocess path with their launch recipe cwd
   — confirm no special-case "coordinator must stay interactive" carve-out.

---

## Why this fits flotilla's architecture

The product already separates **host-local launch recipes** (cwd, command) from **portable roster**
(names, schedules). Subprocess ceremony extends `launch.HarnessSlot` shape as a **one-shot slot** —
same cwd, different argv template — without a second delivery stack. Standing sessions remain the
coordination surface; ceremonies become **side-channel bounded work**, matching how `flotilla-doctor`
already treats recovery as headless subprocess, not pane injection.