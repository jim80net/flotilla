# Design — Recursive desk heartbeat (#183)

**Status:** DRAFT for the trio gate (systems-review + open-code-review + STORM) and operator review.
**Operator directive (2026-06-27, HIGH priority):** *"where you're missing translating your system
clock — that is the heartbeats — into probes and downstream heartbeats for your desks. Make sure your
heartbeats are recursively applied to your flotillas and then to their desks."*
**Live diagnosis:** a desk went idle Fri and nothing re-engaged it → zero progress all weekend (a
desk stalled mid-task with no downstream heartbeat).

## 1. The gap (code-grounded)

`flotilla watch` runs ONE change-detector that monitors the WHOLE agent tree (`DetectorConfig.Desks` =
every agent incl. the XO) but only ever INJECTS into the **primary XO** (`cfg.Wake → xoAgent`,
`detector.go:457-461`) — plus synthesizing parents on a subordinate-finish (`cfg.WakeAgent(agent,
WakeSynthesis, …)`, `detector.go:688-689`). EVERY non-primary-XO agent — leaf desks AND federated
sub-flotilla XOs — is ASSESSED (`cfg.Assess`) for material wakes / the visibility mirror / synthesis,
but is **never heartbeated**: nothing re-engages an agent that goes Idle mid-task. The only re-engagement
a desk gets is indirect — a *material* change wakes the XO, and the XO may or may not drive that desk.
So a desk (or a sub-XO) that settles to Idle with unfinished work **silently stalls**.

The XO has a full self-continuation machinery the desks lack: a quiet-tick counter (`quietTicks`), a
self-continuation cap (`selfCont`/`maxSelfCont`), a settle marker (`SettleConsume`/`XOSettled`), and a
continuation prompt (`detectorContinuationBuiltin`). The fix is to give EVERY monitored non-primary-XO
agent the SAME treatment — a recursive downstream heartbeat.

## 2. The mechanism (reuses the existing seams)

The detector already has the two things needed: it ASSESSES every agent's state each tick, and it has a
`WakeAgent(agent, kind, reasons)` seam that delivers a turn to an ARBITRARY agent (today only for
synthesis). A desk heartbeat is: *on a per-agent cadence, when a non-primary-XO agent is IDLE and has
NOT signaled settled, deliver it a "continue or idle" turn via `WakeAgent`.*

1. **Per-agent heartbeat state (mirror the XO's).** For each monitored agent that is NOT the primary XO,
   track a per-agent quiet-tick counter and a per-agent consecutive-heartbeat cap (parallel to
   `quietTicks` / `selfCont`). State lives in the detector (in-memory, keyed by agent), persisted in the
   snapshot like the XO's.
2. **Idle-gated trigger (each tick, under `d.mu`).** For each non-primary-XO agent: if its assessed state
   is `Idle` (NOT Working — a working agent is making progress; NOT Shell/AwaitingApproval — those are
   liveness/escalation paths, not heartbeat) AND it is NOT desk-settled AND its quiet-ticks ≥ the
   desk-heartbeat cadence, mark it owed a heartbeat. Reset its quiet counter when it transitions
   Working (it re-engaged) or when an operator/XO message is delivered to it.
3. **Deliver via `WakeAgent` (in `runTail`, off-mutex).** Enqueue `Job{Agent: agent, Message:
   <desk-continuation-prompt>, Kind: "desk-heartbeat"}`. The injector is ALREADY agent-agnostic
   (`inject.go` routes by `Job.Agent`; busy-defer is per-agent) — a heartbeat to a busy desk is dropped
   and re-evaluated next tick, exactly like an XO tick. The `Kind:"desk-heartbeat"` keeps it OUT of the
   operator audit mirror (like `heartbeat`/`detector` Kinds) — no operator-channel spam.
4. **The desk-continuation prompt** (per-agent overridable via the agent's workspace `HEARTBEAT.md`, like
   the XO's): *"[flotilla heartbeat] You finished a turn / have been idle. Advance the next clear,
   ALREADY-AUTHORIZED step of your current task — reading durable state, not memory. If a blocker is the
   only thing left, advance it locally and surface it in one line. If NOTHING authorized remains, reply
   'idle' and touch <desk-settle-marker> to signal done."*
5. **Per-agent settle signal (mirror the XO settle).** A per-agent settle marker the agent touches when
   genuinely done; the detector consumes it (suppressing further heartbeats) until the agent is
   re-engaged (a new operator/XO message, or a material change). Fail-safe: unreadable → NOT settled
   (keep heartbeating), same as the XO settle.
6. **Cap → escalate, not infinite-poke.** A per-agent consecutive-heartbeat cap (like
   `max-self-continuations`): after N heartbeats with no progress (no Working edge, no settle), STOP
   heartbeating that agent and ESCALATE it to its parent/the operator (a wedged desk surfaces loudly
   rather than being poked forever). Reuse the liveness-alert path.

## 3. Recursion / federation (it falls out of the tree)

Because the detector monitors the WHOLE tree (`Desks` = all agents), heartbeating EVERY non-primary-XO
agent IS the recursive cascade:
- **Leaf desks** get a direct heartbeat → they advance their own task.
- **Federated sub-flotilla XOs** are themselves non-primary-XO agents in `Desks`, so they ALSO get the
  cadence heartbeat → a sub-XO re-engages to drive ITS desks (and the leaf desks under it get their own
  direct heartbeats too — belt-and-suspenders). The tree topology (`roster.AgentsBelow/AgentsAbove`)
  already models parent→child; the escalation (§2.6) routes a wedged agent to its parent via that tree.
- If a sub-flotilla runs its OWN `flotilla watch` (the "clock is per-XO" federation model), each
  daemon heartbeats its own subtree — the same mechanism, naturally recursive across daemons.

So the operator's "recursively applied to your flotillas and then to their desks" = the detector
heartbeats every agent it monitors, and the federation tree makes that a cascade.

## 4. Safety / not-spamming (the design's load-bearing constraints)

- **Idle-gated:** never heartbeat a Working agent (it's progressing) — only an Idle one.
- **Cadence-bounded:** a per-agent interval (a multiple of the tick; tunable, default e.g. the
  heartbeat interval) — not every tick.
- **Settle-suppressed:** a genuinely-done agent (touched its settle marker) is not poked.
- **Cap-escalated:** a wedged agent escalates after N pokes, never infinite-loops.
- **Busy-safe:** a heartbeat to a busy agent is dropped (the injector's per-agent busy-defer), never
  queued to interrupt a turn.
- **Audit-quiet:** `Kind:"desk-heartbeat"` is not operator-mirrored.
- **Off-mutex delivery:** the enqueue is in `runTail`, off `d.mu` (like every other wake), so it never
  stalls the tick / `OperatorWake`.

## 5. Decisions (LOCKED — operator/XO, 2026-06-27)

1. **Cadence = the heartbeat interval (~20m).** An idle agent is re-engaged within one interval (a
   per-agent quiet-tick counter ≥ `interval/tick` ⇒ heartbeat).
2. **DEFAULT-ON, roster opt-OUT (NOT opt-in).** The operator directive is universal — heartbeats apply
   to ALL desks recursively — and opt-in would defeat the purpose (the desks that stall are exactly the
   ones nobody opted in). The consecutive-cap escalation (§5.3) is the safety against poking a wedged
   desk. A per-agent roster opt-OUT flag (`heartbeat: false` on an agent) excludes a deliberately-quiet
   desk. The primary XO keeps its OWN clock (it is never double-driven by the desk heartbeat).
3. **Consecutive-cap N = 3 → escalate to the parent, then stop.** After 3 consecutive heartbeats with no
   progress (no Working edge, no settle), STOP heartbeating that agent and ESCALATE it to its parent
   (`roster.AgentsAbove` → the XO; the primary XO for a top-level desk) via the liveness-alert path — a
   wedged agent surfaces loudly, never an infinite poke.
4. **Per-agent settle marker, mirroring the XO's.** A desk that replied 'idle' / touched its settle
   marker is NOT re-poked until its state changes (a new operator/XO message, or a material change,
   re-arms it — clears settled + resets the quiet/cap counters, exactly like `OperatorWake` for the XO).
   Fail-safe: unreadable ⇒ NOT settled (keep heartbeating).

## 6. Scope / phasing

- **In #183 (the mechanism):** per-agent heartbeat state (quiet-counter + consecutive-cap) + Idle-gated
  cadence trigger + `WakeAgent` delivery with `Kind:"desk-heartbeat"` + the desk-continuation prompt
  (workspace `HEARTBEAT.md`-overridable) + per-agent settle marker + the cap→escalate-to-parent path; the
  roster opt-OUT flag; the `watch` spec delta. DEFAULT-ON.
- **NOT in:** changing the XO's own heartbeat/clock; the legacy always-wake heartbeat (v2/detector
  only); a new daemon (this is additive to the existing detector).

## 7. Synthesis interaction (confirmed composes)

A sub-XO gets BOTH synthesis wakes (material, on a subordinate finish) AND the cadence heartbeat. They
compose because the heartbeat is **Idle-gated**: while the sub-XO is Working a synthesis turn it is not
heartbeated, and a synthesis wake resets its quiet counter (it re-engaged). So no double-drive — the
heartbeat is the floor (re-engage an Idle sub-XO with nothing material pending); synthesis is the
material-triggered overlay.

## 8. Design-trio findings folded (systems + OCR + STORM — all three, code-grounded)

The trio confirmed the mechanism + seams are real, and found that the XO's machinery is SINGULAR (one
settle file, one `OperatorWake` keyed to the XO, in-memory scalar counters) and does NOT generalize
per-agent for free. Resolutions:

### 8a. SAFETY (load-bearing) — claude's Idle SUBSUMES awaiting-approval → escalate to the operator
The design's "Idle-gate excludes AwaitingApproval/AwaitingInput" is FALSE for claude: `claudeCode.Assess`
(`claude.go:88-105`) is binary — Working iff a spinner, else Idle; it NEVER returns `AwaitingApproval`
(only grok does, `grok.go:190`, post-#158). So a claude desk parked on a permission/tool-approval modal
reads as IDLE and would be heartbeated — and the heartbeat could land text into a focused modal or be
read as authorization. With DEFAULT-ON this touches the approval-sensitive / order-placing desks.
**Resolutions (folded):**
- The desk-continuation prompt is EXPLICITLY NON-AUTHORIZING: *"Advance only ALREADY-AUTHORIZED steps. If
  a tool/permission/approval prompt is pending, do NOT approve it on a heartbeat — reply 'idle'."* (The
  XO prompt's "ALREADY-AUTHORIZED" framing, harder.)
- The input-blocked path: a heartbeat that hits a focused modal raises `ErrPanelBlocked` (`inject.go:172`)
  for a non-relay kind → logged + DROPPED (not fired into the modal); the cap MUST NOT count an
  input-blocked drop as a failed heartbeat (no false "wedged" escalation).
- **OPERATOR DECISION (surfaced):** approval-sensitive / order-placing desks opt-OUT by
  DEFAULT until the claude driver gains a genuine approval classifier (a separate driver follow-up). The
  universal directive is honored for the general fleet; the approval-sensitive desks are the carve-out the
  binary-Idle ambiguity forces.

### 8b. The settle RE-ARM is unwired for desks (would recreate the stall, one layer down)
`OperatorWake` (`detector.go:381-398`) clears settled/resets counters ONLY for the XO; `onAccepted`
(`watch.go:403-407`) calls it only `if target == xo` — even though the relay DOES route `@desk` messages
and calls `onAccepted(deskName)` (`relay.go:88-92`). **Fold:** add `AgentWake(agent)` (clears that
agent's settled + resets its quiet/cap), wired from `onAccepted` for EVERY target (not just the XO). A
material change relevant to the desk also re-arms it. Without this, a settled desk = permanent silence.

### 8c. Per-agent settle marker NAMESPACE + counter persistence
The XO settle is a SINGLE file; sharing it collides across desks (and races the XO's `SettleConsume`).
**Fold:** per-agent marker path `<roster-dir>/flotilla-<agent>-settled` + a per-agent `SettledMarker`;
the desk prompt's `{{settle}}` resolves to the AGENT's path (today `ResolvePrompt` hardcodes the XO's).
The quiet/cap COUNTERS are IN-MEMORY (matching the XO's, which are NOT snapshot-persisted — correcting
§2.1's "persisted in the snapshot" claim); the per-agent SETTLED state is the durable file marker (so a
restart does not re-poke a settled desk — closing the restart-storm).

### 8d. `Kind` + the `wakeAgent` dispatcher (delivery would be dropped/spammed as written)
- `SetMirror` suppresses only `"heartbeat"`/`"detector"` (`watch.go:197`) — a literal `"desk-heartbeat"`
  Kind would be operator-MIRRORED (spam). **Fold:** the desk heartbeat uses `Kind:"detector"` (already
  suppressed + already `isRelay`-false so busy-dropped, not queued).
- The wired `wakeAgent` closure REJECTS any non-synthesis kind (`watch.go:313-321`: `if kind !=
  WakeSynthesis { return }`). **Fold:** extend it with a `WakeDeskHeartbeat` case building the
  desk-continuation body — the cmd-side wiring (not the detector-side `cfg.WakeAgent` field) is the part
  that needs the change.

### 8e. Escalate-to-parent: `AgentsAbove` is EMPTY for a leaf desk
`AgentsAbove` (`synthesis.go:69-85`) is the synthesis-parent relation — populated for sub-XOs that OWN a
channel, EMPTY for a leaf desk (a member, not an XO). So the cap-escalation has no parent for the exact
agents it targets. **Fold:** escalate a leaf desk to the XO of the channel it is a MEMBER of
(`BindingForChannel(...).XOAgent`), falling back to the primary XO; and use the LOUD `Alert`/watchdog
path (operator-visible) — NOT a quiet `WakeAgent` to a possibly-idle parent.

### 8f. The cap "no-progress" measurement + reset matrix (was under-specified)
**Fold:** latch a per-agent `progressedSinceHeartbeat` bool — set TRUE on any transition INTO Working,
cleared when a heartbeat is enqueued. The cap increments only when a heartbeat fires AND the bool is
false. A Working edge RESETS the cap to 0. Escalate fires ONCE on the `== N` edge (matching the backlog
loop's edge-trigger, `detector.go:810`), then the stop suppresses subsequent ticks; the stop is re-armed
by the SAME `AgentWake` hook as the settle (§8b) — else a capped desk is wedged forever.

### 8g. Cold-start suppression + the delivery site
- Cold-start (`detector.go:540-547`) re-seeds everything Idle with no settle markers → a restart-storm.
  **Fold:** the cold tick owes NO desk a heartbeat (mirror the XO cold-start); a stalled desk waits one
  cadence post-restart before re-engagement (stated, acceptable).
- **Fold:** desk-heartbeat is a PARALLEL `tickLocked` section (decide under `d.mu` → a
  `pendingDeskHeartbeats` slice) + a `runDeskHeartbeats` tail (off-mutex, like `runSynthesis`), NOT
  inlined into `continueXO`/the XO wake path — byte-inert when off (regression-lock), matching the
  mirror/synthesis precedent.

### 8h. The desk prompt is DISTINCT from the XO's (doctrine)
**Fold:** a dedicated desk-continuation built-in (NOT a copy of `detectorContinuationBuiltin`): it
carries the agent's own `{{settle}}` path + teaches the touch-when-idle contract inline; it DROPS the
XO's "your context is rotated between steps" line (desks are NOT rotated by this design) and does NOT
instruct reading a durable tracker a leaf desk may not keep ("continue your in-flight task; if you've
lost the thread, reply idle"). The `HEARTBEAT.md` per-agent override still applies.

### 8i. Federation double-drive invariant + opt-OUT flag shape
- **Fold:** a sub-XO is heartbeated by EITHER its own daemon's clock OR the parent's desk-heartbeat,
  never both — the parent opt-OUTs any agent that is the primary XO of another running daemon.
- **Fold:** the roster opt-OUT is `roster.Agent.Heartbeat *bool` (pointer; absent ⇒ default-ON; a bare
  bool would make the zero value opt-OUT, inverting the intent).

### 8j. Process — spec delta + tasks MISSING (blocks the change)
**Fold:** author `specs/watch/spec.md` (ADD the recursive-desk-heartbeat requirement, referencing the
existing XO-heartbeat / materiality / self-continuation / serialized-injection requirements + the §5/§8
decisions) + `tasks.md` (bite-sized TDD, reusing the `detector_synthesis_test.go` agent-wake fixture
pattern) before implementation.

## 9. G4 implementation spec — every state transition traced (the careful core)

The per-agent heartbeat decision lives in the existing per-desk loop in `tickLocked` (`detector.go:601`),
reached ONLY past the cold-start early-return (`:584-591`) — so a cold baseline emits NO beats, for free,
exactly as the visibility mirror gets cold-start suppression for free. Decided UNDER `d.mu`; DELIVERED and
cap-accounted OFF `d.mu` in a new `runDeskHeartbeats` tail (mirroring `runSynthesis`), because delivery
acquires the pane transaction lock — a bounded wait that must never be held under the detector mutex.

Per monitored desk `name` (`name ∈ d.cfg.Desks`, `name != XOAgent`, gated on `d.cfg.HeartbeatEnabled(name)`),
with `cur = d.debounce(name, Assess(name))`:

**Transition table (under `d.mu`, in `tickLocked`):**
- `cur == Working`: progress. `deskProgressed[name]=true`; clear `deskStopped[name]` (progress un-wedges);
  `deskNoProgress[name]=0`; `deskSinceBeat[name]=0` (restart cadence — a freshly-idle desk gets a full
  cadence before its first beat); clear `deskSettled[name]`.
- `cur == Idle`: consume the per-agent marker — `if DeskSettleConsume!=nil && DeskSettleConsume(name) →
  deskSettled[name]=true`. THEN: if `deskSettled[name] || deskStopped[name]` → no beat, NO cadence accrual
  (a settled/stopped desk does not advance toward a beat). Else `deskSinceBeat[name]++`; if
  `deskSinceBeat[name] >= cadenceTicks` → desk is OWED a beat: append `name` to `pendingDeskBeats`, reset
  `deskSinceBeat[name]=0`. (Cap accounting is NOT done here — see below.)
- `cur` is Unknown/other (unassessable pane): no state change, no beat, NO cadence accrual — wait for a
  confirmed state (an unreadable pane is not a confirmed Idle).

**Cap accounting (OFF `d.mu`, in `runDeskHeartbeats`, AFTER the delivery attempt, under a short re-lock):**
the cap counts DELIVERED beats that produced no progress — an input-blocked drop is NOT a failed heartbeat
(§8f). For each `name ∈ pendingDeskBeats`: deliver via `WakeDeskHeartbeat(name)` (G5 dispatcher; enqueues
the desk-continuation `Job{Kind:"detector"}`, audit-suppressed). The dispatch reports delivered vs
input-blocked (the injector drops a non-relay kind on `ErrPanelBlocked`, §8a). Then re-lock `d.mu` and:
  - input-blocked → no cap change (the beat never landed; do NOT penalize the desk);
  - delivered AND `deskProgressed[name]` was set → `deskNoProgress[name]=0` (responsive desk never caps);
  - delivered AND not progressed → `deskNoProgress[name]++`; if `deskNoProgress[name] >= capN` → wedged:
    raise ONE loud escalation to the owning XO (G6, edge-trigger on `==capN`) + `deskStopped[name]=true`
    (stop beating until re-armed). Clear `deskProgressed[name]=false` after the accounting.

**Re-arm:** `AgentWake(name)` (G3, shipped) clears all five maps for `name` → the desk re-enters the cadence
fresh (a stopped/wedged desk resumes beating once the operator re-engages it).

**Why cap-accounting is off-mutex (the subtlety the live clock can't get wrong):** the owed-beat decision
needs only in-memory state (cheap, under `d.mu`), but whether a beat COUNTS toward the wedged-cap depends on
the delivery outcome (input-blocked vs landed), which is known only after the off-mutex dispatch. Doing cap
accounting in `tickLocked` would penalize a desk for a beat that an input-block silently swallowed — a false
"wedged" escalation. So `tickLocked` decides the cadence; `runDeskHeartbeats` decides the cap.

**G4 TDD matrix (every transition is a test):** (1) cold-start tick → no beat; (2) Idle + cadence → one
beat, cadence resets; (3) settled (marker consumed) → suppressed until re-arm; (4) Working → no beat, cadence
+ cap reset, progressed set; (5) Idle→Working→Idle (progress) → cap resets, never escalates; (6) capN
consecutive no-progress delivered beats → ONE escalation (edge `==capN`) + stopped, further ticks silent;
(7) AgentWake after stopped → beats resume; (8) opted-out / approval-sensitive-default-off → never beats;
(9) primary XO → never beats; (10) Unknown state → no beat, no cadence accrual; (11) input-blocked delivered
beat → NOT counted toward the cap (no false escalation).
