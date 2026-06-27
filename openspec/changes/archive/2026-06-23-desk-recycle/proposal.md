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

## Decisions carried in (parlayed with alpha-xo via flotilla message, 2026-06-23)

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
  PR blast-radius bounded (does NOT couple the mechanism to beta-xo's approval-sensitive parity check).

## What the design-trio rework changed (forks unchanged; mechanism + gate signals corrected)

The trio caught one systematic blind spot: the first draft templated the handoff/takeover injection
from MEMORY of the desk-l `/handoff` and `/takeover` skills. Reading the skill bodies shows BOTH are
human-INTERACTIVE (a "/handoff" confirmation pause with NO commit step; a "/takeover" *"shall I
start?"* pause) — so injecting the bare skills would deadlock a remote-driven recycle. The rework:
inject **non-interactive recycle-specific turns** (not the bare skills) that produce the same
artifacts; gate on **`Idle ∧ ComposerCleared`** (not `Idle` alone, which a skill-confirmation pause
also reads); detect the handoff via a **recycle-DESIGNATED committed blob** (not mtime + whole-tree-
clean); hold a **pipeline-spanning pane-txn lock** (and make `resume` take it too); add an **idle
precondition** (Phase 0) and a **relaunch-generation idempotency marker**; use **per-phase timeouts**.

## What Changes

- **`surface.Driver` gains `Close(pane) error`** — the per-surface graceful exit via slash-keys
  (claude `/exit`, keystroke verified in the 6.3 live-validation; grok returns `ErrNoGracefulClose`
  EXPLICITLY until #158 live-characterizes it; aider `/exit`; opencode/cursor `ErrNoGracefulClose`
  unless a clean quit is confirmed). The caller falls back to a hard respawn-kill on
  `ErrNoGracefulClose` — safe ONLY because the handoff is already durable by then. The pane drops to a
  Shell, the state `resume` already safely respawns into.
- **An OPTIONAL `surface.RecycleBridge` capability** — `HandoffPath(cwd, token)` (the per-harness
  designated-handoff path convention), `HandoffTurn(path)` (the NON-INTERACTIVE self-committing
  handoff instruction TEXT), `TakeoverTurn(path)` (the IMPERATIVE begin-work-immediately takeover
  instruction TEXT). The two turn methods return TEXT (pure, unit-testable); the command delivers them
  via CONFIRMED delivery. Claude Code is the reference. A surface WITHOUT the bridge cannot be
  context-preservingly recycled, and `flotilla recycle` REFUSES cleanly (never a silent degrade). This
  is where the cross-harness bridge (fork 3) is templated per harness.
- **`flotilla recycle <desk>`** — the fail-closed lifecycle orchestration, the safety-critical
  decision core (`runRecycle`) separated from I/O and unit-tested à la `runResume`, the WHOLE pipeline
  under one held `AcquirePaneTxn` lock (and `cmdResume` changed to take the same lock):
  1. resolve the pane (marker-first; refuse on none/ambiguous);
  2. **Phase 0 — idle precondition:** poll until `Idle ∧ ComposerCleared` (honour InjectSlash's
     "only inject when idle" contract; the XO triggers on chapter-complete, often mid-turn) — else ABORT;
  3. **Phase 1 — handoff:** deliver `HandoffTurn` via confirmed delivery, then the **fail-closed gate**:
     poll until the DESIGNATED handoff blob is durable (committed at HEAD in a git tree, or on disk for
     a non-git cwd, AND non-trivial) AND `Idle ∧ ComposerCleared`, within `--handoff-timeout` — else
     **ABORT** (desk keeps running, nothing closed; at-most-once-context-LOSS);
  4. **Phase 2 — close:** require `ComposerCleared` (selfHeal an overlay if enabled, else ABORT — never
     fire `/exit` into an overlay), `Close`, then confirm `Shell` within `--close-timeout` (RETRY on a
     transient `Unknown` glitch) — else ABORT naming the recovery (`flotilla resume <desk>` for a
     closed-but-not-relaunched dead desk); never relaunch on a possibly-live session;
  5. **Phase 3 — relaunch:** existing `RespawnPane` (marker survives → control re-bound), verify the
     marker read-back, stamp `@flotilla_recycle_gen=<token>`;
  6. **Phase 4 — takeover:** poll until `Idle ∧ ComposerCleared` within `--boot-timeout`, re-read the
     gen marker (ABORT if superseded), deliver `TakeoverTurn` via confirmed delivery EXACTLY ONCE, then
     poll for a `Working` edge (the resumption-confidence signal; best-effort) within `--takeover-timeout`.
- **`--dry-run`** prints the resolved plan (pane, recipe, the designated path, the handoff/takeover
  turns it would inject) without acting; **per-phase timeouts** (`--handoff-timeout`, `--close-timeout`,
  `--boot-timeout`, `--takeover-timeout`) bound the gates, which have order-of-magnitude-different
  latencies (a handoff turn is multi-minute; close/boot are seconds).
- **Coordination protocol (a flotilla finding from this very parlay):** a recycled/remote desk and its
  remote XO MUST coordinate via **flotilla messages**, never an in-pane interactive menu — an in-pane
  `AskUserQuestion` is UNANSWERABLE by a remote XO over the relay (keystrokes navigate the menu, not
  the composer; the panel-block class, #156). The injected takeover turn tells the fresh session it is
  remote-driven and to surface any clarification via a message, not an interactive prompt.

## Out of scope (separate follow-ups)

- **#158 — the claude→grok cutover + the capability-parity check** (does Grok's harness support
  subagents / parallel-review / git-PR / MCP, which beta-xo relies on, owning delta-xo's
  approval-sensitive order path). Gated on #157's same-harness proof. This change builds the bridge
  cross-harness-READY (arbitrary recipe + a harness-agnostic handoff artifact + a per-driver
  `RecycleBridge` SPI) but exercises only claude→claude; the only harness meeting the recycle-capable
  bar today is Claude Code. ("The markdown bridge already works — this session is proof" is a
  SAME-harness claim, NOT evidence for the cross-harness pillar; the spec does not stand it as such.)
- **Deciding WHEN a chapter is complete** — the XO's judgment; #157 is the mechanism, the XO is the
  trigger.
- **A structured/normalized handoff schema** (fork-3 option B) — deferred unless #158 shows the
  freeform markdown does not transfer cross-harness.

## Impact

- **`internal/surface/surface.go`** — `Close` on `Driver`; `ErrNoGracefulClose`; the optional
  `RecycleBridge` interface (`HandoffPath`/`HandoffTurn`/`TakeoverTurn`); a `RecycleSupport(d)
  (RecycleBridge, bool)` type-assert helper for the clean refusal.
- **`internal/surface/{claude,grok,aider,opencode}.go`** — each implements `Close` (claude `/exit`;
  grok `ErrNoGracefulClose` until #158; aider `/exit`; opencode/cursor `ErrNoGracefulClose`); the
  claude driver implements `RecycleBridge` (the `.claude/handoffs/` path convention + the non-
  interactive handoff turn + the imperative takeover turn) as the reference.
- **`cmd/flotilla/recycle.go`** (new) — the command + the `runRecycle` fail-closed core; reuses the
  `resume` ops (resolve / respawn / readMarker / tag) and the launch-recipe resolution; delivers turns
  via `surface.Confirm`; holds `AcquirePaneTxn` across the pipeline.
- **`cmd/flotilla/resume.go`** — `cmdResume` takes the SAME `AcquirePaneTxn` lock (so recycle×resume
  cannot interleave on a pane — the race `resume.go:176-183` admits, which recycle widens).
- **`internal/deliver`** — `HandoffDurable(cwd, designatedPath, minBytes)` (the committed-blob /
  on-disk durability + minimum-viability check, git-root resolved from cwd) and the
  `@flotilla_recycle_gen` pane-option stamp/read, both injectable. (The first draft's mtime
  fresh-handoff detector and tracked-tree-clean check are REPLACED by this exact check.)
- **`docs/watch-runbook.md`** — the recycle procedure + the remote-parlay-via-message protocol +
  `--dry-run` as the recommended first step.
- **Risk: HIGH.** Recycle CLOSES a live desk (the running session is intentionally ended; context is
  preserved by the handoff). Guarded by: the fail-closed completion gate (ABORT before any close if
  the handoff is not durably confirmed — at-most-once-context-LOSS); the `ComposerCleared`-before-close
  guard + the close→Shell confirmation (retry-on-Unknown; never relaunch on a live session); the
  pipeline-spanning pane-txn lock (no recycle×resume interleave; watch-delivery frozen); the relaunch-
  generation idempotency marker (at-most-once takeover); reuse of the already-hardened `resume`
  relaunch/marker path; and a mandatory **live claude→claude end-to-end validation on one real desk**
  (cold-test the artifact) before the capability is used in anger — mirroring surface-self-heal's
  live-validation gate.
