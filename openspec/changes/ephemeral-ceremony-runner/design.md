# Design — ephemeral ceremony runner

**Status:** Design-only (operator-raised concern, 2026-07-06). Implementation follows operator gate.

## The gap, stated in one line

Ceremony dispatches (walk, parade, visibility synthesis beats) inject **full ceremony prompts**
into a desk/XO/CoS **standing session** every time they fire. The standing pane accumulates
ceremony register across runs — context poisoning — while the product has no **disposable
one-shot invocation** path, only `deliver.ResolvePane` + tmux keystrokes into the persistent
harness.

## Grounded seams (cite, do not re-derive)

| Seam | Location | What it gives this design |
|------|----------|---------------------------|
| Standing-pane injection | `cmd/flotilla/watch.go` — scheduler `KindSchedule`, `newDeskHeartbeatDispatch` | Today every ceremony goes through the injector into the **registered standing pane** |
| Session hygiene (not disposable) | `internal/surface/surface.go:192` `RotateContext` | Wipes standing context **in place** after the fact; does not isolate one task |
| Host cwd / worktree | `internal/launch/launch.go` `Recipe.Cwd`; `internal/workspace/worktree.go` `ProvisionWorktree` | Ceremony runner inherits desk filesystem context without new access machinery |
| Headless precedent | `deploy/flotilla-doctor.sh` — `claude --print` recovery agent | Proves subprocess one-shot is already trusted for **side-channel** work |
| Tmux pane creation | `internal/deliver/resume.go` `NewWindow` / `NewSession` | Optional visibility path; not required for subprocess-first |
| Confirmed delivery (#369) | `internal/watch/inject.go` `KindSchedule` | Standing pane still receives **short pings** via confirmed-delivery; ceremony body does not |

## One-shot harness verification (first design obligation — probed 2026-07-06)

| Surface | One-shot mode | Verified? | Notes |
|---------|---------------|-----------|-------|
| **claude-code** | `claude -p/--print "<prompt>"` | **Yes** | `--print` documented; doctor uses `claude --print` |
| **grok** | `grok -p/--single "<prompt>"` or `--prompt-file <path>` | **Yes** | `--single` = "prints response to stdout and exits"; `--permission-mode` + `--always-approve` for unattended |
| **codex** | `codex exec "<prompt>"` | **Yes** | `codex exec` subcommand is explicitly non-interactive |
| **opencode** | `opencode run "<message>"` | **Yes** (CLI help) | `opencode run` exists; **live exit-code / cwd behavior not yet probed** — P0 gate includes one live smoke per surface before claiming parity |

**Falsified assumption to drop:** "every surface needs a tmux pane to run a ceremony." Three of four
already expose headless/single-turn CLIs; opencode needs a live probe in P0 implementation, not design.

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

**Pure policy** in `internal/ceremony`; **watch** wires scheduler → `ceremony.Run` when
`schedule.mode == "ephemeral"` (default for new ceremony schedules).

### Roster / schedule extension

```json
{
  "name": "walk-agent-a",
  "at": "08:00Z",
  "to": "agent-a",
  "prompt": "prompts/walk.md",
  "mode": "ephemeral",
  "artifacts": ["state/scorecards/walk-agent-a.yaml"],
  "write_locks": ["fleet-backlog.md"]
}
```

- `mode`: `"ephemeral"` | `"standing"` (default **`standing`** for backward compat until operator
  flips fleet config — dogfood migration is host-local roster, not repo defaults).
- `artifacts` / `write_locks`: host-local roster fields (gitignored fleet config), validated at load.
- Generic example in `flotilla.example.json` uses `agent-a` / `agent-b` synthetic names only.

### Completion ping (standing session — minimal poisoning)

After `ceremony.Run` succeeds (exit 0 + artifact checks):

```
[flotilla ceremony] walk-agent-a complete — scorecard: state/scorecards/walk-agent-a.yaml
```

Enqueued via existing injector as `KindSchedule` or a dedicated `KindCeremonyPing` treated like
schedule (confirmed delivery per #369). **Max ~120 bytes** — no ceremony body, no register.

On failure: LOUD escalate (same posture as undelivered relay), pending row stays until operator
clears or retry succeeds.

### Durable-write serialization (load-bearing — do not ship without)

**Problem:** two ephemeral runners replacing the same anchor (e.g. `fleet-backlog.md`) race.

**Mechanism (recommended):** `internal/ceremony/anchorlock.go`

1. Before subprocess start, acquire **exclusive `flock`** on each `write_locks` path (create empty
   lock stub if missing).
2. Hold lock for subprocess duration + artifact verify.
3. Release on exit.

If lock unavailable (another ceremony holds it): **queue locally** in watch (same serialize pattern
as injector — one runner per anchor at a time), escalate if wait exceeds `relayStaleAlertInterval`.

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
     → on success: enqueue completion ping only
     → CommitFired on ping confirmed (#369)
```

`last_fired` still commits on **confirmed delivery of the ping**, not on subprocess start — same
#369 semantics extended: subprocess completion is necessary but not sufficient until the standing
desk acknowledges the short ping (or artifact-only mode for desks without standing pane — see open
question).

### Relation to #369 items

| #369 item | This design |
|-----------|-------------|
| 1 Walk cadence (N schedules) | Unchanged — roster already supports N entries; ephemeral mode is per-entry |
| 2 Confirmed delivery | Completion **ping** uses #369 path; ceremony body never touches injector |
| 3 Memex integration | **Out of scope** |
| 4 R&D lane | **Out of scope** |

---

## Phasing

- **P0 (design gate → implement):** `internal/ceremony` subprocess runner; claude/grok/codex
  argv builders; flock serialization; roster `mode: ephemeral`; scheduler branch; completion ping;
  tests with synthetic `agent-a`/`agent-b`; one live probe per surface on dogfood host.
- **P1:** `flotilla ceremony run` CLI for manual/adhoc ceremonies; opencode live verification;
  staging-file merge pattern for multi-writer anchors if lock contention is too coarse.
- **P2:** Optional tmux visibility window; dash surfacing of in-flight ceremony runs.

---

## Open questions for the operator

1. **Default mode flip:** Should new generic `flotilla.example.json` schedules default to
   `ephemeral`, or stay `standing` until dogfood proves subprocess parity on all coordinator surfaces?
   Design recommends: example shows `ephemeral`; shipped default in code stays `standing` until
   operator affirms flip on private roster.

2. **Artifact-only success:** If the standing desk has no live pane (crashed), is subprocess exit +
   artifact presence sufficient to `CommitFired`, with escalate-only (no ping)? Design leans **yes**
   for walk scorecards; **no** for parades that require coordinator ack.

3. **Coordinator ceremonies:** CoS/XO walks use the same subprocess path with their launch recipe cwd
   — confirm no special-case "coordinator must stay interactive" carve-out.

---

## Why this fits flotilla's architecture

The product already separates **host-local launch recipes** (cwd, command) from **portable roster**
(names, schedules). Subprocess ceremony extends `launch.HarnessSlot` shape as a **one-shot slot** —
same cwd, different argv template — without a second delivery stack. Standing sessions remain the
coordination surface; ceremonies become **side-channel bounded work**, matching how `flotilla-doctor`
already treats recovery as headless subprocess, not pane injection.