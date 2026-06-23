# Proposal — desk-recycle (XO-triggered close-and-restart on chapter-complete; context preserved via the handoff bridge)

## Why

A long-running desk that loops across disparate tasks accumulates context until it **compacts** — and
post-compaction quality degrades (summaries lose nuance; the desk re-derives or drops threads). There
is no clean "close the chapter, restart fresh" primitive today; desks run until they compact. Operator
directive (#157, 2026-06-22): *"Develop the ability to manage closing and restarting desks when a task
is logically complete … close a chapter out and start with a fresh context window … rely on flotilla
scaffolding to make sure the relevant context activities are preserved."*

**Half of the mechanism already exists.** `flotilla resume` performs the harness-agnostic **relaunch**
leg: it drives `recipe.Launch` verbatim (`bash -lc 'grok…'`, `claude --name…`), and `RespawnPane`
reuses the pane id so the `@flotilla_agent` marker SURVIVES — the control channel re-binds for free
(#157 step 5, already solved). What is missing is the other half:

1. **A graceful CLOSE.** The `surface.Driver` SPI has `Submit`/`Assess`/`Rotate` but **no `Close`**.
   `Rotate` injects `/clear` (claude) / `/new` (grok) — it keeps the SAME process, useless for a
   cross-harness move and for a truly fresh window. `resume`'s only close is `RespawnPane -k` — a
   **kill**, not the graceful exit #157 demands (a kill can corrupt the harness's own session store
   mid-write).
2. **The context-bridge ORCHESTRATION** — drive the desk to emit its durable handoff, gate on that
   landing durably, only THEN close, relaunch, and point the fresh session at the handoff.

## Decisions carried in (parlayed with hydra-ops via flotilla message, 2026-06-23)

- **Fork 1 — graceful close: a per-driver `Close()` on the SPI.** Consistent with the SPI's "Driver
  DECIDES, deliver EXECUTEs"; per-harness, unit-testable; flushes the harness session store before
  exit. (SIGTERM / RespawnPane-kill rejected — they risk a half-written transcript.)
- **Fork 2 — command shape: ONE `flotilla recycle <desk>`** that gates-on-completion → handoff →
  graceful close → relaunch → inject-takeover, **FAIL-CLOSED** (timeout/ambiguous → ABORT, leave the
  desk running). This makes *handoff-committed-before-close* CODE-ENFORCED — the at-most-once-context-
  loss property — not operator discipline. (Verify-only / two-command shapes rejected: they hand the
  ordering knife back to a human.)
- **Fork 3 — cross-harness context bridge ⭐ PRODUCT-PILLAR (operator-to-confirm):** provisionally **a
  portable markdown handoff + a per-harness-templated "take over" turn** (claude `/takeover <path>`;
  grok "read `<path>` and take over"). Zero new format; it IS the manual version flotilla already
  dogfoods. A structured handoff schema is deferred unless the claude→grok exercise (#158) proves it
  necessary. Marked `operator-to-confirm` in the spec so #157 is not blocked on the escalation.
- **Fork 4 — scope: #157 = the mechanism + a claude→claude same-harness end-to-end proof; #158 = the
  claude→grok cutover + the capability-parity check.** Per #158's stated hard dependency; keeps the
  PR blast-radius bounded (does NOT couple the mechanism to family-office's real-money parity check).

## What Changes

- **`surface.Driver` gains `Close(pane) error`** — the per-surface graceful exit (claude/grok/aider/
  opencode inject their own clean exit; a surface with no clean in-session exit returns
  `ErrNoGracefulClose` so the caller falls back to a hard respawn-kill — safe ONLY because the handoff
  is already durable by then). The pane drops to a Shell, which is exactly the state `resume` already
  safely respawns into.
- **An OPTIONAL `surface.RecycleBridge` capability** — `InjectHandoff(pane)` + `InjectTakeover(pane,
  handoffPath)`, the two per-harness context hooks. Claude Code is the reference (memex `/handoff` +
  `/takeover` skills). A surface WITHOUT the bridge cannot be context-preservingly recycled, and
  `flotilla recycle` REFUSES cleanly (never a silent degrade). This is where the cross-harness bridge
  (fork 3) is templated per harness.
- **`flotilla recycle <desk>`** — the fail-closed lifecycle orchestration, with the safety-critical
  decision core (`runRecycle`) separated from I/O and unit-tested à la `runResume`:
  1. resolve the pane (marker-first; refuse on none/ambiguous);
  2. `InjectHandoff`, then the **fail-closed completion gate**: poll until a FRESH handoff artifact
     exists (mtime after injection) AND the pane is back to `Idle` AND durable outputs are committed
     (tracked-tree clean + the handoff committed), within a timeout — else **ABORT** (the desk keeps
     running, nothing closed; at-most-once-context-loss);
  3. `Close`, then confirm the pane reached `Shell` within a timeout — else ABORT the relaunch (never
     relaunch on top of a still-live session → no duplicate process);
  4. relaunch via the existing `RespawnPane` (marker survives → control re-bound) and verify the
     marker read-back;
  5. wait for the fresh session to reach `Idle`, then `InjectTakeover(pane, handoffPath)` exactly once.
- **`--dry-run`** prints the resolved plan (pane, recipe, the handoff/takeover turns it would inject)
  without acting; **`--timeout`** bounds each gate.
- **Coordination protocol (a flotilla finding from this very parlay):** a recycled/remote desk and its
  remote XO MUST coordinate via **flotilla messages**, never an in-pane interactive menu — an in-pane
  `AskUserQuestion` is UNANSWERABLE by a remote XO over the relay (keystrokes navigate the menu, not
  the composer; the panel-block class, #156). The injected takeover turn tells the fresh session it is
  remote-driven and to surface any clarification via a message, not an interactive prompt.

## Out of scope (separate follow-ups)

- **#158 — the claude→grok cutover + the capability-parity check** (does Grok's harness support
  subagents / parallel-review / git-PR / MCP, which family-office relies on, owning tactical-head's
  real-money order path). Gated on #157's same-harness proof. This change builds the bridge
  cross-harness-CAPABLE (arbitrary recipe + a harness-agnostic handoff artifact) but exercises only
  claude→claude.
- **Deciding WHEN a chapter is complete** — the XO's judgment; #157 is the mechanism, the XO is the
  trigger.
- **A structured/normalized handoff schema** (fork-3 option B) — deferred unless #158 shows the
  freeform markdown does not transfer cross-harness.

## Impact

- **`internal/surface/surface.go`** — `Close` on `Driver`; `ErrNoGracefulClose`; the optional
  `RecycleBridge` interface; a `Recycle(d, pane, …)`-style helper that routes the bridge + fallback.
- **`internal/surface/{claude,grok,aider,opencode}.go`** — each implements `Close`; the claude driver
  implements `RecycleBridge` (`/handoff`, `/takeover <path>`) as the reference.
- **`cmd/flotilla/recycle.go`** (new) — the command + the `runRecycle` fail-closed core; reuses the
  `resume` ops (resolve / respawn / readMarker / tag) and the launch-recipe resolution.
- **`internal/deliver`** — a fresh-handoff detector (new file under a handoffs dir since a timestamp)
  and a tracked-tree-clean check, both injectable.
- **`docs/watch-runbook.md`** — the recycle procedure + the remote-parlay-via-message protocol.
- **Risk: HIGH.** Recycle CLOSES a live desk (the running session is intentionally ended; context is
  preserved by the handoff). Guarded by: the fail-closed completion gate (ABORT before any close if
  the handoff is not durably confirmed — at-most-once-context-loss); the close→Shell confirmation
  (never relaunch on a live session); reuse of the already-hardened `resume` relaunch/marker path; and
  a mandatory **live claude→claude end-to-end validation on one real desk** (cold-test the artifact)
  before the capability is used in anger — mirroring surface-self-heal's live-validation gate.
