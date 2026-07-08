# Design — mechanical operator protected window (adjutant seam gate)

**Status:** Design-only. **Implementation is mandatory** — prompt-contract alone is explicitly
insufficient per operator directive (`flotilla-dispatch-c2b2726e`).

## 1. Invariant

When `OperatorProtectedWindow(leader) == true`, the watch daemon MUST NOT enqueue a
**non-urgent** adjutant consolidated brief (`KindDetector` seam inject from
`drainAdjutantSeamFor`) into `leader`'s pane. Buffered items stay in
`flotilla-<leader>-buffer.json` until the window closes.

**Urgent bypass** (skip buffer / cut through protected window):

| Class | Mechanism today | Protected-window behavior |
|---|---|---|
| Operator relay | `KindRelay` — never buffered | Always delivers (existing busy-defer) |
| Money / irreversible / fork / incident / incapacitation | `urgent_windows[]` + `UrgentMaterial()` | Leader immediate wake; never held for seam |
| Evaluation mechanical ack | Adjutant touches `flotilla-<leader>-alive` | Allowed on adjutant pane — not a leader inject |

Routine finish-edge material, stale-desk digests, and consolidated buffer briefs are **non-urgent**
for this gate.

## 2. Detection sources (v1 mechanical OR)

Predicate is **true when any** source fires. All sources are host-local and public-safe (no
deployment identifiers in tests/fixtures).

```
                    ┌─────────────────────────────────────┐
  relay queue ─────►│ Pending KindRelay for leader        │
  awaiting marker ─►│ flotilla-<leader>-awaiting present  │──► OR ──► protected?
  relay in-flight ─►│ Injector has deferred relay to leader│
  active conv tail ─►│ Recent confirmed relay + no settle  │
                    └─────────────────────────────────────┘
                              │
                    optional (phase 2):
                    dash bridge compose-active for leader channel
```

### 2.1 Pending operator relay (primary — operator typing proxy)

**Source:** `flotilla-relay-queue.json` contains a pending entry with `agent == leader`.

**Rationale:** Operator messages defer when the leader is `Working` (#286). A pending queue
entry means an operator message is **in flight** toward that leader — the operator is engaged
or waiting on delivery. Injecting an adjutant brief into the same pane would interleave with
that relay.

**API:** extend `relayQueueStore` with `PendingForAgent(agent string) bool` (read-only; no new
disk format).

### 2.2 Awaiting-operator marker (active conversation)

**Source:** `AwaitingMarker` for leader reports `Present() == true`
(`flotilla-<leader>-awaiting` via `roster.LayerAwaitingPath` / `ResolveLayerClockPath`).

**Rationale:** Leader posed a question to the operator; rotate and seam inject are already
fail-safe vetoed for context wipe. Same marker gates adjutant leader inject.

**Fail-safe:** Reuse existing awaiting semantics — unreadable marker ⇒ present (suppress inject).

### 2.3 In-flight relay in injector queue (race window)

**Source:** Injector worker queue contains a `KindRelay` (or `KindDefault`) job targeting
`leader` not yet confirmed.

**Rationale:** Between enqueue and confirm, queue file may not yet reflect deferral; in-memory
queue is the authoritative short window.

**API:** `Injector.HasPendingRelayFor(agent string) bool` — O(n) scan of buffered channel;
acceptable (queue depth ≪ fleet size).

### 2.4 Active conversation tail (post-delivery, pre-resolution)

**Source:** Within `activeConversationTTL` (default **10 minutes**) of a **confirmed**
`KindRelay` delivery to `leader`, AND leader has not consumed a resolution seam:

- `settled` marker not touched since relay confirm, **OR**
- `awaiting` marker still present (already covered by 2.2)

Track via lightweight sidecar `flotilla-<leader>-last-operator-relay.json`:

```json
{ "message_id": "…", "delivered_at": "2026-07-08T12:00:00Z" }
```

Written on confirmed relay mirror hook; cleared when leader touches `settled` or removes
`awaiting`. If sidecar unreadable ⇒ treat as **protected** (fail-safe).

**Rationale:** Operator message delivered to leader pane; leader is composing a reply — distinct
from queue-pending (relay already landed). Prevents adjutant brief mid-reply.

### 2.5 Dash / bridge compose-active (optional adapter — bridge integration)

**Source:** `OperatorComposeActive(channelOrLeader string) bool` on an optional watch seam.

When dash-next-gen bridge exposes operator compose state for a bound Discord channel, watch
registers the adapter at startup. Absent adapter ⇒ source inert (false).

**Rationale:** Restores laminar flow for operators typing in the web bridge before send — same
protected semantics as Discord relay defer.

## 3. Fail-safe posture

| Situation | Behavior |
|---|---|
| Any detection source errors / unreadable | **Suppress** leader seam inject (buffer retained) |
| All sources false | Allow seam inject per existing laminar rules |
| Protected + non-urgent buffer at seam | Skip inject; log info; retry next `AdjutantSeamOnFinish` / evaluation act-by-tier |
| Protected + urgent material | Urgent path already bypassed buffer — unaffected |

**Never** fail-open into leader pane on ambiguity. A missed brief costs delay; a mid-operator
interrupt costs trust.

## 4. Code seams

### 4.1 New package surface — `internal/watch/operatorprotected.go`

```go
type ProtectedWindowInput struct {
    Leader           string
    Awaiting         func(string) bool   // leader → awaiting marker present
    RelayQueuePending func(string) bool  // leader → pending relay on disk
    InjectorRelayPending func(string) bool
    ActiveConversation func(string) bool // leader → tail TTL active
    BridgeComposeActive func(string) bool // optional; nil ⇒ false
}

func OperatorProtectedWindow(in ProtectedWindowInput) bool
```

`cmd/flotilla/watch.go` wires real closures from roster paths, relay store, injector, sidecar.

### 4.2 Gate point — `drainAdjutantSeamFor`

Before `injector.Enqueue` of seam brief:

```go
if OperatorProtectedWindow(in) {
    log.Printf("flotilla watch: adjutant seam deferred for %q (operator protected window)", owner)
    return // buffer unchanged; claim not registered
}
```

### 4.3 Evaluation tick act-by-tier

`adjutantEvaluationTickBody` instructs inject at seam. Mechanical enforcement: when protected,
evaluation tick may still **ack** alive file on adjutant turn, but `drainAdjutantSeamFor` /
leader-targeted digest enqueue is suppressed until window clears.

Anti-starvation: if buffer oldest item age exceeds `bufferSeamMaxWait` (default **30 minutes**)
AND not protected, inject at next evaluation tick even if leader still `Working` — existing
#439 stale-leader path. Protected window **still blocks** that inject; TTL resumes when window
closes.

### 4.4 Prompt contract demotion

`adjutantDualObservationContract` prose remains as **documentation for the adjutant agent**, but
MUST carry a header line:

> Mechanical gate: watch suppresses leader seam inject during operator protected windows; do not
> `flotilla notify` the leader to bypass this gate for routine items.

## 5. Goal-loop composition

Goal-driven loops keep the leader `Working` for extended periods. Rules:

| State | Protected? | Seam inject? |
|---|---|---|
| Leader `composing` (loop posture), goal active, operator idle | false | Defer to finish seam OR evaluation `bufferSeamMaxWait` |
| Leader `composing`, operator relay pending | true | **Suppress** |

Loop posture vocabulary: `openspec/changes/loop-aware-status-taxonomy/` — adjutant observes
`loop_posture`, not pane `idle` alone.
| Leader `Working`, awaiting marker set | true | **Suppress** |
| Leader `Idle` / settled, not protected | false | Allow `drainAdjutantSeamFor` |
| Evaluation tick, work-found, not protected | false | May inject despite `Working` after TTL |
| Evaluation tick, protected | true | Ack + evaluate only; buffer retained |

**Forbidden:** Waiting for perfect long idle while buffer starves **when not protected**.
**Also forbidden:** Injecting through protected window to cure starvation.

## 6. Tests (TDD — load-bearing)

| Test | Package | Asserts |
|---|---|---|
| `TestOperatorProtectedWindow_pendingRelay` | `internal/watch` | Queue entry ⇒ true |
| `TestOperatorProtectedWindow_awaiting` | `internal/watch` | Awaiting marker ⇒ true |
| `TestOperatorProtectedWindow_injectorRelay` | `internal/watch` | In-flight relay job ⇒ true |
| `TestOperatorProtectedWindow_activeConversationTail` | `internal/watch` | Recent relay sidecar ⇒ true |
| `TestOperatorProtectedWindow_failSafeUnreadable` | `internal/watch` | Corrupt sidecar ⇒ true |
| `TestOperatorProtectedWindow_allClear` | `internal/watch` | No signals ⇒ false |
| `TestDrainAdjutantSeam_suppressedWhenProtected` | `cmd/flotilla` | Finish edge + protected ⇒ no leader enqueue |
| `TestDrainAdjutantSeam_allowedWhenClear` | `cmd/flotilla` | Finish edge + clear ⇒ brief enqueued |
| `TestDrainAdjutantSeam_urgentBypassUnaffected` | `cmd/flotilla` | Urgent material still immediate (existing) |

Use generic names from `flotilla.example.json` (`xo`, `xo-adj`).

## 7. Bootstrap §2.4 amendment (for PR #520)

Replace the "Signal (implementer)" column for protected windows with:

| Window | Mechanical signal (v1) |
|---|---|
| Operator typing | Pending relay queue entry for leader; optional bridge compose-active |
| Operator active conversation | Awaiting marker; active-conversation tail after confirmed relay |
| Leader mid-compose (non-operator) | **Not** a protected window alone — handled by laminar buffer + seam |

Add doctor note **B011a**: when adjutant configured, verify watch build includes
`OperatorProtectedWindow` gate (feature flag or compile-time symbol check in doctor).

## 8. Permissions cross-ref (PR #521)

`laminar.no_interject_during_operator_window` capability is **enforced by watch**, not harness
permissions. Canonical JSON comment updated to reference this change ID.

## 9. Rollout

1. Land this design + spec delta (permissions branch or standalone commit).
2. Implement `operatorprotected.go` + gate in `drainAdjutantSeamFor` with tests.
3. Wire active-conversation sidecar on relay confirm mirror hook.
4. Bootstrap doctor B011a + validation V9c (protected-window suppress case).
5. Optional: dash bridge adapter when compose-active API ships.