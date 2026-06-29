# Proposal — harness / subscription switching (fail a desk over to a different harness on a provider throttle)

## Why

A server-side Anthropic rate limit (`Server is temporarily limiting requests`, operator 2026-06-29)
throttled EVERY Claude-based desk at once and stalled the fleet. Today most desks share one harness
(Claude Code) and one provider (Anthropic), so a single provider-wide throttle is a fleet-wide
outage — and flotilla has no way to fail a desk over to a DIFFERENT harness/subscription while
preserving its in-flight context.

flotilla already has every primitive needed to do this — it just lacks the orchestration:

- per-agent `surface` in the roster (`internal/roster/roster.go:25-44`) + a registered `Driver` SPI
  per harness (`internal/surface/surface.go:166`),
- arbitrary host-local launch recipes (`internal/launch/launch.go:18-40`) resolved workspace-first
  (`internal/workspace/recipe.go:46-66`),
- a context-preserving same-harness `flotilla recycle` whose `RecycleBridge`
  (`internal/surface/recycle.go:17-37`) ALREADY takes a command-supplied handoff path
  (`HandoffTurn(designatedPath)` / `TakeoverTurn(designatedPath)`), and a fail-closed phased core
  (`cmd/flotilla/recycle.go:90-239`),
- the cross-harness migration pattern ratified in the archived grok-recycle change
  (`openspec/changes/archive/2026-06-23-recycle-cross-harness-grok/design.md` §3C: portable-markdown
  handoff + recipe flip + resume + takeover).

What is missing is (1) a declared per-desk failover CHAIN (primary + ordered fallbacks, each with a
`provider` identity), (2) a runtime ACTIVE-slot overlay so `watch`/`send` route to the live harness
WITHOUT a mid-incident roster commit, (3) a NEW `flotilla switch` command that orchestrates a
TWO-driver cross-harness handover (FROM-driver handoff → relaunch on the TO recipe → TO-driver
takeover) with the SAME fail-closed safety as recycle, and (4 — gated, phase 1+) an optional
provider rate-limit probe + a non-`approval_sensitive` auto-switch detector.

This change is an UPGRADE that EXTENDS recycle/resume/launch/surface — it does NOT replace them and
it does NOT touch the single-driver `recycle` invariant (see design §2 / the P1-A reshape).

## What changes

1. **Per-desk failover chain (`launch.Recipe` extension, backward-compatible).** Add optional
   `primary` + ordered `fallbacks[]` harness slots, each carrying a `surface`, a `launch` command, a
   **`provider`** identity (`anthropic`/`xai`/`zai`, distinct from an optional `subscription_id`), and
   optional `model`. When `primary`/`fallbacks` are ABSENT, the existing flat `launch` IS the primary
   slot — every current recipe keeps working unchanged.

2. **Runtime active-harness overlay.** A host-local `~/.flotilla/<agent>/active-harness.json` names
   the live slot. `ResolveHarness(agent)` resolves the chain (workspace → flat, the existing
   precedence) then applies the overlay; `agentSurface` (`cmd/flotilla/watch.go:986`) reads the
   overlay surface BEFORE the roster surface — so a switch re-routes `watch`/`send` with no roster
   commit. Absent overlay ⇒ primary (today's behavior, byte-identical).

3. **A NEW `flotilla switch` command — a TWO-driver decision core (`runSwitch`), NOT a recycle
   mirror.** `runRecycle` resolves exactly ONE driver and uses its bridge for BOTH handoff and
   takeover; a cross-harness switch needs a **FROM** driver (idle-gate + handoff) AND a **TO** driver
   (takeover) on the SAME pane. The command supplies a single **harness-neutral** handoff path
   (`<project_root>/.flotilla/handoffs/switch-<token>.md`) into BOTH `HandoffTurn(FROM)` and
   `TakeoverTurn(TO)` — overriding each driver's own `HandoffPath` (claude's is `.claude/handoffs/`,
   grok's is `.flotilla/handoffs/` — they DIFFER, so neutrality REQUIRES the override). The absent-at-
   HEAD baseline + durability gates operate on the switch path. P0 ships the operator-only manual path
   (`flotilla switch <agent> --to <slot>`); no auto.

4. **(P1+, gated) optional `RateLimitProbe` + auto-switch.** An OPTIONAL driver capability reports
   whether the pane's current turn hit a provider throttle, classified `ServerSide` (poison the whole
   `provider`) vs `AccountSide` (poison only the `subscription_id`). The watch detector may enqueue a
   switch candidate for a NON-`approval_sensitive`, idle desk; storm cooldowns key on provider.

5. **The 4 pinned safety gates (NON-NEGOTIABLE).** (1) a fresh launch works with NO from-harness,
   bundle, or corpus; (2) the handoff/bundle paths are harness-neutral; (3) flotilla carries a
   POINTER, never corpus text or operator-constraint prose; (4) an `approval_sensitive` desk NEVER
   auto-switches (operator `--confirm` only, refused at the watch ENQUEUE, not just at exec).

6. **Layer split (continuity).** Layer 1 — the switch MECHANISM (handoff → relaunch → takeover) — is
   corpus-INDEPENDENT and ships P0. Layer 2 — the `HarnessContinuityBundle` + a bare-string
   `memex_injection_hint` consumed by memex (PR #21) against the shared corpus (#20) — is a write-side
   seam frozen at P0 (bundle WRITTEN at the desk-scoped pinned path) but CONSUMED only at P4; it does
   NOT block P0–P2.

## Impact

- **Affected specs:**
  - `switch` (**NEW capability**) — the `flotilla switch` command, the `runSwitch` two-driver
    lifecycle, idempotency/recovery (`--repair`), and the 4 pinned gates.
  - `workspace` (ADDED) — the `primary`/`fallbacks[]` chain on the recipe, per-slot validation,
    `active-harness.json` overlay, and `ResolveHarness`/`ResolveActiveRecipe` precedence.
  - `surface` (ADDED) — the OPTIONAL `RateLimitProbe` + `RateLimitScope`; the handoff-path-injection
    contract (the bridge turns are path-parametric, so a command can override the per-driver path).
  - `watch` (ADDED) — the auto-switch detector integration + provider storm cooldown, GATED behind a
    probe and restricted to non-`approval_sensitive` desks.
- **Affected code (by phase):** `internal/launch/launch.go` (+chain schema + per-slot validation);
  `internal/workspace/` (+`active-harness.json` + `ResolveHarness`); `cmd/flotilla/switch.go` (NEW —
  `runSwitch` + `cmdSwitch` + `--to`/`--repair`); `cmd/flotilla/watch.go` (`agentSurface` overlay
  read; P2 detector hook); `internal/surface/{claude,grok}.go` (P1 `RateLimitProbe`).
- **No behavior change** to `recycle`, `resume`, or any existing driver on the non-switch path: the
  chain schema is additive (absent ⇒ flat-launch-as-primary), the overlay is absent-by-default
  (⇒ roster surface), and `switch` is a NEW command (it composes, never edits, the existing cores).

## Trio findings folded (systems-review + open-code-review + STORM — design gate)

The design (`docs/harness-subscription-switching.md`) cleared the trio; the load-bearing refinements
are folded into `design.md` and REFINE the source doc (where they conflict, the findings win):

- **P1-A (reshape):** `switch` is a NEW two-driver `runSwitch`, NOT a single-driver-recycle mirror;
  the single-driver `recycle` invariant is preserved BECAUSE switch is a separate core (design §2).
- **P1-B (atomicity/recovery):** eager+durable `last-switch.json` phase records (fsync+rename) at each
  boundary; `flotilla switch --repair` reconciles the overlay from the LIVE pane's actual harness
  (design §5).
- **P1-C (TOCTOU):** re-verify idle∧cleared AND a LIVE re-probe of the rate-limit scope under the
  pane-txn lock; in the AUTO path acquire the lock BEFORE Phase-1 handoff delivery; the detector
  dedupes one in-flight switch per desk (design §4/§6).
- **P1-D (poison terminals):** explicit FAIL-CLOSED terminals — all-providers-poisoned ⇒ REFUSE
  before any handoff commit; cap-exhausted ⇒ defined stuck-state + notify. `auto_revert` is CUT from
  v1 (unimplementable as specified — no pane to probe once FROM is gone); v1 revert is operator-only
  `switch --to primary` (design §6/§7).

## Not in

- memex consumer implementation + corpus ingest (#20/#21) — Layer 2, P4; does NOT block P0–P2.
- `auto_revert` — CUT from v1 (P1-D); revert is operator-only `switch --to primary`.
- The §3.3 mtime tiebreaker for desk-binding — REMOVED in favor of fail-closed-to-operator (P2).
- opencode/aider `RecycleBridge` (no bridge today ⇒ not a switch TO/FROM target — fail-closed) — P3.
- cursor/codex slots — P5, when those drivers register.
- Editing the committable roster on every switch (overlay only); credential/secret rotation; in-harness model routing (operator encodes the model in the slot `launch`).
