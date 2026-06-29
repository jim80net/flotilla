# Tasks — harness / subscription switching (TDD, phase-ordered)

Load-bearing properties (assert across paths — the 4 pinned gates + the P1 findings):
- **(GATE-1) fresh-launch fallback works with NOTHING** — a cold resume on the TO harness with no
  from-harness/bundle/corpus produces a productive desk.
- **(GATE-2) harness-neutral paths** — handoff `<project_root>/.flotilla/handoffs/switch-<token>.md`
  and bundle `<project_root>/.flotilla/switch/<flotilla_agent>/continuity-<token>.json`, never a
  driver-branded dir.
- **(GATE-3) pointer-not-content** — flotilla writes only a bare-string `memex_injection_hint`, never
  corpus text or constraint prose.
- **(GATE-4) approval_sensitive NEVER auto-switches** — refused at the watch ENQUEUE
  (`internal/roster/roster.go:39-44`).
- **(P1-A) two-driver core** — `runSwitch` resolves FROM and TO drivers separately; `runRecycle`
  (`cmd/flotilla/recycle.go:90-239, 389`) is UNTOUCHED (single-driver invariant preserved).
- **(P1-B) eager durable recovery** — `last-switch.json` fsync+rename phase records; `--repair` reads
  the live pane.
- **(P1-C) TOCTOU** — under-lock re-verify of idle∧cleared AND a live scope re-probe; AUTO path locks
  BEFORE Phase-1 handoff; one-in-flight-switch-per-desk dedupe.
- **(P1-D) fail-closed terminals** — all-poisoned ⇒ refuse before any handoff; cap-exhausted ⇒
  stuck-state + notify; NO `auto_revert`.

---

## P0 — schema, overlay, resolution (no auto)

### 1. Failover chain schema + per-slot validation
- [ ] 1.1 TEST FIRST (`internal/launch/launch_test.go`): a recipe with no `primary`/`fallbacks` resolves
  the flat `launch` as the primary slot (backward-compat); a slot with empty `launch` or a `\t`/`\n`/`\r`
  is rejected; a chain preserves each slot's `provider` distinct from `subscription_id`.
- [ ] 1.2 Implement the `primary`/`fallbacks[]` slot fields on `launch.Recipe`
  (`internal/launch/launch.go:18-40`) + per-slot `ValidateRecipe` checks (non-empty `launch`, no control
  chars; surface known-driver check DEFERRED to switch/resume time to avoid an import cycle, mirroring
  the roster cmd-layer validation).

### 2. active-harness.json overlay + ResolveHarness precedence
- [ ] 2.1 TEST FIRST (`internal/workspace/recipe_test.go`): absent overlay ⇒ primary; a present overlay
  naming `fallback-0` resolves that slot's launch+surface; a torn/unreadable overlay falls back to
  primary (fail-safe).
- [ ] 2.2 Implement `active-harness.json` read/atomic-write + `ResolveHarness(agent, flat)` /
  `ResolveActiveRecipe` layered on the existing `ResolveRecipe` precedence
  (`internal/workspace/recipe.go:46-66`).
- [ ] 2.3 TEST FIRST (`cmd/flotilla/watch_test.go`): `agentSurface` returns the overlay surface when set,
  else the roster surface, else default; a torn overlay falls back to the roster surface.
- [ ] 2.4 Implement the overlay-first precedence in `agentSurface` (`cmd/flotilla/watch.go:986-991`).

### 3. runSwitch — the two-driver decision core (P1-A/B/C/D safety)
- [ ] 3.1 TEST FIRST (`cmd/flotilla/switch_test.go`): `runSwitch` (injected ops, à la `recycleOps`)
  resolves FROM and TO drivers separately; the SAME neutral path
  `<project_root>/.flotilla/handoffs/switch-<token>.md` is passed into `fromDrv.HandoffTurn` AND
  `toDrv.TakeoverTurn` (NOT either driver's own `HandoffPath`) — GATE-2.
- [ ] 3.2 TEST: a FROM or TO surface lacking `RecycleBridge`+`ComposerStateProbe` REFUSES cleanly
  (mirror `recycle.go:393-402`); the absent-at-HEAD baseline + durability gate run on the switch path.
- [ ] 3.3 TEST (P1-C): the under-lock re-verify reads not-idle ⇒ ABORT (mirror `recycle.go:160-165`); in
  the AUTO path a now-cleared under-lock re-probe ⇒ ABORT.
- [ ] 3.4 TEST (P1-B): `last-switch.json` is written fsync+rename at each boundary
  (`relaunching`→`overlay-pending`→`complete`); the overlay is written ONLY after a confirmed relaunch +
  marker read-back (`recycle.go:209-218`).
- [ ] 3.5 Implement `runSwitch` + the phased lifecycle (Phase 0 FROM idle-gate, Phase 1 FROM handoff,
  lock, re-verify, Phase 2 FROM close/respawn-kill, Phase 3 TO relaunch via `runResume`
  (`cmd/flotilla/resume.go:145-219`) + `@flotilla_switch_gen` stamp, Phase 3b overlay write, Phase 4 TO
  takeover). Reuse `deliver.AcquirePaneTxn` (`recycle.go:150-155`) + the recycle token
  (`recycle.go:337-346`). Assert `runRecycle` is unchanged (P1-A).
- [ ] 3.6 Implement the continuity-bundle WRITE-side at the frozen desk-scoped neutral path
  (durability-gated like the handoff); bare-string `memex_injection_hint` only (GATE-3); `from` optional
  (GATE-1). NO consumer (Layer 2 / P4).

### 4. Manual `flotilla switch --to` (operator-only) + `--repair`
- [ ] 4.1 TEST FIRST (`cmd/flotilla/switch_test.go`): `parseSwitchArgs` accepts `<agent> --to <slot>`
  (and `--to <surface>` resolving to the first matching fallback, else error), `--confirm`, `--repair`,
  `--force` (à la `parseRecycleArgs`, `recycle.go:529`).
- [ ] 4.2 TEST (P1-B `--repair`): `--repair` reads the LIVE pane's harness (`pane_current_command` /
  stamped marker) and reconciles `active-harness.json` to match; a dead pane ⇒ report + name
  `flotilla resume <agent>`.
- [ ] 4.3 Implement `cmdSwitch` (wire real ops, manual path, no auto) + `--repair`; register the `switch`
  subcommand. Manual path keeps recycle's lockless Phase-1 (singular operator switch).
- [ ] 4.4 TEST (GATE-4 manual): `--to … --confirm` on an `approval_sensitive` desk succeeds; without
  `--confirm` it refuses with the ack instruction.

### 5. Provider-poison SELECTION unit tests (pure, no I/O)
- [ ] 5.1 TEST: server-side `anthropic` poison ⇒ target selection picks the first fallback whose
  `provider ∉ poisoned` = `grok`, NEVER an `anthropic-personal` slot (different `subscription_id`, same
  poisoned provider).
- [ ] 5.2 TEST: account-side poison ⇒ a same-provider alternate `subscription_id` is preferred before
  crossing providers.
- [ ] 5.3 TEST (P1-D): ALL providers poisoned ⇒ selection returns "refuse" (no viable TO) BEFORE any
  handoff is committed; the desk stays put + operator notified.
- [ ] 5.4 TEST (P1-D): cap-exhausted (3 auto/hour) ⇒ a defined stuck-state + a single cap-edge notify;
  operator-forced switches are uncapped.
- [ ] 5.5 Implement the pure `selectFailoverTarget(chain, poisoned, scope)` + the cap/poison bookkeeping.

### 6. Idempotency + recovery tests
- [ ] 6.1 TEST: re-running `switch` with a `phase:"complete"` token ⇒ no-op success; a superseding
  `@flotilla_switch_gen` ⇒ Phase-4 takeover aborts (mirror `recycle.go:224-230`).
- [ ] 6.2 TEST: each half-switch recovery row (design §5 table) — Phase-3b overlay-write-fail ⇒
  `--repair`; Phase-4 takeover-fail ⇒ the documented `flotilla send` escape hatch.
- [ ] 6.3 `openspec validate harness-subscription-switching --strict` green; `go build ./...` +
  `go test ./internal/launch/... ./internal/workspace/... ./cmd/flotilla/...` green.

---

## P1 — RateLimitProbe (claude + grok)

### 7. RateLimitProbe SPI + claude/grok classifiers
- [ ] 7.1 TEST FIRST (`internal/surface/claude_test.go`): a captured `Server is temporarily limiting
  requests` in the CURRENT turn region across 2 consecutive reads ⇒ `RateLimited`=true,
  scope=`ServerSide`; the same string only in scrollback ⇒ not material; a one-frame glitch ⇒ not
  material (mirror `parseBusy` `claude.go:102` + `clearedConfirmPolls` `confirm.go:214`).
- [ ] 7.2 Implement the OPTIONAL `RateLimitProbe` + `RateLimitScope` in `internal/surface/surface.go`
  (sibling of `RecycleSupport`) and the claude classifier; assert `var _ RateLimitProbe = claudeCode{}`.
- [ ] 7.3 Implement the grok classifier (live-captured phrases only — never fabricated, per the grok
  driver's wrong-product history `internal/surface/grok.go:18-26`); a starting `Rate limit exceeded`
  phrase, scope live-characterized.
- [ ] 7.4 Wire a `rate-limited` material wake into the change-detector's transition set (surface it to
  the XO; do NOT auto-switch yet — P2).

---

## P2 — auto-switch + storm cooldown (non-approval-sensitive only)

### 8. Detector auto-switch integration
- [ ] 8.1 TEST FIRST (`internal/watch/detector_test.go` / `cmd/flotilla/watch_test.go`): a throttled
  idle non-sensitive desk enqueues exactly ONE `flotilla switch --auto` candidate (argv-array exec, slot
  + agent validated); a second is rejected while in flight (one-in-flight dedupe, P1-C); a mid-turn desk
  waits; an `approval_sensitive` desk is refused at ENQUEUE (GATE-4).
- [ ] 8.2 Implement the detector hook (after `agentSurface` reads the overlay): probe → scope → enqueue
  via side-channel argv exec; per-desk in-flight dedupe; status to the log side-channel only
  (`agent-control-notices-to-side-channel`).
- [ ] 8.3 TEST + implement the AUTO-path lock-BEFORE-handoff and the under-lock live re-probe in
  `runSwitch` (P1-C).

### 9. Provider storm cooldown
- [ ] 9.1 TEST FIRST: `~/.flotilla/provider-cooldowns.json` — N server-side reports on one `provider`
  within W ⇒ poison the whole provider fleet-wide (30 min); account-side ⇒ poison only the
  `subscription_id` (15 min); provider lookup precedes subscription lookup.
- [ ] 9.2 Implement the storm-state read/write + the provider-first cooldown lookup; auto-switch
  consults `poisoned_providers` (no auto-revert — P1-D).

---

## P3+ — deferred (do NOT block P0–P2)

### 10. opencode RecycleBridge (P3)
- [ ] 10.1 opencode `RecycleBridge` + `ComposerStateProbe` so opencode is a non-degraded switch TO/FROM
  target (live-verify opencode's close + composer render first).

### 11. memex Layer-2 consumer (P4 — gated on memex #20/#21)
- [ ] 11.1 (memex side) consume the bundle's bare-string `memex_injection_hint` → corpus query →
  injection; flotilla's write-side is already frozen at P0. Does NOT block P0–P2.

### 12. cursor + codex slots (P5)
- [ ] 12.1 cursor/codex failover slots when those drivers register.

---

## Review + ship
- [ ] 13.1 Implementation-trio (systems-review + open-code-review + STORM, parallel) on the P0 diff;
  iterate clean. Verify GATE-1..4 + P1-A..D each have a test.
- [ ] 13.2 `openspec validate --all --strict` green; PR referencing this change. Record the deferred
  follow-ups (opencode bridge, memex Layer-2, cursor/codex) as issues.
