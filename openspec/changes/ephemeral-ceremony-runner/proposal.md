# Proposal — ephemeral ceremony runner

## Problem

Scheduled and heartbeat-driven ceremonies (walks, parades, visibility synthesis) today
inject full ceremony prompts into a desk/XO/CoS **standing tmux pane** via
`deliver.ResolvePane` + keystroke submit (`cmd/flotilla/watch.go`). Repeated runs risk
**context poisoning**: the agent's long-lived session accumulates ceremony register and
phrasing until the standing session is tuned toward the ceremony rather than fleet work.

Operator concern (2026-07-06): "say parade 1000 times and eventually it's all you think
about." Applies to desks, XOs, and CoS alike.

## What changes

A flotilla **product capability** — not ops-only config — that runs bounded ceremony tasks
in a **throwaway subprocess** (one-shot harness invocation), writes artifacts only to
agreed durable paths, tears down, and pings the standing session with a **short completion
line** (never the ceremony transcript).

Composes with:
- **#369** walk-cadence confirmed delivery (standing-pane path remains for non-ceremony traffic)
- **`RotateContext`** (session hygiene — orthogonal; does not replace disposable invocation)
- **`launch.Recipe` / `ProvisionWorktree`** (cwd + worktree inheritance)

## Scope

**In:**
- Design + P0 implementation path for subprocess ceremony runner
- Per-surface one-shot harness verification (claude/grok/codex/opencode)
- Durable-write serialization guard (anchor-replace races)
- Scheduler/roster opt-in `mode: ephemeral` for ceremony-class dispatches
- Short completion ping to standing pane after confirmed artifact write

**Out (this change):**
- Memex integration for walk findings (#369 item 3)
- R&D lane ephemeral spawn (#369 item 4)
- Replacing all `KindDetector` desk heartbeats (only ceremony-class schedules/prompts)

## Success criteria

1. A scheduled walk/parade fires in a subprocess with fresh context; standing pane receives
   only `"<ceremony> complete — see <path>"`.
2. Artifact lands at the declared durable path; no ceremony transcript injected into standing session.
3. Two concurrent ceremonies targeting the same anchor-replace file serialize (no clobber).
4. Generic product code — synthetic agent names in tests/fixtures; no deployment topology in repo.