# Design — the goal-driven loop (backlog gate; mechanical anti-passivity)

> **Status:** design-gate draft, **post-review** (`/systems-review` + `/open-code-review` done;
> both found the same 2 critical items — folded below). Next: **design-gate checkpoint with the
> XO** (4 decisions) → confirmatory re-review → openspec deltas + `tasks.md` → TDD → PR. grok #58
> stays queued behind.

## Problem (the defect, one line)

The autonomous loop can resolve to **idle while authorized work remains**. `continueXO`
(`internal/watch/detector.go:314-344`) settles the XO (`XOSettled=true`) on EITHER the XO's
self-signal (`SettleConsume()`) OR the `MaxSelfContinuation` cap — both **regardless of whether
work remains**. The XO can *self-declare* idle, and the cap *forces* idle. That is the
2026-06-16 passive-holding failure: "wake → if quiet, HOLD."

## Ratified brief (operator/XO, 2026-06-16)

- **Backlog gate in `continueXO`:** MUST NOT settle while `state/fleet-backlog.md` has
  **UNBLOCKED** items.
- **Drain = dispatch the top unblocked item each wake** — across desks/harnesses (incl.
  grok/opencode), NOT the XO doing everything; **quota-aware** (wake at **cadence**, not a tight
  0s loop).
- **Compose with** the existing settle / self-continuation / cap machinery — **gate it, don't
  rip it out.**
- **Settle ONLY** when the backlog is empty OR every remaining item is **operator-blocked** (and
  the XO drives PREP on those — never sits).

## Core insight (what the gate IS, verified by both reviews)

The defect is the XO *self-declaring* idle. The fix is a **mechanical veto**: the detector
**independently** reads the backlog and refuses to honor a settle (self-signal OR cap) while
unblocked items remain. Gating BOTH the `SettleConsume()` branch (`detector.go:329`) AND the
`selfCont > MaxSelfContinuation` branch (`detector.go:338`) is **necessary and sufficient** to
close the self-declare-idle hole (systems-review verified). A better prompt is NOT the fix (the
XO can ignore it by replying "idle") — the gate is the forcing function.

## Design

### 1. The backlog is a CONTRACT: an explicit status-marker convention + a FAIL-SAFE parser (XO clarification ①)

Rather than infer status from free prose (brittle), the backlog item-line is a **documented
contract**: a bullet with a leading bracketed **status marker**. The convention is documented in
BOTH the spec delta AND a header block in `fleet-backlog.md` (so the XO maintains it correctly);
the live file is migrated to it as a rollout step (the fail-safe below means an un-migrated line
never breaks the loop). A structured-format upgrade (e.g. a JSON/TOML sidecar) is a fine
follow-up if prose markers prove brittle.

**Item-line convention:** `- [<status>] <text>` where `<status>` ∈
`{in-flight, next, blocked, needs-attention, done}` (case-insensitive marker):
- `in-flight`, `next` → **unblocked** (actionable: drive it).
- `blocked`, `needs-attention` → **operator-blocked** (does NOT block settle; the XO drives PREP).
- `done` → **excluded** (drained).

```
type Status struct {
    Unblocked []string // ordered unblocked items (RAW lines, owner hint kept) — the drive queue
    Blocked   int      // operator-blocked count (for the alert text / logging)
    Done      int      // done count (informational)
    Malformed int      // item lines with NO recognized [status] marker — flagged; see fail-safe
    Found     bool     // the "## Backlog" section was located
    Items     int      // total item lines seen in the section
}
func Parse(markdown string) Status   // TOTAL function: never panics on any input
```

Parse rules — a **section-scoped line scan** (mirroring `deliver.ParseBusy`'s line idiom,
`busy.go:70-82` — NO markdown AST), and a **TOTAL, fail-safe** function (clarification ①):
- Locate the section by `strings.HasPrefix(line, "## Backlog")` (the live heading is
  `## Backlog (prioritized; …)` — prefix match). Section ends at the next `## `.
- An **item line** matches `^\s*(\d+\.|[-*+])\s+\S` at the section's base indent; an indented
  continuation line is NOT a new item. (Numbered `N.` items are accepted too — the marker, not the
  bullet glyph, carries the status.)
- The item's status = the FIRST `[<status>]` marker on the line, classified per the convention
  above. **Done is checked first** (precedence done → blocked → unblocked-marker).
- **FAIL-SAFE on a malformed/ambiguous item** (no recognized `[status]` marker, OR two
  conflicting markers): `Malformed++` AND append the raw line to `Unblocked` — i.e. **err toward
  KEEP DRIVING + FLAG**, never silently drop or misclassify, never crash. The wiring raises the
  `Alert` once when `Malformed > 0` ("N backlog lines lack a [status] marker — fix the format")
  so a format slip surfaces loudly while the loop keeps driving (and the malformed line, driven,
  is itself what surfaces it to the XO).
- `Unblocked` preserves file order (operator priority); it is the drive QUEUE the gate consumes
  (§4 picks the top non-stuck entry).

### 2. Fail-open is correct for ABSENT/UNREADABLE; LOUD for PRESENT-BUT-UNPARSEABLE (M2)

The single most dangerous failure mode (both reviews): a silent no-op. The wiring closure
distinguishes three cases — only ONE is silent:
- **file absent / unreadable** → `Unblocked:0` (silent no-gate; the file legitimately may not
  exist yet, and a torn mid-write read self-heals next tick). Fail toward today's behavior.
- **`Found && Items>0`** → gate on the parsed `Unblocked` count (the happy path; works on the
  live prose day-one).
- **PRESENT-BUT-UNPARSEABLE** (`!Found` but the file has content, OR `Found && Items==0` with
  non-blank section content) → raise the detector's `Alert` ONCE (loud: "backlog present but
  unparseable — gate may be inert") and fail toward **no-gate** (so a misconfigured
  `--backlog-file` path doesn't burn quota driving forever — but it is never SILENT). The alert,
  not the drive-vs-settle choice, is the safety the reviews demanded.

### 3. The detector consults an injected predicate, defaulted to inert (M1)

`DetectorConfig` gains an injected func; `NewDetector` **defaults it to an inert closure**
(matching `SignalHash`, `detector.go:121-124`) so `continueXO` calls it unconditionally (no
call-site nil-guard) and every OTHER deployment is byte-unchanged (regression-locked):

```
BacklogGate func() backlog.Status   // NewDetector defaults to func() backlog.Status { return backlog.Status{} }  (zero ⇒ Unblocked:0 ⇒ no gate)
BacklogStuckCap int                 // NewDetector defaults (e.g. if <1 { =5 }); optional --backlog-stuck-cap flag, parallel to MaxSelfContinuation
```

Production wires `BacklogGate` over a new `--backlog-file` flag whose **unset ⇒ inert** (opt-in),
aligned with the `--signal-file` precedent (`watch.go:54,196-199`), NOT the always-defaulted
`--tracker-file`. The file is **read fresh each call and NOT content-hashed** (it is XO-authored
output, like the tracker, `watch.go:184-188` — hashing it would self-wake the XO on its own
backlog edits, the exact bug that comment warns against; M3).

### 4. `continueXO` — gate the settle, faithful to the real control flow (H1)

Rotate runs FIRST on **both** branches (unchanged, gated by `Awaiting`); `SettleConsume()` is
consumed as today. The veto branches on the gate. `selfCont` keeps its empty-backlog cap meaning
(don't overload it — H1); a new **per-item** drive-count map `driveCount[itemKey]int` drives the
PER-ITEM stuck handling (clarification ④ — drive the top NON-stuck item, don't spin on one).

```
continueXO(cur, wake):
    # 1. rotate (UNCHANGED — gated by Awaiting; runs before any wake on every branch)
    if Awaiting==nil || !Awaiting(): Rotate()
    settleSignalled := SettleConsume()             # consume the marker as today (detector.go:329)
    gate := BacklogGate()                            # inert default ⇒ {Unblocked: nil}
    queue := gate.Unblocked                          # ordered drive queue (file priority); nil if no/empty gate

    # 2. Awaiting gates the DRIVE too, not just the rotate (P2-2): an outstanding operator
    #    question is a legitimate operator-gated pause (NOT the self-declare-idle defect).
    #    OperatorWake re-engages when the operator answers.
    if Awaiting!=nil && Awaiting(): queue = nil

    prune(driveCount, keysNotIn(queue))              # drop counts for items that left the unblocked set (drained/blocked)

    if len(queue) == 0:
        # backlog empty / all-operator-blocked / awaiting / no-gate → TODAY'S behavior, UNCHANGED:
        if settleSignalled: cur.XOSettled = true; return
        selfCont++
        if selfCont > MaxSelfContinuation: cur.XOSettled = true; log; return
        wake(WakeContinuation, nil); return

    # 3. len(queue) > 0 → NEVER settle (self-signal & cap OVERRIDDEN — the core fix):
    selfCont = 0                                     # not in the empty-backlog runaway regime
    # PER-ITEM stuck handling (④): drive the highest-priority item NOT yet over the stuck cap;
    # if EVERY queued item is over the cap, drive the top one anyway (keep driving at cadence).
    target := first item in queue with driveCount[key] < BacklogStuckCap, else queue[0]
    driveCount[target.key]++
    if driveCount[target.key] == BacklogStuckCap:    # just crossed → escalate THIS item ONCE
        Alert("backlog item «%s» not progressing after %d wakes — advance it, or mark it [blocked]/[needs-attention]", target, BacklogStuckCap)
    wake(WakeBacklog, [target.raw])                  # drive the top non-stuck unblocked item
```

A "stuck" item (driven `BacklogStuckCap` times while still unblocked) is **deprioritized** — the
loop drives lower-priority progressing items instead, and escalates the stuck one ONCE so the XO
durably marks it `[blocked]`/`[needs-attention]` (which removes it from the queue → its count is
pruned). The loop never SPINS on a non-progressing item, and never settles while any unblocked
item remains (clarification ④). `driveCount` is keyed by a stable item identity (the item text;
the `[status]` marker is at the line START, and a status change moves the item OUT of the
unblocked queue, so the key is stable while the item stays unblocked).

- **Cadence, not a tight loop (verified):** `continueXO` fires only on the XO's Working→Idle
  observed at `Tick` (every `Interval`=20m). A drive cycle needs the XO to go Working then Idle
  across ≥2 ticks, so wakes are paced at **≥ Interval** (≥20m). The cap's old "force settle" role
  becomes the stuck-loop **safety escalation** (the brief's "cap becomes pacing").
- **Un-settle invariant (the gate does NOT re-arm an already-settled XO — pinned):**
  `continueXO` is entered only when `!cur.XOSettled` (`detector.go:291`), so once settled the gate
  is dormant until something clears `XOSettled`. This is SAFE only because the backlog can gain
  unblocked items solely via paths that ALREADY clear `XOSettled`: an operator message
  (`OperatorWake`, `detector.go:226`) or an external desk transition (`detector.go:289`). A settled
  XO is idle and cannot self-edit the backlog. **Invariant: every way the backlog gains an unblocked
  item also clears `XOSettled`** — locked by a test, so a future change that lets the XO self-add
  items while settled (which would silently strand them) is caught.

### 5. Liveness interaction — `AckAge` is the load-bearing wedge backstop (P1-2)

This regime (XO never settles while the backlog is non-empty) is now the dominant one, so the
liveness story must be explicit and **locked by a test**:
- **Crash detection is unaffected:** `evalLiveness` (`detector.go:368-383`) keys the crash branch
  on `shellStreak >= shellDebounce`, independent of `XOSettled`/the backlog. A gone pane still
  crash-alerts in 2 ticks.
- **Wedge detection is unaffected and is the backstop:** `evalLiveness` runs **unconditionally
  every Tick** (`detector.go:283`), and the wedge branch fires on `AckAge() > alertInterval ×
  Interval` regardless of `XOSettled`. Every wake appends the ack instruction, so a *driven,
  alive* XO re-acks; a *wedged* XO (stuck in Working, or accepting deliveries into a frozen
  composer) stops acking and the wedge alert fires after the window — even though `continueXO`
  never settles. With `liveness_ping_mode: none`, the safety ping is wide (2K intervals) anyway.
- **`WakePing` is intentionally moot** while always-driving (`continueXO` wakes every cadence →
  `quietTicks=0`), and that is SAFE *only because* `AckAge` is the independent backstop — NOT
  because "the ping is moot." **Regression test (required): `Unblocked>0` forever + `AckAge`
  exceeds the window → the wedge alert STILL fires.** This guards against a future refactor that
  wrongly makes `evalLiveness` conditional on `XOSettled` and silently blinds the watchdog.

### 6. The wake prompt (`WakeBacklog`)

A new `WakeKind` (sibling of `WakeContinuation`/`WakeMaterial`/`WakePing`); the per-kind prompt is
composed in `cmd/flotilla/watch.go` (`watch.go:196-209`, where deployment paths live — the
detector owns WHEN/WHY, the caller owns the text, `detector.go:23-26`). The prompt passes the
**raw line of the driven item (the top non-stuck unblocked item, owner hint included)** and
instructs: dispatch it to the right
desk/harness if not started; check-in/unblock if in flight; if operator-blocked, drive PREP and
move on; "reply idle ONLY if every remaining item is done or operator-blocked — the loop will not
settle while unblocked work remains; read the backlog file, not memory" (rotate erases memory each
wake, so durable-state re-read is the contract — `watch.go:34`). **The `WakeBacklog` body MUST
append the ack instruction** (`ackInstr`, like every other wake kind, `watch.go:167,211`) — a
continuously-driven XO that is never told to ack would falsely trip the `AckAge` wedge alert (§5).

### 7. `OperatorWake` (unchanged, verified)

`OperatorWake` (`detector.go:219-234`) clears `XOSettled` + resets counters on an operator
message — strictly compatible (it re-engages AND likely adds a backlog item; the gate then
drives). It must also **clear `driveCount`** (alongside the existing `selfCont=0`) — a fresh
operator directive must not inherit stale per-item stuck counts (which would wrongly fire a stuck
alert / deprioritize on the next wake).

## Ratified decisions (XO design gate, 2026-06-16) — all 4, with clarifications folded above

1. **Parse the live PROSE now — YES, as a documented CONTRACT.** §1 defines the explicit
   `- [<status>] <text>` marker convention (in-flight/next/blocked/needs-attention/done),
   documented in the spec AND a `fleet-backlog.md` header, with a **TOTAL fail-safe parser**
   (malformed → flag + err-toward-driving, never crash, never silently misclassify). A
   structured-format upgrade is a noted follow-up.
2. **`Awaiting` gates the drive — YES** (§4 step 2). A desk awaiting input/approval needs that
   input, not another task; `OperatorWake` re-engages.
3. **In-flight items block settle — YES (Option A)** (§4 / queue). Load-bearing: off-harness /
   pull-only desks don't push (no `externalMaterial` wake, `detector.go:287`), so the XO must keep
   waking to collect/advance their in-flight work.
4. **Stuck-loop = escalate-but-keep-driving, PER ITEM** (§4 step 3). Escalate the STUCK ITEM
   (operator alert) + keep draining the REST of the queue, and **deprioritize** the stuck item so
   the loop does NOT spin on it (quota-aware) — the XO durably marks it `[blocked]`/
   `[needs-attention]` in response, removing it from the queue.

## Quota envelope (P2-1 — an owned decision, not an emergent surprise)

Interval=20m, ping=none. Steady-state while unblocked items remain: ~1 drive wake per 1-2
intervals (~every 20-40m), on top of the external-change wakes those desks already generate.
Worst case (backlog never drains): continuous 20m-cadence waking + the `BacklogStuckCap` alert.
This is the brief's intended tradeoff (drive, don't idle); the operator owns it. The
`--backlog-file` opt-in means it is OFF until explicitly enabled.

## Test plan (TDD — deterministic, no tmux/clock/LLM)

**`internal/backlog.Parse`** (pure; fixtures incl. a **verbatim copy of the real, contract-format
`fleet-backlog.md`** per backward-compat-builds-old-shape):
- the contract-format file → `len(Unblocked)=5, Blocked=1` (5 `[in-flight]`/`[next]` items, 1
  `[blocked]`), `Unblocked[0]` = the top item's raw line, `Found:true`, `Items:6`.
- empty section → `len(Unblocked)=0, Found:true, Items:0` (settle-eligible).
- no `## Backlog` section but non-empty file → `Found:false` (closure alerts).
- `[done]`/`~~`/`✅` excluded; `[blocked]`/`[needs-attention]` → `Blocked`; ignores
  `## Operator decisions` / `## Dropped` / `## Goals` sections.
- **fail-safe (①):** an item line with NO recognized `[status]` marker → `Malformed++` AND appended
  to `Unblocked` (err toward driving); a line matching BOTH `[done]` and `[blocked]` → done
  (precedence); `Parse` never panics on any input (fuzz a few pathological strings).
- `[done]` is matched literally (the lowercase prose word "done" must NOT count as the marker).

**`Detector.continueXO`** (stub `BacklogGate`, scripted):
- empty queue + settle-signalled → settles (today's behavior preserved — regression lock).
- empty queue + cap exceeded → settles (preserved).
- non-empty queue + settle-signalled → does NOT settle; rotates; wakes `WakeBacklog` with the top
  item (the self-signal override — the core fix).
- **per-item stuck (④):** queue `[A,B]`, A driven `BacklogStuckCap` times while still queued →
  escalates A ONCE, then drives **B** (deprioritizes A, does not spin); when A leaves the queue its
  `driveCount` is pruned; all-items-stuck → keeps driving the top at cadence (no settle, no re-spam).
- `Awaiting()==true` + non-empty queue → does NOT backlog-drive (falls to the existing settle/cap).
- `BacklogGate` inert default → byte-identical to today (regression lock).
- `OperatorWake` clears `driveCount` (a re-engage doesn't inherit stale stuck counts).

- **un-settle invariant (P2-B lock):** a settled XO (`XOSettled=true`) is NOT re-armed by the gate
  directly; an `OperatorWake` (or external desk transition) clears `XOSettled` AND then a
  `Unblocked>0` tick drives — i.e. a new unblocked item reaches the XO only via a path that clears
  settled. (Guards the invariant against a future self-add-while-settled regression.)

**Liveness (P1-2 lock):** `Unblocked>0` forever + `AckAge` over the window → wedge alert fires.

**Wiring (`cmd/flotilla/watch.go`)**: `--backlog-file` unset ⇒ inert; present-but-unparseable ⇒
`Alert` once; the `WakeBacklog` prompt names the top item AND carries the ack instruction (P3).

## Non-goals / deferred

- The backlog CONTENTS (deployment-circumstantial; XO-owned). This change owns the parse convention +
  the gate mechanism (generalizable flotilla capability) — and **requires the XO write the backlog
  atomically** (temp+rename, mirroring the snapshot discipline, `detector.go:74-75`) so a
  mid-write read can't tear; documented in `xo-doctrine.md` (M3).
- grok #58 (the grok-build read-path) — queued behind this.
- The legacy heartbeat (this gates the v2 change-detector's `continueXO` only).
