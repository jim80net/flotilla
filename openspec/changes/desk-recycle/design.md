# Design — desk-recycle

## Context

#157 asks for an XO-triggered "close the chapter, restart fresh" primitive that preserves context via
flotilla scaffolding, so a desk never has to run until it compacts. #158 (gated on #157) then moves
family-office claude→grok using the same primitive — so #157 MUST be built cross-harness-capable from
the start, not retrofitted.

The relaunch half is already built and harness-agnostic (`cmd/flotilla/resume.go` + `deliver`): it
drives `recipe.Launch` verbatim and `RespawnPane` reuses the pane id so the `@flotilla_agent` marker
survives. The new work is the **graceful close** and the **context-bridge orchestration** that wraps
the existing relaunch in a fail-closed lifecycle.

This design was parlayed with hydra-ops (the remote XO) over a flotilla message on 2026-06-23. The
four forks and their resolutions are in `proposal.md`. Fork 3 (the cross-harness context contract) is
a product-pillar decision carried to the operator; #157 proceeds on the reversible provisional default
(portable markdown + a templated turn) so it is not blocked on the escalation.

## The recycle state machine

`flotilla recycle <desk>` is a linear pipeline whose ONLY irreversible step (the close) is gated
behind a durably-confirmed handoff. The decision core `runRecycle(ops, plan)` is separated from I/O
(à la `runResume`) so each gate's ABORT behaviour is unit-tested by injecting signals.

```
resolve pane (marker-first)
  ├─ None       → error "no pane for <desk>; nothing to recycle"
  ├─ Ambiguous  → error "fleet mis-tagged; re-tag, then retry"   (never act on ambiguity)
  └─ Unique ↓
record baseline (now=t0; the existing handoff fileset; the launch recipe; the desk key)
  │
  ├─ PHASE 1 — HANDOFF (cooperative: the desk writes its own durable bridge)
  │   InjectHandoff(pane)                        # claude: /handoff  (RecycleBridge)
  │   poll until ALL of, within --timeout:
  │     (a) a handoff artifact with mtime > t0 exists under the desk's handoffs dir
  │     (b) Assess(pane) == Idle                 # the handoff turn finished, not mid-write
  │     (c) durable outputs committed            # tracked-tree clean AND the handoff committed
  │   └─ timeout / ambiguous → ABORT: desk UNTOUCHED, still running, nothing closed.
  │        (at-most-once-context-loss — the close NEVER happens on an unconfirmed handoff)
  │
  ├─ PHASE 2 — GRACEFUL CLOSE (the one irreversible step; the handoff is durable by here)
  │   Close(pane)                                # claude: /exit  → process exits → pane → Shell
  │     └─ ErrNoGracefulClose (surface has no clean exit, e.g. cursor)
  │          → fall back to RespawnPane-kill      # safe: handoff already durable
  │   poll until Assess(pane) == Shell, within --timeout:
  │   └─ not Shell → ABORT the relaunch: error "close did not confirm; desk may still be live"
  │        (NEVER relaunch on top of a live session → no duplicate process / double-bound marker)
  │
  ├─ PHASE 3 — RELAUNCH (reuse the hardened resume primitive)
  │   RespawnPane(pane, cwd, recipe.Launch)      # reuses pane id → @flotilla_agent marker SURVIVES
  │   ReadMarker(pane) == desk.key  else error   # confirm the marker landed (resume's read-back)
  │
  └─ PHASE 4 — TAKEOVER (point the fresh, clean-context session at the bridge)
      poll until Assess(pane) == Idle, within --timeout  # the fresh harness finished booting
      InjectTakeover(pane, handoffPath)          # claude: /takeover <path>; grok: plain instruction
      (exactly once)
```

### Why this ordering is the at-most-once-context-loss property

The chapter's context lives in two places during a recycle: the running session (volatile, dies on
close) and the handoff artifact (durable). The ONLY way to lose context is to close before the handoff
is durable. Phase 1's gate is **fail-closed**: it ABORTS (leaving the desk running) on any
un-confirmation, so the close in Phase 2 is reached ONLY after the handoff artifact provably exists,
the turn finished, and the outputs are committed. Worst case is therefore a *no-op recycle* (the desk
keeps running with its context intact), never a lost chapter. A crash *between* Phase 1 and Phase 2
also loses nothing — the handoff is already durable.

### Why the close→Shell confirmation matters (Phase 2 gate)

`Close` injects a clean exit, but a slash exit can fail to land (a wedged composer, an overlay). If we
relaunched without confirming the old process actually died, `RespawnPane -k` would kill *whatever* is
in the pane — but if the close silently did nothing and the session is alive and mid-operation, we'd
be killing a LIVE session AFTER claiming a graceful close. Confirming `Shell` first means the relaunch
respawns a known-dead pane (the exact precondition `resume`'s `ResolveUnique + StateShell` branch is
built for). A close that does not confirm Shell ABORTS — the operator/XO investigates rather than the
tool force-killing.

## The SPI additions

### `Close(pane string) error` — on the core `Driver` interface (fork 1)

```go
// Close gracefully exits the agent's session in the pane (the per-surface clean exit, e.g. claude
// "/exit"), flushing the harness's own session store and dropping the pane to a Shell. A surface with
// NO clean in-session exit returns ErrNoGracefulClose so the caller may fall back to a hard
// respawn-kill — safe ONLY because recycle has already made the handoff durable. Close MUST NOT blind-
// kill; the kill fallback is the caller's explicit, handoff-gated decision.
Close(pane string) error
```

Every driver implements it (compile-forced — completeness): claude `/exit`, grok `/exit` (confirm in
the grok slash menu during impl), aider `/exit`, opencode its quit (or `ErrNoGracefulClose` if none is
confirmed). The injection mechanism is the slash-keys primitive (literal keystrokes, like `Rotate`'s
`/clear`), NOT bracketed-paste `Submit` — a slash command pasted as a bracketed block may not trigger
the harness's command parser.

### `RecycleBridge` — OPTIONAL capability (forks 2 + 3)

```go
// RecycleBridge is an OPTIONAL Driver capability: the two per-harness context-preservation hooks a
// recycle drives. A surface that implements it can be context-preservingly recycled; a surface WITHOUT
// it makes `flotilla recycle` REFUSE cleanly (never a silent degrade). Claude Code is the reference
// (memex /handoff + /takeover). READ-ONLY w.r.t. flotilla state; it only injects turns into the pane.
type RecycleBridge interface {
    // InjectHandoff triggers the desk to emit its durable handoff (claude: /handoff) — the markdown
    // context bridge, written + committed before the session is closed.
    InjectHandoff(pane string) error
    // InjectTakeover points a freshly-relaunched session at the handoff so it resumes the chapter from
    // a clean context window. The handoff PATH is harness-agnostic (a markdown file); only the injected
    // TURN is templated per harness (claude: "/takeover <path>"; a harness without a takeover skill: a
    // plain "Read <path> and take over per it, you are remote-driven — surface clarifications via a
    // flotilla message, not an interactive prompt"). This is fork-3 provisional-A; the operator confirms
    // the cross-harness context-contract pillar.
    InjectTakeover(pane, handoffPath string) error
}
```

Optional (not on the core interface) because not every surface has a handoff/takeover skill, and the
honest behaviour for one that doesn't is a clean refusal, not a degraded recycle. Keeping it optional
also keeps fork-3 low-commitment — the contract is a thin per-driver template, revisable to a
structured schema if #158 demands it.

### Why handoff/takeover injection lives on the Driver (not in the command)

Both operations are intrinsically harness-specific: claude triggers `/handoff` via slash-keys and
`/takeover <path>` via slash-keys; grok's handoff equivalent and its takeover phrasing differ, and a
plain instruction is delivered via paste, not slash-keys. The *mechanism* (slash vs paste) AND the
*template* both vary by harness — exactly the per-surface policy the Driver SPI exists to encapsulate.
The command orchestrates the lifecycle; the driver knows how to speak to its harness.

## Reuse, not reinvention

Phase 3 is literally `runResume`'s `ResolveUnique + StateShell → RespawnPane → ReadMarker` branch.
Rather than duplicate it, `runRecycle` calls the same injected ops (`respawn`, `readMarker`, `tag`,
`resolve`) that `resume` uses. The recycle command's `recycleOps` is a superset of `resumeOps` plus
the close/handoff/takeover/gate hooks. This keeps the two lifecycle commands sharing one hardened
relaunch+marker path (a single place to get the marker-survival invariant right).

## The completion-gate signals (Phase 1) — provenance, not heuristics

- **(a) fresh handoff artifact** — the robust "the bridge was written" signal: a file under the desk's
  handoffs directory (`.claude/handoffs/` for the reference) with mtime strictly after the injection
  timestamp `t0`. Detecting a NEW file (vs any file) avoids a stale prior handoff false-passing the
  gate. Injectable as `handoffFresh(cwd, since) (path, ok, err)`.
- **(b) `Assess == Idle`** — the handoff turn finished (the desk is not mid-write). Uses the existing
  surface driver; converges with how every other flotilla gate reads "the desk is done."
- **(c) durable outputs committed** — the operator's stated requirement ("durable outputs … committed").
  Proxy: in a git work-tree, no uncommitted changes to TRACKED files AND the fresh handoff file is
  committed (not dangling). Untracked scratch (logs, tmp) does NOT block — it is not a durable output
  unless added. A non-git cwd skips (c) (the handoff artifact + Idle still gate). Injectable as
  `treeClean(cwd) (bool, err)`; fail-closed (a git error → not-clean → ABORT, never assume clean).

All three must hold simultaneously within `--timeout`. The timeout is generous by default (a handoff +
commit is a multi-minute turn) and configurable; expiry is an ABORT, not a force-close.

## Coordination protocol — remote desks parlay via message (a flotilla finding)

This parlay surfaced the rule the hard way: hydra-ops (a REMOTE XO over the relay) could not answer an
in-pane `AskUserQuestion` — the relay delivers keystrokes that navigate the menu, not select an option
(the panel-block class, #156). Recycle bakes this in: the injected takeover turn tells the fresh
session it is remote-driven and to surface any clarification via a flotilla message
(`flotilla notify` / a channel message), NEVER an interactive in-pane prompt. The recycle command
itself emits all status to its own stdout/log (a side channel), never into the desk's composer
(`agent-control-notices-to-side-channel`).

## Cross-harness readiness for #158 (built-in, not exercised)

- The relaunch already targets an arbitrary recipe — no claude hard-coding (verified in `resume.go`).
- The handoff ARTIFACT is a markdown file — harness-agnostic by construction.
- `RecycleBridge` is per-driver, so a grok bridge (its handoff equivalent + a plain takeover
  instruction) is a #158 addition behind the same interface, not a retrofit.
- **Capability-parity is a #158 gate, surfaced not silently degraded:** before the family-office
  cutover, #158 confirms Grok's harness supports subagents / parallel-review / git-PR / MCP (family-
  office runs multi-agent reviews and owns tactical-head's real-money order path). If it genuinely
  cannot, that is an operator-facing finding, not a quiet downgrade. #157 does not assume the answer.

## Alternatives considered (and rejected)

- **SIGTERM / RespawnPane-kill as the close** (fork 1 alts) — a TUI may not flush its session store on
  a signal/kill; not the graceful exit #157 asks for. Per-driver `Close` is the surface's own clean
  path.
- **Verify-only or two-command recycle** (fork 2 alts) — both put the safety-critical
  handoff-before-close ordering in a human's hands; the whole value of a primitive is code-enforcing it.
- **A structured handoff schema now** (fork 3 alt B) — premature; the markdown bridge already works
  (this session is proof). Revisit only if #158 shows freeform doesn't transfer.
- **Recycle decides WHEN to recycle** — out of scope; that is the XO's judgment (#157 is the mechanism).

## Risks

- **HIGH — recycle ends a live session.** Mitigated by the fail-closed Phase-1 gate (no close without a
  durable handoff), the Phase-2 close→Shell confirmation (no relaunch on a live session), reuse of the
  hardened resume relaunch/marker path, and a mandatory live claude→claude end-to-end validation on one
  real desk before use in anger.
- **The desk does not cooperate with `/handoff`** (ignores it, errors, or the skill is absent) — the
  Phase-1 gate times out and ABORTS (desk keeps running). The XO sees the abort and intervenes; no loss.
- **A long handoff turn vs the timeout** — the default timeout is generous and configurable; an abort
  is recoverable (re-run recycle), a premature force-close would not be — so the gate errs toward abort.
