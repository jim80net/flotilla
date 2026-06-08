# Design: heartbeat v2 — change-detector (materiality-gated XO waking)

## Problem

Today `flotilla watch` wakes the XO every `heartbeat_interval` (idle-gated) with a
generic "do your duties" prompt. The XO burns context on every tick even when
nothing changed — the original Ralph-loop pain. v2 replaces the always-wake
heartbeat with a **cheap, pure-Go change detector** that wakes the XO **only on a
material change**, and rotates the XO's context after each settled handling (via
the `surface.RotateContext` guard shipped in Phase 1). An idle fleet costs
**$0/tick**; the XO's context is never churned for no reason.

## Mechanism — the detector tick (pure Go, no LLM)

Each watch tick, deterministically:

1. **Snapshot** materiality signals (no LLM — fork D confirmed):
   - **Per monitored desk** (roster agents incl. the XO): resolve pane, then
     `surface.Driver.Assess(pane)` → semantic `State`. NOT raw pane bytes — the
     assessed state, so a spinner-frame flicker is not "change."
   - **State tracker** (`.flotilla-state.md`): content hash.
   - (Operator messages are NOT a detector signal — the relay delivers them
     immediately, see "Operator wake" below.)
2. **Load** the persisted last-tick snapshot (a JSON file next to the ack file).
3. **Diff** → decide material change from the curated transition set (fork C).
4. On **material change** → wake the affected XO with a **targeted** prompt naming
   what changed. On **no change** → do nothing (the XO sleeps; the win).
5. **Persist** the new snapshot (atomic write — fork D).

The detector runs in the watch daemon, reusing the serialized injector, the
surface `Driver` (Assess + RotateContext), and the pane-resolution path.

## Fork C — materiality set v1 (actionable transitions only)

A material change is ANY of:

- A **desk State transition INTO an actionable state**, where the actionable set
  is exactly:
  - `→ StateShell` (crashed), `→ StateErrored`, `→ StateAwaitingApproval`,
    `→ StateAwaitingInput` (a desk newly needs attention), and
  - `StateWorking → StateIdle` ("finished a turn" — the XO may need to collect a
    result / advance).
  - **Explicitly NOT `→ StateWorking`** — a desk resuming or starting work needs
    no XO action; excluding it keeps the wake set tight (more $0-idle). Also not
    `Idle→Idle`/no-change, nor transitions into/out of `StateUnknown` (treated as
    no-signal to avoid flapping).
- The **state-tracker file hash changed** (someone updated the goal/task tracker).
- **XO self-continuation** (fork B, below).

The set is **spec-extensible**: PR/git-landed detection is DEFERRED to a
follow-up (keeps v1 tight). New signals add transitions/hashes to the snapshot +
the materiality predicate without changing the loop shape.

## Fork B — XO self-continuation (wake-once, rotate-between, settle)

To preserve self-continuation without a blind timer and without fragile
tracker-parsing (option (b) rejected):

- On the XO's own `StateWorking → StateIdle` transition, wake it **once** with a
  **continuation prompt** that carries the **narrow-answer discipline**:
  *"Advance the next clear, already-authorized step if one remains; otherwise
  reply 'idle' and do nothing — do NOT manufacture work."* (Required #1 — per the
  dont-gate-free-authorized-work + read-own-design rules; without it the XO churns
  on trivial steps.)
- If the XO continues (→Working→Idle again), repeat — with a **context rotate
  between steps** (fresh context each step; see RotateContext below).
- If the XO replies idle-settled, set a **settled flag** in the snapshot and
  **sleep** (no further self-continuation wakes) until an EXTERNAL material change
  (a desk transition, a tracker change, or an operator message).
- **Required #2:** an operator-message wake (relay) **clears the settled flag**,
  so a settled XO re-engages on operator input.

## Fork A — liveness (three layers; no regression of the K×interval window)

The current watchdog alerts when the XO's ack is older than `K×interval`
(K = `--max-missed-acks`, default 3). v2 wakes the XO only on material change, so
a healthy idle XO wouldn't touch the ack file — naive reuse would false-alert.
The three layers:

1. **Shell-detection (immediate).** Every tick the detector already Assesses the
   XO pane; `StateShell` → immediate crash alert. This is strictly BETTER than
   today's K-missed-ack lag for the crash case.
2. **Ack staleness (unchanged threshold).** Keep the existing rule — alert when
   the ack mtime is older than `K×interval`. This preserves the EXACT current
   wedged-XO detection window (no regression). **[SUPERSEDED by C1/C1b below:]**
   the strict `K×interval` window holds only under `liveness_ping_mode: interval`;
   the shipped default `none` (true $0-idle) intentionally widens the idle-fleet
   wedge window to ~`2K×interval` — a crash is still immediate. See C1b.
3. **Max-quiet liveness ping.** If the XO has not been woken (by a material change
   OR a prior ping) for **N intervals**, force a minimal liveness ping (a wake
   that asks only for an ack). **N is chosen `< K`** (default `N = max(1, K-1)`)
   so a healthy idle XO is pinged and re-acks BEFORE its ack crosses the
   `K×interval` staleness threshold — preventing a false down-alert — while a
   genuinely wedged XO still trips staleness at exactly `K×interval`.
   - **Monitoring-cadence reasoning (spec'd):** N is the upper bound on the gap
     between liveness checks; setting `N < K` keeps the wedged-XO detection
     latency at the current `K×interval` floor (no regression). A busy fleet
     (frequent material wakes) essentially never triggers the ping; it is the
     safety net for the alive-but-wedged-on-an-idle-fleet case.

## RotateContext — the production caller (the Phase-1 seam)

When the XO is woken, handles the change, and settles back to `StateIdle`, the
detector rotates its context via **`surface.RotateContext(xoDriver, xoPane)`**:

- claude-code (`SlashCommand`) → injects `/clear`; a future cursor XO
  (`RestartProcess`) → `ErrRestartRequired` (the caller restarts; the guard
  guarantees NO slash is typed into a restart-only TUI).
- **Gated by the awaiting-operator veto marker** (from the #18 lineage): do NOT
  rotate while the XO has an outstanding operator question.
- Net: each material-change handling runs in FRESH context — even a busy fleet
  never accumulates XO context. This is the v2 evolution of the closed-#18
  idle-context-reset, now detector-driven + driver-routed.

## Fork D — pure-Go + atomic/fail-safe snapshot

- The detector uses **deterministic signals only** (Assess states, file hashes);
  the XO LLM fires only on material change. That IS the $0-idle win.
- The snapshot JSON (next to the ack file; `--snapshot-file`, env override,
  default `<roster-dir>/flotilla-detector-state.json`) is **atomic-write**
  (write-temp + rename) and **fail-safe**: a missing or corrupt snapshot degrades
  to **treat-as-everything-changed → wake once** (conservative), and a
  read/parse/write error NEVER crashes the detector or silently skips a tick
  (the same fail-open principle as `Assess` capture-error → Idle).

## Operator wake (immediate, bypasses the detector)

A relay-delivered operator message wakes the target agent immediately (as today),
resets the heartbeat/quiet timer, and — for the XO — **clears the settled flag**
(fork B #2). Operator input is never gated behind the detector.

## Composition

Replaces the generic always-wake heartbeat. Reuses: the serialized `Injector`;
the surface `Driver` (`Assess`, `RotateContext`); the pane-resolution path; the
ack file. The watchdog's ack-staleness layer is retained; its trigger gains the
Shell-immediate + max-quiet-ping layers. The awaiting-veto gates the rotate.

## Backward compatibility / config

- New roster/flag config: enable v2 (`change_detector: true`?) vs the legacy
  always-wake heartbeat (default — decided at the openspec checkpoint; recommend
  v2 opt-in initially, then default once proven). `--snapshot-file`,
  `--max-quiet-intervals N` (default `max(1,K-1)`).
- Legacy heartbeat path unchanged when v2 is disabled.

## Test plan (TDD)

1. **Materiality predicate** (pure, table-driven): every actionable transition
   wakes; `→Working`, `Idle→Idle`, `→/from Unknown` do NOT; tracker-hash-change
   wakes; combinations.
2. **Snapshot fail-safe**: missing file → all-changed→wake-once; corrupt JSON →
   same; write via temp+rename (atomic); a write error does not crash/skip.
3. **Self-continuation**: Working→Idle → one continuation wake; settled reply →
   settled flag set → no further self-wake until external change; operator wake
   clears settled.
4. **Liveness three-layer**: Shell→immediate alert; ack-staleness at K×interval
   unchanged; max-quiet ping fires at N<K and refreshes ack (no false alert);
   wedged XO still trips at K×interval.
5. **RotateContext wiring**: XO settle → RotateContext called (claude→/clear);
   awaiting-veto present → rotate skipped; (RestartProcess surface → never
   injected — already guarded + tested in Phase 1).
6. `gofmt`/`vet`/`build`/`go test -race ./...` green; `openspec --strict` valid.

## Systems-review revisions (folded in — supersede the sections above where they conflict)

A pre-build systems-review (verified against `watchdog.go`/`ack.go`/`surface/`)
found 2 CRITICAL + 3 HIGH + MEDIUM/LOW. Corrections, authoritative:

- **[C1/C1b — liveness re-grounded on ack AGE, + a real tradeoff].** The current
  watchdog has NO mtime-staleness rule: `Watchdog.Observe` counts missed acks
  in-memory and `AckWatcher.Acked()` returns a bool ("mtime advanced since last
  call"). The "K×interval window" is EMERGENT from the XO being prompted (→acking)
  every interval — which v2 removes. So v2 MUST redesign liveness on **wall-clock
  ack age**: extend `AckWatcher` with `Age() time.Duration`; the detector tick
  (which still runs every interval) alerts when `Age() > K×interval` AND the XO
  isn't Shell — a cadence-independent threshold that genuinely preserves the
  window. **GENUINE TRADEOFF for the operator (cannot have all three):** strict
  ≤K×interval wedge-detection + true $0-idle + bounded LLM round-trip are mutually
  exclusive. A max-quiet ping at `N=K-1` leaves only ~1 interval for the
  wake→turn→touch round-trip before the K×interval alert → false-alerts a healthy-
  but-slow XO. Options: **(i)** ping every interval (N=1) with a MINIMAL ack-only
  prompt + rotate — preserves the exact window, but idle ≈ a cheap ping/interval
  (NOT $0, though far cheaper than today's full-duties wake); **(ii)** true $0-idle
  (no pings) and accept a larger idle-fleet wedge window (~2K×interval) — Shell
  (crash) stays immediate, and a wedged XO on a truly-idle fleet has nothing to do
  anyway; **(iii)** ping at N≤K-2 + require 2 consecutive missed pings (middle
  ground). **My rec: (ii)** — the $0-idle win is the whole point and the
  slow-idle-wedge cost is near-zero (no work is being missed; crashes still fire
  instantly). SURFACE THIS — it's the operator's call, not mine.

- **[C2 — the awaiting-veto marker is NEW WORK in this change, not a #18 lineage
  dependency].** PR #18 is CLOSED/never-merged; the veto marker, its flag/env, and
  its `docs/xo-doctrine.md` lifecycle do NOT exist in the tree (only the
  `RotateContext` helper from Phase 1 exists, with no production caller). This
  change MUST build the marker fresh: `--awaiting-file` (+ `$FLOTILLA_AWAITING_FILE`,
  default `<roster-dir>/flotilla-xo-awaiting`), read fail-safe (stale/unreadable →
  skip rotate, never wrongful rotate), and the XO doctrine for set/clear (set when
  posing an operator question, clear when answered/recorded). (I incorrectly cited
  it as shipped — the institutional-knowledge-loss anti-pattern; corrected.)

- **[H1 — hard cap on self-continuations].** Prompt discipline alone can't stop a
  runaway, and rotation ERASES the XO's memory that it's looping (it can't
  self-throttle post-/clear). Add `--max-self-continuations` (default ~3): after
  that many consecutive XO-initiated Working→Idle→wake cycles with NO interleaved
  external material change, force the settled flag and stop regardless of the XO's
  reply. Reset the counter on any external material change or operator message.

- **[H2 — exclude the XO pane from the desk-finished branch].** The XO is in the
  snapshot too; its own Working→Idle must feed ONLY self-continuation, never the
  desk-finished wake (special-case `cfg.XOAgent`). Desk-finished applies to panes
  ≠ XO. Test: XO Working→Idle → exactly one self-continuation wake.

- **[H3 — snapshot failure fails toward NOT-waking, loudly; liveness independent].**
  A persistent snapshot-WRITE failure must NOT silently regress to wake-every-tick
  (+ per-tick rotation — worse than legacy). On repeated write failure: raise a
  LOUD alert (the down-alert path) and degrade to in-memory-only snapshot (or
  no-wake), failing toward not-spending. Liveness state stays in-memory +
  ack-file, INDEPENDENT of the detector snapshot, so a snapshot outage never
  blinds the watchdog. (A missing/corrupt READ still → treat-all-changed → wake
  ONCE, then persist fresh — the conservative cold-start path.)

- **[M1 — materiality keys only on states the driver EMITS (normative)].** The
  claude-code `Assess` emits only Shell/Working/Idle; `Errored`/`AwaitingApproval`/
  `AwaitingInput` are reserved and emitted by NO driver yet. v1 materiality is
  normatively: `→Shell`, `Working→Idle`, + tracker-hash; the richer-state branches
  activate automatically when a driver emits them (no dead mandated branches).

- **[M2 — debounce Shell].** `Assess` maps a tmux read-error to `StateShell`
  (not Unknown), so a transient blip looks like a crash. Require **2 consecutive**
  Shell assessments before treating it as a crash transition/alert.

- **[M3 — synchronize detector state].** Operator-wake (gateway goroutine) clears
  the settled flag while the detector tick reads/writes it → guard detector state
  (settled flag, quiet/continuation counters, last-snapshot) behind a mutex OR a
  single-writer event channel. Add a `-race` test for operator-wake-during-tick.

- **[M4 — tracker-file path + fail-safe].** Spec the `.flotilla-state.md` location
  (default `<roster-dir>/.flotilla-state.md`, overridable; aligns with open #6),
  absent → no-signal, read-error → treat-UNCHANGED (avoid wake-storm).

- **[L1] every XO wake (material, continuation, ping) carries the ack
  instruction**, so any wake refreshes liveness. **[L2]** remove the byte-level
  activity probe in v2 (Assess is the semantic signal). **[L3]** cold-start seeds
  current states as the baseline WITHOUT emitting transitions (mirror
  `AckWatcher`/`lastFP` seeding) so a steady-idle desk doesn't spuriously fire
  Working→Idle.

## Open sub-decisions for the openspec checkpoint

- v2 enable default (opt-in vs default-on). Rec: opt-in first.
- Exact default for `N` (rec `max(1, K-1)`).
- Whether `StateErrored`/`StateAwaitingInput` are populated by the claude-code
  Assess in Phase 1 (currently it returns only Shell/Working/Idle) — v2's
  materiality set references them, so either (a) v1 keys only on the states the
  claude driver actually emits (Shell, Working→Idle) + tracker-hash, and the
  richer states activate when a driver emits them, or (b) extend claude Assess to
  detect approval/error strings now. Rec (a) — keep v1 to emitted states; the set
  is extensible for when drivers emit the richer states.
