# Design — harness / subscription switching (`flotilla switch`)

**Status:** Design-trio gate PASSED (systems-review + open-code-review + STORM, code-grounded). The
trio findings below REFINE the approved source design (`docs/harness-subscription-switching.md`);
where a finding conflicts with the source doc, the FINDING wins (the conflicts are named in §9).
**Problem:** A provider-wide server-side throttle (`Server is temporarily limiting requests`,
operator 2026-06-29) stalled every Claude desk at once; flotilla cannot fail a desk over to a
different harness/subscription while preserving its in-flight context.
**Goal:** A `flotilla switch` that hands a desk from a FROM harness to a TO harness — handoff →
relaunch → takeover — with recycle's fail-closed safety, plus a declared per-desk failover chain and
a runtime active-slot overlay so routing follows the switch with no roster commit.

This design EXTENDS `launch.Recipe`, the workspace precedence chain, the surface `Driver` SPI, and the
recycle/resume cores. It does NOT replace any of them, and §2 establishes the load-bearing invariant
that the single-driver `recycle` core is untouched.

---

## 1. The four pinned gates (NON-NEGOTIABLE — they appear as `switch` requirements)

1. **Fresh-launch fallback works with NOTHING.** A cold `flotilla resume` of a desk on its TO harness
   with NO from-harness, NO bundle, and NO (or empty) corpus MUST still produce a productive desk
   (harness identity files + workspace `state.md`). Requiring `from`, a bundle, a hint, or a populated
   corpus before a desk can run is FORBIDDEN.
2. **Harness-neutral paths.** The handoff and the continuity bundle live at product-owned, harness-
   neutral paths (`<project_root>/.flotilla/handoffs/switch-<token>.md`,
   `<project_root>/.flotilla/switch/<flotilla_agent>/continuity-<token>.json`) — never a claude- or
   grok-branded directory — so the TO harness always reads the same path family the FROM harness wrote.
3. **Pointer, not content.** flotilla NEVER embeds memex-retrieved corpus text or operator-constraint
   prose in any artifact it writes. The continuity bundle carries a BARE-STRING pointer/hint
   (`memex_injection_hint`) only; memex owns the corpus query.
4. **`approval_sensitive` NEVER auto-switches.** A desk marked `approval_sensitive`
   (`internal/roster/roster.go:39-44`) is switched ONLY by an operator `--confirm`. The refusal is
   enforced at the watch ENQUEUE (the candidate is never enqueued), not merely at exec — so a bug in
   the exec path can never auto-switch an order-placing desk.

---

## 2. P1-A (RESHAPE, load-bearing) — `switch` is a NEW two-driver core, NOT a recycle mirror

The source doc framed `flotilla switch`'s lifecycle as "mirrors recycle's phased core". That is
imprecise and the trio flagged it as the most important reshape: **`runRecycle` resolves exactly ONE
driver** (`cmd/flotilla/recycle.go:389` — `surface.Get(agentSurface(cfg, agentName))`) and uses that
ONE driver's `RecycleBridge` for BOTH the handoff turn (Phase 1) and the takeover turn (Phase 4)
(`recycle.go:408-412`: `bridge.HandoffTurn(designated)` + `bridge.TakeoverTurn(designated)` from the
SAME `bridge`). A cross-harness switch is intrinsically TWO surfaces on one pane:

- the **FROM** driver gates the idle precondition (Phase 0) and authors the handoff turn (Phase 1);
- the **TO** driver authors the takeover turn (Phase 4) and is the surface the relaunched pane runs.

So `switch` is a **new decision core, `runSwitch`**, that resolves `fromDrv` and `toDrv` separately.
It re-uses recycle's fail-closed GATES (idle∧cleared poll, absent→committed→non-trivial durability,
the pane-txn lock, the marker read-back, the generation stamp) but threads two bridges.

### 2.1 The handoff path is COMMAND-supplied and harness-neutral (path-injection — no new capability)

The `RecycleBridge` turn methods are ALREADY path-parametric: `HandoffTurn(designatedPath)` and
`TakeoverTurn(designatedPath)` take the path as an argument (`internal/surface/recycle.go:24-36`), and
`cmdRecycle` already computes `designated := bridge.HandoffPath(cwd, token)` then passes it into both
turns (`recycle.go:408-412`). For recycle the path comes from the ONE driver's `HandoffPath`; for
switch the FROM and TO drivers DISAGREE on `HandoffPath` (claude → `<cwd>/.claude/handoffs/…`, grok →
`<cwd>/.flotilla/handoffs/…` — `claude.go` vs `grok.go` bridges), so neither driver's `HandoffPath`
can be authoritative. **`runSwitch` IGNORES both drivers' `HandoffPath` and supplies its OWN neutral
path** `<project_root>/.flotilla/handoffs/switch-<token>.md` into `fromDrv.HandoffTurn(neutralPath)`
AND `toDrv.TakeoverTurn(neutralPath)`. The absent-at-HEAD baseline
(`deliver.HandoffAbsentAtHead`) and the durability gate (`deliver.HandoffDurable`) operate on this
neutral switch path.

**Decision: NO new `SwitchBridge` capability.** Because the turns are already path-injectable, the
existing `RecycleBridge` is sufficient — `switch` simply calls `HandoffTurn`/`TakeoverTurn` with a
command-chosen path instead of the driver's own. The spec DOCUMENTS this path-injection contract (a
recycle-capable bridge's turns MUST honor a caller-supplied path; they MUST NOT re-derive it from
`HandoffPath` internally — both claude and grok already comply, since they format the argument
verbatim). A surface is a valid switch FROM/TO target iff it implements `RecycleBridge` +
`ComposerStateProbe` (the same recycle-capable bar); a surface lacking either fails closed (§7).

### 2.2 The single-driver `recycle` invariant is PRESERVED — because switch is a separate core

`runRecycle` and `cmdRecycle` are UNTOUCHED. `recycle` still resolves ONE driver and still uses that
driver's OWN `HandoffPath`. The two-surface span lives ENTIRELY in the new `runSwitch`/`cmdSwitch`.
This is the same structural decision the archived grok-recycle design reached (its §3: rejected
"`recycle --to-surface`" precisely because it "breaks recycle's single-driver invariant") — here we
honor it by making switch its own verb rather than overloading recycle.

### 2.3 `runSwitch` lifecycle

```
Phase 0  FROM  idle-gate (Assess==Idle ∧ ComposerState==Cleared) on the pane         [lockless in manual path]
Phase 1  FROM  deliver HandoffTurn(neutralPath); gate on absent→committed→non-trivial ∧ idle∧cleared
   ── acquire pane-txn lock (manual: here; AUTO: BEFORE Phase 1 — see §4) ──
   ── re-verify idle∧cleared AND (auto) LIVE re-probe rate-limit scope under lock (§3 P1-C) ──
Phase 2  FROM  Close (or ErrNoGracefulClose ⇒ handoff-gated respawn-kill); confirm exited
Phase 3  TO    runResume with the TO slot's launch; marker read-back; stamp @flotilla_switch_gen
Phase 3a (eager+durable) last-switch.json: phase="relaunching" + intended TO slot   (BEFORE relaunch)
Phase 3b TO    write active-harness.json overlay (ONLY after successful relaunch + marker confirm)
Phase 3c (eager+durable) last-switch.json: phase="overlay-pending" → "complete"     (AFTER relaunch)
Phase 4  TO    deliver TakeoverTurn(neutralPath) once, while @flotilla_switch_gen still matches
```

The ordering invariant from the source doc holds: `active-harness.json` is written ONLY after a
successful Phase-3 relaunch + marker confirmation; if relaunch succeeds but the overlay write fails,
`last-switch.json` records `phase: "overlay-pending"` and routing falls back to the roster surface
until `--repair` reconciles (§5).

---

## 3. The failover chain, the overlay, and routing precedence

### 3.1 Chain schema (backward-compatible — `internal/launch/launch.go:18-40`)

Add OPTIONAL `primary` + `fallbacks[]` to `launch.Recipe`. Each slot: `surface` (registered driver
name), `launch` (this harness's shell command), **`provider`** (logical provider — `anthropic`,
`xai`, `zai` — LOAD-BEARING for failover target selection), optional `model`, optional
`subscription_id` (a billing/account bucket WITHIN a provider; NOT a secret). `cwd`/`tmux`/`state`
stay recipe-level (the DESK — worktree + pane — is stable; only the foreground process changes).
**Backward-compat rule:** absent `primary`/`fallbacks` ⇒ the flat `launch` IS the primary slot and
`roster.Agent.surface` (or default `claude-code`) is its implied `surface`. Per-slot validation
(load-time, surface-agnostic to avoid an import cycle — mirroring the roster's cmd-layer surface
check): non-empty `launch`, no control chars; `surface` known is re-checked at switch/resume time.

### 3.2 Active overlay + `ResolveHarness` (`internal/workspace/recipe.go:46-66`)

`~/.flotilla/<agent>/active-harness.json` (host-local, atomic write) names the live `slot`
(`"primary"`/`"fallback-N"`), its `surface`/`provider`/`subscription_id`, `switched_at`,
`switch_token`, `reason`, `cooldown_until`, and `poisoned_providers[]`. Absent ⇒ primary.

`ResolveHarness(agent, flat) → (slot, Recipe-for-slot, error)`: (1) resolve the chain via the existing
`ResolveRecipe` precedence (workspace `launch.json` → flat `flotilla-launch.json`); (2) read the
overlay slot name; (3) return that slot's launch + surface. `ResolveActiveRecipe` is the
recipe-shaped view used by `switch`/`resume` relaunch.

### 3.3 Routing precedence — `agentSurface` (`cmd/flotilla/watch.go:986-991`)

`agentSurface` today returns `roster.Agent.surface`. New precedence: **active-overlay surface (if
set) → roster `Agent.surface` → default**. This is the seam that makes `watch`/`send` route to the
LIVE harness after a switch with NO roster commit — the whole reason the overlay exists. A read error
on the overlay is fail-SAFE: fall back to the roster surface (a missing/torn overlay must never make a
live desk unroutable).

### P1-C (TOCTOU, load-bearing) — re-verify idle∧cleared AND the scope under the lock; lock-before-handoff in AUTO

recycle re-verifies its idle∧cleared gate UNDER the pane-txn lock (`recycle.go:160-165`) to close the
post-handoff TOCTOU. `runSwitch` MUST do the same AND, in the AUTO path, ALSO live-re-probe the
rate-limit scope under the lock: a `RateLimited` snapshot taken at detector time is a point-in-time
observation (per `verify-stale-empirical-status-before-propagating`) — by the time the lock is held
the throttle may have cleared, so committing an irreversible switch on a stale "rate-limited" read is
a bug. The under-lock re-probe (a fresh `RateLimitProbe` read) gates the irreversible span; a cleared
probe ABORTS the auto-switch (desk untouched).

**Lock-before-handoff in the AUTO path.** recycle takes the lock only at Phase 2 (the handoff phase is
deliberately lockless to avoid starving operator delivery — `recycle.go:141-155`). That is safe for a
MANUAL, singular recycle. For AUTO-switch, concurrent storm triggers are the NORM (a provider throttle
fires the detector for many desks at once), so two schedulers could each pass the lockless Phase-0/1
for the SAME desk and DOUBLE-deliver a handoff. Therefore in the AUTO path `runSwitch` acquires the
pane-txn lock BEFORE Phase-1 handoff delivery (the manual path keeps recycle's lockless-Phase-1 to
preserve operator-delivery responsiveness, since a manual switch is singular). The detector
additionally DEDUPES switch candidates per-desk — at most ONE in-flight switch per desk — so a storm
does not enqueue a second candidate while the first is mid-switch.

---

## 4. The switch trigger (auto path — P2, gated)

### 4.1 `RateLimitProbe` (OPTIONAL surface capability — P1)

```go
type RateLimitProbe interface {
    RateLimited(pane string) (limited bool, scope RateLimitScope, detail string)
}
type RateLimitScope int
const (
    RateLimitServerSide RateLimitScope = iota // provider-wide infra throttle ⇒ poison the whole `provider`
    RateLimitAccountSide                       // per-account/key ⇒ poison only the `subscription_id`
)
```

READ-ONLY (pane capture / session store). Tonight's Anthropic event was **ServerSide** — every
Claude subscription would have been hit at once, so failover MUST cross to a slot with a DIFFERENT
`provider` (the claude→grok→opencode chain). A second `anthropic` slot would not have helped.

**P2 verdict discipline (folded):** the rate-limit banner MUST be detected in the CURRENT turn/composer
region — NOT anywhere in scrollback (a throttle string scrolled up into history is stale) — and MUST
survive 2 CONSECUTIVE reads before it is treated as material, mirroring `parseBusy`'s working-classifier
discipline (`claude.go:102`) and `confirm.go`'s `clearedConfirmPolls` consecutive-stable reads
(`confirm.go:214`). This prevents a one-frame render glitch from triggering an irreversible switch.

### 4.2 Failover target selection

1. classify scope via the probe; 2. **ServerSide** ⇒ add the slot's `provider` to
`poisoned_providers`; pick the FIRST fallback whose `provider ∉ poisoned_providers`; 3. **AccountSide**
⇒ poison only the `subscription_id`; prefer a same-provider alternate subscription, else fall through
to a different provider as in (2); 4. operator `--to <slot>` overrides selection but still respects
poisoned providers unless `--force`.

### 4.3 Detector integration + storm cooldown (P2)

After `agentSurface` reads the overlay, the detector calls `RateLimitProbe` (when the driver
implements it) for a desk that is Idle/Errored (NOT mid-turn — wait for idle, same discipline as
recycle Phase 0) and NOT `approval_sensitive` (gate #4 — refused at ENQUEUE). On a confirmed scope it
enqueues `flotilla switch <agent> --auto` via SIDE-CHANNEL exec (§8), deduped one-in-flight-per-desk.
Storm state (`~/.flotilla/provider-cooldowns.json`) keys on `provider`+scope: ≥N desks sharing a
`provider` reporting ServerSide within window W ⇒ poison the whole provider fleet-wide. Cooldowns:
30 min/provider (server-side), 15 min/subscription (account-side).

### P1-D (poison terminals, load-bearing) — explicit FAIL-CLOSED ends; `auto_revert` CUT

- **All providers poisoned ⇒ auto-switch REFUSES.** When NO fallback has an un-poisoned provider, the
  auto-switch does NOT fire: the desk STAYS on its current harness and the operator is notified. This
  check runs BEFORE any Phase-1 handoff commit — **never commit a handoff you cannot land a takeover
  for** (a handoff with no viable TO target would strand the desk mid-switch).
- **Max-switches-per-desk-per-hour cap exhausted (3 auto) ⇒ a DEFINED stuck-state + notify.** The desk
  stays put, a LOUD operator notification fires once on the cap-crossing edge, and auto-switch is
  suppressed for that desk until the window rolls or the operator acts. Operator-forced switches are
  uncapped.
- **`auto_revert` is CUT from v1.** The source doc's auto-revert (probe the FROM provider for recovery,
  then switch back) is UNIMPLEMENTABLE as specified: once the FROM harness is gone, there is no pane
  running it to probe, so "the provider-recovery probe has no pane to read" is a contradiction. v1
  revert is operator-only: `flotilla switch <agent> --to primary` (which still respects poisoning
  unless `--force`). This is a genuine source-doc/finding conflict resolved in the finding's favor
  (§9).

---

## 5. P1-B (atomicity / recovery, load-bearing) — eager durable phase records + `--repair`

The overlay write (Phase 3b) happens AFTER the relaunch, but Phase-4 routing/idempotency READ it. The
most-likely crash window is "the pane is already running the TO harness, but the overlay still says
FROM" (relaunch succeeded, overlay write or the process died before it). Recovery:

- **Eager + DURABLE `last-switch.json` phase records at each boundary** — written `fsync`+rename (NOT
  best-effort): BEFORE relaunch, `phase: "relaunching"` + the intended TO slot; AFTER a confirmed
  relaunch, `phase: "overlay-pending"`; after the overlay write, `phase: "complete"`. (recycle's
  `last-recycle.json` is best-effort atomic-rename — `recycle.go:480-524`; switch HARDENS this to
  fsync because it is the recovery ground-truth for the half-switched window.)
- **`flotilla switch <agent> --repair`** reads the LIVE pane's ACTUAL harness — `pane_current_command`
  and/or the harness marker the relaunch stamped — and reconciles `active-harness.json` AUTHORITATIVELY
  to match it. If the pane runs the TO harness but the overlay still says FROM (the
  `overlay-pending`/`relaunching` record), `--repair` writes the TO overlay; if the pane is dead,
  `--repair` reports the half-switch and names `flotilla resume <agent>`. `--repair` reads the live
  pane, NOT the (possibly stale) record — the record points it at what to check; the PANE is the truth
  (per `verify-stale-empirical-status-before-propagating`).

### Half-switched recovery table

| Failure point | Desk state | Recovery |
|---|---|---|
| Phase 1 abort | live, FROM harness | retry; no overlay written |
| Phase 2 abort | live, handoff committed | operator `send` takeover manually, or `recycle` same harness |
| Phase 3 relaunch fail | dead pane, handoff committed | `flotilla resume <agent>` (overlay not yet written) |
| Phase 3b overlay-write fail | live, TO harness, overlay says FROM | `flotilla switch <agent> --repair` (reconciles from the live pane) |
| Phase 4 takeover fail | live, TO harness, empty context | `flotilla send <agent> 'read <handoff> and take over'` |

---

## 6. Idempotency, concurrency, safety

- **Idempotency.** Every attempt mints a `switch_token` (recycle's format — `recycle.go:337-346`:
  timestamp + crypto/rand nonce). `last-switch.json` records `{token, phase, from, to, handoff_path,
  bundle_path, error?}`. Re-running with a completed token ⇒ no-op success. Phase 3 stamps
  `@flotilla_switch_gen` (parallel to `@flotilla_recycle_gen` — `recycle.go:216-229`) so Phase 4
  aborts if a newer switch superseded it.
- **Concurrency.** The pane-txn lock is shared with resume/recycle (`deliver.AcquirePaneTxn` —
  `resume.go:111-117`, `recycle.go:150-155`), so a switch, a recycle, and a resume cannot interleave
  on one pane. (P1-C adds: in the AUTO path, acquire it BEFORE Phase 1.)
- **`approval_sensitive` (gate #4).** No auto-switch — refused at the watch ENQUEUE. Operator paths:
  `flotilla switch <agent> --to <slot> --confirm` (after an explicit ack), or the XO `notify`-proposes
  and the operator confirms.
- **Side-channel arguments (P2).** The auto-switch exec uses an ARGV ARRAY
  (`exec.Command("flotilla", "switch", agent, "--to", slot)`), NEVER a shell string — no shell
  interpolation of an attacker-influenceable token. Before exec, validate `slot ∈
  {primary, fallback-N}` AND `agent ∈ roster`. Status/notices go to a LOG side-channel, never into the
  target pane (per `agent-control-notices-to-side-channel`).

---

## 7. Fail-closed for incapable surfaces

A surface is a valid switch FROM/TO target iff it implements BOTH `RecycleBridge` AND
`ComposerStateProbe` (the recycle-capable bar). Today only `claude-code` and `grok` qualify;
`opencode`/`aider` lack the bridge. A switch whose FROM or TO surface is not recycle-capable REFUSES
cleanly, naming the surface — never a silent context-losing restart (matching `recycle.go:393-402`).
Auto-switch never selects an incapable TO slot.

---

## 8. Continuity — the two layers

| Layer | What | Ships | Corpus needed? |
|---|---|---|---|
| **1 — switch mechanism** | handoff → relaunch → takeover | **P0** | **No** — the handoff markdown is self-contained |
| **2 — constraint-portability seam** | `HarnessContinuityBundle` + bare-string `memex_injection_hint` → corpus query → injection | **P4** (after memex #20/#21) | Yes, once #20 lands |

The bundle WRITE-side path is FROZEN at P0 even though CONSUMPTION is P4: the bundle is written at the
desk-scoped neutral path `<project_root>/.flotilla/switch/<flotilla_agent>/continuity-<token>.json`
(durability-gated, same `HandoffDurable`-class gate as the handoff, before Phase 4) and
`last-switch.json` records its `bundle_path`. flotilla writes a BARE-STRING hint
(`hint_version` parallel to `bundle_version`; unknown version ⇒ memex degrades to mode-only) and
NEVER corpus text or constraint prose (gate #3). `from` is OPTIONAL (gate #1 — fresh launch has none).
A fresh launch or an empty corpus DEGRADES gracefully (the handoff alone preserves context; a corpus
query returning empty is NOT an error). Design rule (load-bearing): do NOT assume the corpus carries
the operator's standing `~/.claude/rules/` constraints — until #20 ingests them, the shelf may be
empty for exactly the constraints the portability headline promises; Layer 1 must not depend on them.

---

## 9. Where the findings CONFLICT with the source doc (resolved in the finding's favor)

1. **`auto_revert`.** Source doc §2.2 keeps an optional `auto_revert` (probe the FROM provider, switch
   back). P1-D CUTS it: once FROM is gone there is no pane to probe — unimplementable as specified. v1
   revert is operator-only `switch --to primary`. **Finding wins.**
2. **switch "mirrors recycle's single-driver core".** Source doc §4.2 frames switch as a recycle
   mirror. P1-A reshapes it to a NEW two-driver `runSwitch` (recycle resolves ONE driver). **Finding
   wins** — and the single-driver recycle invariant is preserved precisely BECAUSE switch is separate.
3. **AUTO-path lock timing.** Source doc §5.4 inherits recycle's "lock at Phase 2". P1-C moves lock
   acquisition BEFORE Phase 1 in the AUTO path (concurrent storm triggers ⇒ double-handoff risk).
   **Finding wins** for the auto path; the manual path keeps recycle's lockless Phase 1.
4. **Desk-binding mtime tiebreaker.** Source doc §3.3 breaks a multi-bundle tie by newest mtime. P2
   REMOVES the mtime tiebreaker in favor of fail-closed-to-operator (a clock-skewed mtime is a poison
   source — cf. `backfill-mtime-clamp-to-now`). **Finding wins.**

---

## 10. Implementation phases

| Phase | Deliverable | Depends on |
|---|---|---|
| **P0** | chain schema + per-slot validation; `active-harness.json` + `ResolveHarness`/`ResolveActiveRecipe`; `agentSurface` overlay precedence; `runSwitch` two-driver core + manual `flotilla switch --to` (operator-only, no auto) + `--repair`; eager-durable `last-switch.json`; the 4 gates; bundle write-side at the frozen path | — |
| **P1** | `RateLimitProbe` on claude (+ grok); current-region + 2-consecutive-read discipline; watch surfaces a `rate-limited` material wake | P0 |
| **P2** | auto-switch for non-`approval_sensitive` desks (lock-before-handoff, under-lock re-probe, per-desk dedupe, poison terminals) + provider storm cooldown | P1 |
| **P3** | `RecycleBridge` for opencode (a non-degraded opencode TO/FROM target) | opencode close/live-verify |
| **P4** | Layer 2: memex PR #21 consumer + corpus ingest (#20); the bare-string hint is already defined | memex-side; does NOT block P0–P2 |
| **P5** | cursor + codex slots when those drivers register | driver shipping |

---

## 11. References (code seams — verified this session)

| Topic | Path |
|---|---|
| `runRecycle` resolves ONE driver; plan carries precomputed turn texts | `cmd/flotilla/recycle.go:90-239, 389, 408-416` |
| `RecycleBridge` turns are path-parametric (`HandoffTurn(designatedPath)`) | `internal/surface/recycle.go:17-37` |
| recycle token (timestamp + crypto/rand nonce) | `cmd/flotilla/recycle.go:337-346` |
| `last-recycle.json` atomic-rename status record (switch HARDENS to fsync) | `cmd/flotilla/recycle.go:480-524` |
| under-lock re-verify of the idle∧cleared gate | `cmd/flotilla/recycle.go:150-165` |
| Launch `Recipe` (chain extends this) | `internal/launch/launch.go:18-40` |
| Workspace recipe precedence (workspace → flat) | `internal/workspace/recipe.go:46-66` |
| `agentSurface` (overlay precedence goes here) | `cmd/flotilla/watch.go:986-991` |
| `parseBusy` working-classifier (RateLimitProbe mirrors its current-region discipline) | `internal/surface/claude.go:102` |
| `clearedConfirmPolls` consecutive-stable reads | `internal/surface/confirm.go:214` |
| `RateLimitProbe` does NOT exist today (Discord/GitHub rate limits are unrelated) | `internal/discord/provision.go`, `internal/dash/tracker/gh.go` (§2.3) |
| Roster `surface` / `approval_sensitive` | `internal/roster/roster.go:25-44` |
| Cross-harness migration precedent (fork-3) | `openspec/changes/archive/2026-06-23-recycle-cross-harness-grok/design.md` §3C |
