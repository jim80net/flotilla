# switch Specification (delta)

## ADDED Requirements

### Requirement: `flotilla switch` hands a desk from a FROM harness to a TO harness as a TWO-driver core, preserving the single-driver recycle invariant

The system SHALL provide `flotilla switch <agent> --to <slot>` — a single operation that hands a
desk's running pane from its current (FROM) harness to a declared fallback (TO) harness, preserving
the desk's in-flight context via the FROM harness's handoff and the TO harness's takeover. Because a
cross-harness handover is intrinsically TWO surfaces on one pane, `switch` SHALL be a NEW decision
core (`runSwitch`) that resolves the FROM driver and the TO driver SEPARATELY — the FROM driver gates
the idle precondition and authors the handoff turn; the TO driver authors the takeover turn and is the
surface the relaunched pane runs. `switch` SHALL NOT be implemented by overloading `flotilla recycle`
(which resolves exactly ONE driver and uses that one driver's bridge for both handoff and takeover);
the single-driver `recycle` core SHALL remain UNTOUCHED, so the single-driver recycle invariant is
preserved BECAUSE switch is a separate verb. A surface is a valid FROM or TO target ONLY when it
implements BOTH `RecycleBridge` AND `ComposerStateProbe` (the recycle-capable bar); a switch whose
FROM or TO surface lacks either SHALL REFUSE cleanly, naming the surface, never a silent context-
losing restart.

#### Scenario: A cross-harness switch hands off and takes over across two drivers

- **WHEN** `flotilla switch <agent> --to <slot>` runs on a recycle-capable FROM surface to a
  recycle-capable TO slot
- **THEN** the FROM driver gates the idle precondition and authors the handoff turn, the desk relaunches
  on the TO slot's launch recipe, the TO driver authors the takeover turn, and the desk continues its
  chapter via the handoff

#### Scenario: switch does not alter the single-driver recycle core

- **WHEN** `flotilla recycle <desk>` runs after this change ships
- **THEN** recycle still resolves exactly ONE driver and uses that driver's own `HandoffPath`/bridge
  for both handoff and takeover, byte-unchanged — the two-driver span lives only in `runSwitch`

#### Scenario: A switch to or from a non-recycle-capable surface refuses

- **WHEN** the FROM or TO surface of a switch does not implement both `RecycleBridge` and
  `ComposerStateProbe` (e.g. opencode/aider today)
- **THEN** the command refuses cleanly, naming the incapable surface, rather than restarting the desk
  with its context lost

### Requirement: The handoff path is command-supplied and harness-neutral

`runSwitch` SHALL supply a SINGLE harness-neutral handoff path
`<project_root>/.flotilla/handoffs/switch-<token>.md` into BOTH the FROM driver's `HandoffTurn` and
the TO driver's `TakeoverTurn`, OVERRIDING each driver's own `HandoffPath` convention (claude's is
`.claude/handoffs/…`, grok's is `.flotilla/handoffs/…` — they differ, so neutrality REQUIRES the
override). The bridge turn methods are already path-parametric (`HandoffTurn(designatedPath)` /
`TakeoverTurn(designatedPath)`), so this requires NO new capability: `switch` calls them with a
command-chosen path instead of the driver's own. A recycle-capable bridge's turn methods SHALL honor
the caller-supplied path verbatim and SHALL NOT re-derive the path from `HandoffPath` internally. The
absent-at-HEAD baseline and the durability gate SHALL operate on this neutral switch path, and
`<project_root>` SHALL be the recipe `cwd` realpath'd (so the path resolves under the git work-tree
the durability check computes against).

#### Scenario: One neutral path is threaded into both turns

- **WHEN** `runSwitch` computes the handoff path for a switch token
- **THEN** the path is `<project_root>/.flotilla/handoffs/switch-<token>.md`, the FROM `HandoffTurn`
  and the TO `TakeoverTurn` both name that exact path, and neither driver's own `HandoffPath`
  convention is used

#### Scenario: The durability gate operates on the switch path

- **WHEN** the FROM handoff turn writes and commits the handoff
- **THEN** the absent→committed→non-trivial durability gate is evaluated against
  `<project_root>/.flotilla/handoffs/switch-<token>.md`, not a driver-branded path

### Requirement: The four pinned switch gates

`flotilla switch` and its auto-switch path SHALL honor four NON-NEGOTIABLE gates: (1) a fresh launch
of a desk on its TO harness with NO from-harness, NO continuity bundle, and NO (or empty) corpus SHALL
still produce a productive desk (its harness identity files + workspace `state.md`) — requiring
`from`, a bundle, a hint, or a populated corpus before a desk can run is FORBIDDEN; (2) the handoff
and continuity-bundle paths SHALL be product-owned and harness-neutral, never a claude- or
grok-branded directory; (3) flotilla SHALL carry only a BARE-STRING pointer/hint in the continuity
bundle and SHALL NEVER embed memex-retrieved corpus text or operator-constraint prose in any artifact
it writes; (4) an `approval_sensitive` desk SHALL NEVER auto-switch — it is switched ONLY by an
operator `--confirm`, and the refusal SHALL be enforced at the watch ENQUEUE (the candidate is never
enqueued), not merely at exec.

#### Scenario: A fresh desk launches with nothing carried over

- **WHEN** a desk cold-starts on its TO harness with no prior switch, no bundle, and an empty corpus
- **THEN** it is productive via its harness identity files and workspace state — no `from`, bundle,
  hint, or populated corpus is required

#### Scenario: An approval-sensitive desk is never auto-switched

- **WHEN** the auto-switch detector evaluates an `approval_sensitive` desk that reports a provider
  throttle
- **THEN** no switch candidate is enqueued for it (the refusal is at enqueue), and the only path that
  switches it is an operator `flotilla switch <agent> --to <slot> --confirm`

#### Scenario: flotilla never writes corpus text or constraint prose

- **WHEN** flotilla writes a continuity bundle for a switch
- **THEN** the bundle carries only a bare-string `memex_injection_hint` pointer and never any
  memex-retrieved corpus text or operator-constraint prose

### Requirement: The irreversible span is serialized under the pane lock and re-verifies idle and the throttle scope

`runSwitch` SHALL acquire the per-pane transaction lock shared with `resume`/`recycle` for the
irreversible span (close → relaunch → takeover) and SHALL RE-VERIFY the idle∧cleared gate under it
before closing (closing the post-handoff TOCTOU, as recycle does). In the AUTO (rate-limit-triggered)
path it SHALL ADDITIONALLY acquire the lock BEFORE delivering the Phase-1 handoff (not only at the
close), because concurrent storm triggers make double-handoff by two schedulers the norm; and it SHALL
LIVE-RE-PROBE the rate-limit scope under the lock — a `RateLimited` snapshot taken at detector time is
a point-in-time observation, so a now-cleared probe SHALL ABORT the auto-switch (desk untouched)
rather than committing an irreversible switch on a stale read. The MANUAL path MAY keep recycle's
lockless Phase-1 handoff (a manual switch is singular, so operator-delivery responsiveness is
preserved).

#### Scenario: A turn that starts before the lock is caught by the under-lock re-verify

- **WHEN** the handoff gate passes while unlocked and the desk starts a turn before the switch acquires
  the lock
- **THEN** the under-lock re-verify reads the desk as not idle-and-cleared and the switch ABORTS rather
  than closing a mid-turn desk

#### Scenario: An auto-switch whose throttle cleared aborts under the lock

- **WHEN** an auto-switch acquires the lock and the under-lock re-probe finds the provider throttle has
  cleared
- **THEN** the auto-switch ABORTS (the desk stays on its current harness), never committing the
  irreversible switch on the stale detector-time snapshot

#### Scenario: An auto-switch acquires the lock before the handoff

- **WHEN** two storm-triggered schedulers race to auto-switch the same desk
- **THEN** the first acquires the pane lock before delivering the handoff and the second's acquire
  fails (or the per-desk in-flight dedupe rejects it), so the handoff is never double-delivered

### Requirement: The active overlay is written only after a confirmed relaunch, with eager durable phase records and a repair path

`runSwitch` SHALL write `~/.flotilla/<agent>/active-harness.json` ONLY after a successful Phase-3
relaunch AND marker confirmation; if the relaunch succeeds but the overlay write fails, the desk is
left running the TO harness with the overlay still naming FROM. To make this half-switched window
recoverable, `runSwitch` SHALL write `last-switch.json` phase records EAGERLY and DURABLY (fsync +
rename, not best-effort) at each boundary — `phase: "relaunching"` with the intended TO slot BEFORE
the relaunch, `phase: "overlay-pending"` after a confirmed relaunch, `phase: "complete"` after the
overlay write. The system SHALL provide `flotilla switch <agent> --repair` that reads the LIVE pane's
ACTUAL harness (`pane_current_command` and/or the stamped marker) and reconciles `active-harness.json`
AUTHORITATIVELY to match the live pane — consulting the durable record only to know what to check, and
treating the live PANE as the truth. When the pane is dead, `--repair` SHALL report the half-switch
and name `flotilla resume <agent>` rather than guessing.

#### Scenario: A failed overlay write after a good relaunch is repaired from the live pane

- **WHEN** a switch relaunches the TO harness successfully but the `active-harness.json` write fails
  (the record reads `overlay-pending`)
- **THEN** `flotilla switch <agent> --repair` reads the live pane running the TO harness and writes the
  TO overlay, reconciling routing — it does not trust the stale FROM overlay

#### Scenario: Phase records are durable across a crash

- **WHEN** the process crashes between the relaunch and the overlay write
- **THEN** the eager fsync+rename `last-switch.json` record (`relaunching`/`overlay-pending`) survives,
  so `--repair` (or the operator) can resolve the half-switched desk

### Requirement: Switch is idempotent and minted with a unique token

Every switch attempt SHALL mint a unique `switch_token` (a sortable timestamp + a crypto/rand nonce,
the recycle token format) and record `{token, phase, from, to, handoff_path, bundle_path, error?}` in
`last-switch.json`. Re-running `switch` with the same already-completed token SHALL be a no-op success.
Phase 3 SHALL stamp a `@flotilla_switch_gen` marker on the pane (parallel to `@flotilla_recycle_gen`)
so Phase 4 ABORTS its takeover if a newer switch superseded it.

#### Scenario: A completed switch re-run is a no-op

- **WHEN** `switch` is re-run with a token whose `last-switch.json` records `phase: "complete"`
- **THEN** it reports a no-op success and does not re-handoff, re-relaunch, or re-takeover

#### Scenario: A superseded takeover aborts

- **WHEN** a newer switch stamps a different `@flotilla_switch_gen` before this switch's Phase-4
  takeover
- **THEN** this switch's takeover aborts (it does not deliver a stale takeover into a re-switched pane)

### Requirement: The auto-switch path fails closed at its terminals

The auto-switch path SHALL define explicit FAIL-CLOSED terminals. When NO fallback slot has a provider
that is not poisoned (all providers poisoned), auto-switch SHALL REFUSE: the desk STAYS on its current
harness and the operator is notified, and this check SHALL run BEFORE any Phase-1 handoff is committed
(never commit a handoff for which no takeover can be landed). When the max-switches-per-desk-per-hour
cap (default 3 auto; operator-forced uncapped) is exhausted, the system SHALL enter a DEFINED
stuck-state — the desk stays put and a LOUD operator notification fires ONCE on the cap-crossing edge —
and SHALL suppress further auto-switches for that desk until the window rolls or the operator acts.
v1 SHALL NOT auto-revert (the FROM harness is gone once switched, so its provider cannot be probed from
a live pane); reverting is operator-only via `flotilla switch <agent> --to primary`.

#### Scenario: All providers poisoned refuses before any handoff

- **WHEN** an auto-switch is considered but every fallback's provider is poisoned
- **THEN** no handoff is committed, the desk stays on its current harness, and the operator is notified

#### Scenario: Cap exhaustion is a defined stuck-state, notified once

- **WHEN** a desk reaches the max-switches-per-hour cap
- **THEN** auto-switch is suppressed for that desk, the desk stays put, and exactly one loud operator
  notification fires on the cap-crossing edge (not every tick)

### Requirement: The continuity bundle write-side is frozen at P0; consumption is deferred

`runSwitch` SHALL write a continuity bundle at the desk-scoped harness-neutral path
`<project_root>/.flotilla/switch/<flotilla_agent>/continuity-<switch_token>.json`, durability-gated by
the same `HandoffDurable`-class gate as the handoff before Phase-4 takeover, and SHALL record its
`bundle_path` in `last-switch.json`. The bundle SHALL carry a `bundle_version`, a `hint_version`, an
OPTIONAL `from` (null on a fresh launch), a `to`, the `flotilla_agent` (desk binding), and a
BARE-STRING `memex_injection_hint` — never corpus text or constraint prose. Bundle CONSUMPTION (a
memex reader querying the corpus from the hint) is DEFERRED (Layer 2, gated on memex #20/#21) and SHALL
NOT block the switch: a switch SHALL succeed via the handoff alone even when memex is offline or the
corpus is empty, and an empty/partial corpus query SHALL be a valid non-error outcome.

#### Scenario: The bundle is written but switch succeeds without consumption

- **WHEN** a switch completes and no memex consumer is present (or the corpus is empty)
- **THEN** the bundle is written at the desk-scoped neutral path and recorded in `last-switch.json`, and
  the desk continues via the handoff alone — the absent consumption is not an error

#### Scenario: A fresh-launch bundle has no `from`

- **WHEN** a bundle is written for a fresh launch with no FROM harness
- **THEN** its `from` is null (or the field is absent) and the switch/launch still succeeds
