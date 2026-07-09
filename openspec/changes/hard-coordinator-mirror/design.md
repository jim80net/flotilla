# Design — hard coordinator egress mirror

**Status:** P0 product gap (operator regression, 2026-07-08). **Current state = regression:**
Discord shows watch errors only; Codex COS turn-finals never arrive.

## 1. Regression anatomy

```
Today (broken for Codex COS):

  Codex turn completes → codexstore.LatestResult ✓
    → detector W→I → CoordinatorMirrorOnFinish
         → deskMirror{ledgerOnly: true}  ← Discord SKIPPED
         → session-mirror.jsonl append (best-effort)
    → Claude Stop hook  ← INERT on Codex (no hook)
    → Discord: (silence)
    → Operator sees only watch LOUD alerts on failure paths
```

```
Target (harness-agnostic):

  Coordinator finish (any surface with ResultReader)
    → readDeskTurnFinal
    → readerModelInternal (info body for Discord/dash)
    → durable outbox enqueue (idempotent key)
    → worker: Discord chunk post + session-mirror + cos ledger
    → on sustained failure: LOUD operator alert + doctor B013
```

**Load-bearing comment to remove** (`watch.go:1490-1493`): "Discord posting is deliberately
omitted — the XO Stop hook already posts." That assumption is **false** for Codex, Grok
coordinators without hooks, and any seat where the hook is misinstalled.

## 2. Operator-visible vs internal traffic

| Traffic class | Mirror? | Mechanism |
|---|---|---|
| Coordinator turn-final (operator-facing prose) | **YES — required** | `CoordinatorMirrorOnFinish` |
| `flotilla notify` to operator | YES (existing) | notify + `mirrorNotifyToLedger` |
| Operator relay → coordinator (inbound) | YES (existing) | inject `SetMirror` + cos ledger |
| `flotilla send --no-mirror` | NO | explicit flag |
| Inter-agent send (default) | NO | `mirror_inter_agent` default false |
| Detector KindDetector / heartbeat prompts | NO | not operator-visible output |
| Adjutant buffer briefs to leader | NO | laminar internal seam |

**Rule:** If the text would appear in an executive mini-brief / turn-final to the operator, it is
operator-visible and MUST traverse the hard mirror path.

## 3. Scope — which agents

Coordinator mirror on finish SHALL apply to **every monitored coordinator** in `cfg.Desks` where
`IsCoordinator(name)` — not only `xo_agent`. This covers:

- Primary clock `xo_agent` (meta-XO / Codex COS today)
- `cos_agent` when distinct and monitored
- Project XOs when monitored (same hard path; channel webhook per agent)

Non-coordinator desks keep existing `MirrorOnFinish` (desk tier-1 path).

## 4. Tri-surface delivery (atomic unit)

One **MirrorUnit** per coordinator finish:

| Surface | Artifact | Failure mode |
|---|---|---|
| **Discord** | Webhook post via `transport.Post`, chunked at 1900 runes | Outbox retry + LOUD alert |
| **Dash** | `session-mirror/<agent>.jsonl` append | Outbox retry (same unit) |
| **CoS ledger** | `context-ledger.md` append `{from: agent, to: operator, gist}` | Best-effort after Discord success; log on fail |

Discord + session-mirror are **paired required** for unit success. CoS ledger follows notify
precedent (best-effort, never drops Discord success).

## 5. Durable outbox

**File:** `<roster-dir>/flotilla-coordinator-mirror-queue.json`

```jsonc
{
  "pending": [{
    "id": "sha256(agent|body|finished_at)",
    "agent": "xo",
    "body": "...",
    "verbose": "...",
    "enqueued_at": "2026-07-08T12:00:00Z",
    "attempts": 2,
    "last_error": "discord: 502"
  }]
}
```

**Worker:** watch daemon goroutine (or piggyback on injector idle), retry with exponential backoff
cap, replay on startup before live traffic (mirror `#286` relay queue).

**Idempotency:** finish-edge key = `hash(agent + normalized_body + minute_bucket)` suppresses
duplicate enqueue when Claude Stop hook and watch both fire.

**Roster knob (migration):**

```jsonc
"coordinator_mirror_via": "watch"   // watch | hook | both (default → watch for codex/grok/opencode; both for claude during migration)
```

v1 implementation MAY ship `watch` only for non-claude surfaces immediately; doctor warns on
`hook`-only codex coordinator.

## 6. Failure surfacing

| Condition | Behavior |
|---|---|
| Post fails transiently | Outbox retry; operator not spammed |
| Pending > 30m | LOUD alert: "coordinator mirror stalled for \<agent\>" |
| N consecutive finish edges with empty/skipped read | LOUD alert: "coordinator mirror SKIP: no turn-final" |
| Doctor B013 on bootstrap | `live_expected` coordinator + codex surface + no successful mirror in 24h → **FAIL** |

**Audit log:** extend mirror decision lines:

```
flotilla watch: coordinator-mirror POST xo 3 chunks resplen=4200
flotilla watch: coordinator-mirror OUTBOX-RETRY xo attempt=2: discord 502
flotilla watch: coordinator-mirror FAIL-CLOSED xo: outbox exhausted
```

## 7. Laminar flow composition

Hard mirror is **egress only** — it does not inject into the coordinator pane. Adjutant
`OperatorProtectedWindow` and buffer seam policy are unchanged. Mirror worker runs off the
detector tail goroutine (same as today's `coordinatorMirrorOne`), not through the injector.

## 8. Codex COS validation (acceptance)

1. Operator messages Codex COS in Discord → relay → COS responds → finish edge fires.
2. Within one detector tick + outbox drain: turn-final appears in Discord channel under COS webhook.
3. Same text in `session-mirror/<cos>.jsonl` and dash Conversations thread.
4. `flotilla bootstrap doctor` B013 green.
5. Inter-agent `flotilla send --no-mirror` does NOT appear in Discord.

## 9. Implementation phases

See `tasks.md`. Phase 1 (regression hotfix): remove `ledgerOnly`, wire Discord in
`coordinatorMirrorOnFinish`, extend detector to all coordinators. Phase 2: durable outbox +
retry + B013.

## 10. Related

- `openspec/changes/codex-coordinator-seat/design.md` §2.3 — update: watch is authoritative, hook optional
- `openspec/changes/dash-next-gen/` §3 tri-surface mirroring
- `openspec/changes/durable-relay-queue/` — outbox pattern
- `deploy/flotilla-xo-discord-mirror.sh` — deprecate to optional for Claude, not required path